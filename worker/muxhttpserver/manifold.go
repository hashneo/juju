// Copyright 2020 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package muxhttpserver

import (
	"github.com/juju/errors"
	"github.com/juju/worker/v2"
	"github.com/juju/worker/v2/dependency"

	"github.com/juju/juju/apiserver/apiserverhttp"
	"github.com/juju/juju/pki"
)

type ManifoldConfig struct {
	AuthorityName string
	Logger        Logger
}

func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AuthorityName,
		},
		Output: manifoldOutput,
		Start:  config.Start,
	}
}

func manifoldOutput(in worker.Worker, out interface{}) error {
	inServer, ok := in.(*Server)
	if !ok {
		return errors.Errorf("expected Server, got %T", in)
	}

	switch result := out.(type) {
	case **apiserverhttp.Mux:
		*result = inServer.Mux
	default:
		return errors.Errorf("expected Mapper, got %T", out)
	}
	return nil
}

func (c ManifoldConfig) Start(context dependency.Context) (worker.Worker, error) {
	if err := c.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	var authority pki.Authority
	if err := context.Get(c.AuthorityName, &authority); err != nil {
		return nil, errors.Trace(err)
	}

	return NewServer(authority, c.Logger, DefaultConfig())
}

func (c ManifoldConfig) Validate() error {
	if c.AuthorityName == "" {
		return errors.NotValidf("empty AuthorityName")
	}
	if c.Logger == nil {
		return errors.NotValidf("nil Logger")
	}
	return nil
}
