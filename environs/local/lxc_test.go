package local

import (
	"flag"
	. "launchpad.net/gocheck"
	"testing"
)

const Defaultcontainer = "lxc_test"

func Test(t *testing.T) { TestingT(t) }

type S struct{}

var _ = Suite(&S{})

var lxcEnabled = flag.Bool("lxc", false, "enable LXC tests that require sudo")

func (s *S) SetUpSuite(c *C) {
	if !*lxcEnabled {
		c.Skip("lxc tests need sudo access (-lxc to enable)")
	}
}

func (s *S) TestCreate(c *C) {
	var container container
	container.Name = Defaultcontainer

	_, err := container.create()
	c.Assert(err, IsNil)

	c.Assert(container.running(), Equals, true)

	_, err = container.destroy()
	c.Assert(err, IsNil)

	c.Assert(container.running(), Equals, false)
}

func (s *S) TestStart(c *C) {
	var container container
	container.Name = Defaultcontainer

	_, err := container.create()
	c.Assert(err, IsNil)

	_, err = container.start()
	c.Assert(err, IsNil)

	_, err = container.stop()
	c.Assert(err, IsNil)

	_, err = container.destroy()
	c.Assert(err, IsNil)
}

func (s *S) TestIsRunningWhencontainerIsCreated(c *C) {
	var container container
	container.Name = Defaultcontainer

	_, err := container.create()
	c.Assert(err, IsNil)

	c.Assert(container.running(), Equals, true)

	_, err = container.destroy()
	c.Assert(err, IsNil)
}

func (s *S) TestIsNotRunningWhencontainerIsNotCreated(c *C) {
	var container container
	container.Name = Defaultcontainer

	c.Assert(container.running(), Equals, false)
}

func (s *S) TestRootPath(c *C) {
	var container container
	container.Name = Defaultcontainer
	c.Assert(container.rootPath(), Equals, "/var/lib/lxc/"+Defaultcontainer+"/rootfs/")
}
