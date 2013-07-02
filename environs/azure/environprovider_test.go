// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"io/ioutil"
	. "launchpad.net/gocheck"
	"launchpad.net/juju-core/environs/config"
	"launchpad.net/juju-core/instance"
)

type EnvironProviderSuite struct {
	ProviderSuite
}

var _ = Suite(new(EnvironProviderSuite))

func (EnvironProviderSuite) TestOpen(c *C) {
	prov := azureEnvironProvider{}
	attrs := makeAzureConfigMap(c)
	attrs["name"] = "my-shiny-new-env"
	cfg, err := config.New(attrs)
	c.Assert(err, IsNil)

	env, err := prov.Open(cfg)
	c.Assert(err, IsNil)

	c.Check(env.Name(), Equals, attrs["name"])
}

// create a temporary file with a valid WALinux config built using the given parameters.
// The file will be cleaned up at the end of the test calling this method.
func writeWALASharedConfig(c *C, deploymentId string, deploymentName string) string {
	configTemplateXML := `
	<SharedConfig version="1.0.0.0" goalStateIncarnation="1">
	  <Deployment name="%s" guid="{495985a8-8e5a-49aa-826f-d1f7f51045b6}" incarnation="0">
	    <Service name="%s" guid="{00000000-0000-0000-0000-000000000000}" />
	    <ServiceInstance name="%s" guid="{9806cac7-e566-42b8-9ecb-de8da8f69893}" />
	  </Deployment>
	</SharedConfig>`
	config := fmt.Sprintf(configTemplateXML, deploymentId, deploymentName, deploymentId)
	file, err := ioutil.TempFile(c.MkDir(), "")
	c.Assert(err, IsNil)
	filename := file.Name()
	err = ioutil.WriteFile(filename, []byte(config), 0644)
	c.Assert(err, IsNil)
	return filename
}

func (EnvironProviderSuite) TestParseWALASharedConfig(c *C) {
	deploymentId := "b6de4c4c7d4a49c39270e0c57481fd9b"
	deploymentName := "gwaclmachineex95rsek"
	filename := writeWALASharedConfig(c, deploymentId, deploymentName)
	oldConfigPath := _WALAConfigPath
	_WALAConfigPath = filename
	defer func() { _WALAConfigPath = oldConfigPath }()

	config, err := parseWALAConfig()
	c.Assert(err, IsNil)
	c.Check(config.Deployment.Name, Equals, deploymentId)
	c.Check(config.Deployment.Service.Name, Equals, deploymentName)
}

func (EnvironProviderSuite) TestConfigGetDeploymentName(c *C) {
	deploymentId := "b6de4c4c7d4a49c39270e0c57481fd9b"
	config := WALASharedConfig{Deployment: WALADeployment{Name: deploymentId, Service: WALADeploymentService{Name: "name"}}}

	c.Check(config.getDeploymentHostname(), Equals, deploymentId+".cloudapp.net")
}

func (EnvironProviderSuite) TestConfigGetDeploymentHostname(c *C) {
	deploymentName := "gwaclmachineex95rsek"
	config := WALASharedConfig{Deployment: WALADeployment{Name: "id", Service: WALADeploymentService{Name: deploymentName}}}

	c.Check(config.getDeploymentName(), Equals, deploymentName)
}

func (EnvironProviderSuite) TestPublicAddressAndPrivateAddress(c *C) {
	deploymentId := "b6de4c4c7d4a49c39270e0c57481fd9b"
	filename := writeWALASharedConfig(c, deploymentId, "name")
	oldConfigPath := _WALAConfigPath
	_WALAConfigPath = filename
	defer func() { _WALAConfigPath = oldConfigPath }()

	expectedAddress := deploymentId + ".cloudapp.net"
	prov := azureEnvironProvider{}
	pubAddress, err := prov.PublicAddress()
	c.Assert(err, IsNil)
	c.Check(pubAddress, Equals, expectedAddress)
	privAddress, err := prov.PrivateAddress()
	c.Assert(err, IsNil)
	c.Check(privAddress, Equals, expectedAddress)
}

func (EnvironProviderSuite) TestInstanceId(c *C) {
	deploymentName := "deploymentname"
	filename := writeWALASharedConfig(c, "deploy-id", deploymentName)
	oldConfigPath := _WALAConfigPath
	_WALAConfigPath = filename
	defer func() { _WALAConfigPath = oldConfigPath }()

	prov := azureEnvironProvider{}
	instanceId, err := prov.InstanceId()
	c.Assert(err, IsNil)
	c.Check(instanceId, Equals, instance.Id(deploymentName))
}
