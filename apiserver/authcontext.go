// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"net/http"
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/utils/clock"
	"gopkg.in/juju/names.v2"
	"gopkg.in/macaroon-bakery.v1/bakery"
	"gopkg.in/macaroon-bakery.v1/httpbakery"

	"github.com/juju/juju/apiserver/authentication"
	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/bakerystorage"
)

// authContext holds authentication context shared
// between all API endpoints.
type authContext struct {
	st *state.State

	clock     clock.Clock
	agentAuth authentication.AgentAuthenticator

	// localUserBakeryService is the bakery.Service used by the controller
	// for authenticating local users. In time, we may want to use this for
	// both local and external users. Note that this service does not
	// discharge the third-party caveats.
	localUserBakeryService *expirableStorageBakeryService

	// localUserInteractions maintains a set of in-progress local user
	// authentication interactions.
	localUserInteractions *authentication.Interactions

	// macaroonAuthOnce guards the fields below it.
	macaroonAuthOnce   sync.Once
	_macaroonAuth      *authentication.ExternalMacaroonAuthenticator
	_macaroonAuthError error
}

// newAuthContext creates a new authentication context for st.
func newAuthContext(st *state.State) (*authContext, error) {
	ctxt := &authContext{
		st: st,
		// TODO(fwereade) 2016-07-21 there should be a clock parameter
		clock: clock.WallClock,
		localUserInteractions: authentication.NewInteractions(),
	}

	// Create a bakery service for local user authentication. This service
	// persists keys into MongoDB in a TTL collection.
	store, err := st.NewBakeryStorage()
	if err != nil {
		return nil, errors.Trace(err)
	}
	localUserBakeryService, localUserBakeryServiceKey, err := newBakeryService(
		st, store, nil,
	)
	if err != nil {
		return nil, errors.Trace(err)
	}
	ctxt.localUserBakeryService = &expirableStorageBakeryService{
		localUserBakeryService, localUserBakeryServiceKey, store, nil,
	}
	return ctxt, nil
}

// authenticator returns an authenticator.EntityAuthenticator for the API
// connection associated with the specified API server host.
func (ctxt *authContext) authenticator(serverHost string) authenticator {
	return authenticator{ctxt: ctxt, serverHost: serverHost}
}

// authenticator implements authenticator.EntityAuthenticator, delegating
// to the appropriate authenticator based on the tag kind.
type authenticator struct {
	ctxt       *authContext
	serverHost string
}

// Authenticate implements authentication.EntityAuthenticator
// by choosing the right kind of authentication for the given
// tag.
func (a authenticator) Authenticate(
	entityFinder authentication.EntityFinder,
	tag names.Tag,
	req params.LoginRequest,
) (state.Entity, error) {
	auth, err := a.authenticatorForTag(tag)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return auth.Authenticate(entityFinder, tag, req)
}

// authenticatorForTag returns the authenticator appropriate
// to use for a login with the given possibly-nil tag.
func (a authenticator) authenticatorForTag(tag names.Tag) (authentication.EntityAuthenticator, error) {
	if tag == nil {
		auth, err := a.ctxt.externalMacaroonAuth()
		if errors.Cause(err) == errMacaroonAuthNotConfigured {
			// Make a friendlier error message.
			err = errors.New("no credentials provided")
		}
		if err != nil {
			return nil, errors.Trace(err)
		}
		return auth, nil
	}
	switch tag.Kind() {
	case names.UnitTagKind, names.MachineTagKind:
		return &a.ctxt.agentAuth, nil
	case names.UserTagKind:
		return a.localUserAuth(), nil
	default:
		return nil, errors.Annotatef(common.ErrBadRequest, "unexpected login entity tag")
	}
}

// localUserAuth returns an authenticator that can authenticate logins for
// local users with either passwords or macaroons.
func (a authenticator) localUserAuth() *authentication.UserAuthenticator {
	return &authentication.UserAuthenticator{
		Service: a.ctxt.localUserBakeryService,
		Clock:   a.ctxt.clock,
	}
}

