// Copyright 2015 Canonical Ltd.
// Copyright 2015 Cloudbase Solutions SRL
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"encoding/base64"

	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/cloudconfig/providerinit/renderers"
	"github.com/juju/juju/provider/azure"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/version"
)

type UserdataSuite struct {
	testing.BaseSuite
}

var _ = gc.Suite(&UserdataSuite{})

func (s *UserdataSuite) TestAzureUnix(c *gc.C) {
	renderer := azure.AzureRenderer{}
	data := []byte("test")
	result, err := renderer.EncodeUserdata(data, version.Ubuntu)
	c.Assert(err, jc.ErrorIsNil)
	expected := base64.StdEncoding.EncodeToString(utils.Gzip(data))
	c.Assert(string(result), jc.DeepEquals, expected)

	data = []byte("test")
	result, err = renderer.EncodeUserdata(data, version.CentOS)
	c.Assert(err, jc.ErrorIsNil)
	expected = base64.StdEncoding.EncodeToString(utils.Gzip(data))
	c.Assert(string(result), jc.DeepEquals, expected)
}

func (s *UserdataSuite) TestAzureWindows(c *gc.C) {
	renderer := azure.AzureRenderer{}
	data := []byte("test")
	result, err := renderer.EncodeUserdata(data, version.Windows)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result, jc.DeepEquals, renderers.ToBase64(renderers.WinEmbedInScript(data)))
}

func (s *UserdataSuite) TestAzureUnknownOS(c *gc.C) {
	renderer := azure.AzureRenderer{}
	result, err := renderer.EncodeUserdata(nil, version.Arch)
	c.Assert(result, gc.IsNil)
	c.Assert(err, gc.ErrorMatches, "Cannot encode userdata for OS: Arch")
}