// externalMacaroonAuth returns an authenticator that can authenticate macaroon-based
// logins for external users. If it fails once, it will always fail.
func (ctxt *authContext) externalMacaroonAuth() (authentication.EntityAuthenticator, error) {
	ctxt.macaroonAuthOnce.Do(func() {
		ctxt._macaroonAuth, ctxt._macaroonAuthError = newExternalMacaroonAuth(ctxt.st)
	})
	if ctxt._macaroonAuth == nil {
		return nil, errors.Trace(ctxt._macaroonAuthError)
	}
	return ctxt._macaroonAuth, nil
}

var errMacaroonAuthNotConfigured = errors.New("macaroon authentication is not configured")

// newExternalMacaroonAuth returns an authenticator that can authenticate
// macaroon-based logins for external users. This is just a helper function
// for authCtxt.externalMacaroonAuth.
func newExternalMacaroonAuth(st *state.State) (*authentication.ExternalMacaroonAuthenticator, error) {
	controllerCfg, err := st.ControllerConfig()
	if err != nil {
		return nil, errors.Annotate(err, "cannot get model config")
	}
	idURL := controllerCfg.IdentityURL()
	if idURL == "" {
		return nil, errMacaroonAuthNotConfigured
	}
	// The identity server has been configured,
	// so configure the bakery service appropriately.
	idPK := controllerCfg.IdentityPublicKey()
	if idPK == nil {
		// No public key supplied - retrieve it from the identity manager.
		idPK, err = httpbakery.PublicKeyForLocation(http.DefaultClient, idURL)
		if err != nil {
			return nil, errors.Annotate(err, "cannot get identity public key")
		}
	}
	// We pass in nil for the storage, which leads to in-memory storage
	// being used. We only use in-memory storage for now, since we never
	// expire the keys, and don't want garbage to accumulate.
	//
	// TODO(axw) we should store the key in mongo, so that multiple servers
	// can authenticate. That will require that we encode the server's ID
	// in the macaroon ID so that servers don't overwrite each others' keys.
	svc, _, err := newBakeryService(st, nil, bakery.PublicKeyLocatorMap{idURL: idPK})
	if err != nil {
		return nil, errors.Annotate(err, "cannot make bakery service")
	}
	var auth authentication.ExternalMacaroonAuthenticator
	auth.Service = svc
	auth.Macaroon, err = svc.NewMacaroon("api-login", nil, nil)
	if err != nil {
		return nil, errors.Annotate(err, "cannot make macaroon")
	}
	auth.IdentityLocation = idURL
	return &auth, nil
}

// newBakeryService creates a new bakery.Service.
func newBakeryService(
	st *state.State,
	store bakerystorage.ExpirableStorage,
	locator bakery.PublicKeyLocator,
) (*bakery.Service, *bakery.KeyPair, error) {
	key, err := bakery.GenerateKey()
	if err != nil {
		return nil, nil, errors.Annotate(err, "generating key for bakery service")
	}
	service, err := bakery.NewService(bakery.NewServiceParams{
		Location: "juju model " + st.ModelUUID(),
		Store:    store,
		Key:      key,
		Locator:  locator,
	})
	if err != nil {
		return nil, nil, errors.Trace(err)
	}
	return service, key, nil
}

// expirableStorageBakeryService wraps bakery.Service, adding the ExpireStorageAt method.
type expirableStorageBakeryService struct {
	*bakery.Service
	key     *bakery.KeyPair
	store   bakerystorage.ExpirableStorage
	locator bakery.PublicKeyLocator
}

// ExpireStorageAt implements authentication.ExpirableStorageBakeryService.
func (s *expirableStorageBakeryService) ExpireStorageAt(t time.Time) (authentication.ExpirableStorageBakeryService, error) {
	store := s.store.ExpireAt(t)
	service, err := bakery.NewService(bakery.NewServiceParams{
		Location: s.Location(),
		Store:    store,
		Key:      s.key,
		Locator:  s.locator,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return &expirableStorageBakeryService{service, s.key, store, s.locator}, nil
}
