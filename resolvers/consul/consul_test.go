package consul

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
)

func TestNew(t *testing.T) {
	_, server := makeClientServer(t)
	defer server.Stop()
	// Let's try to have fun with consul config
	os.Setenv("CONSUL_HTTP_ADDR", server.HTTPAddr)
	defer os.Setenv("CONSUL_HTTP_ADDR", "")

	config := records.NewConfig()
	errch := make(chan error)
	version := "1.0"

	// Hopefully the ENV vars above should allow us
	// to override the defaults
	backend := New(config, errch, version)
	_, err := backend.Client.Agent().Self()
	if err != nil {
		t.Error("Failed to get consul client initialized")
	}
}

func TestConnectAgents(t *testing.T) {
	_, server := makeClientServer(t)
	defer server.Stop()

	// Let's try to have fun with consul config
	os.Setenv("CONSUL_HTTP_ADDR", server.HTTPAddr)
	defer os.Setenv("CONSUL_HTTP_ADDR", "")

	config := records.NewConfig()
	errch := make(chan error)
	version := "1.0"

	// Hopefully the ENV vars above should allow us
	// to override the defaults
	backend := New(config, errch, version)
	_, err := backend.Client.Agent().Self()
	if err != nil {
		t.Error("Failed to get consul client initialized")
	}

	err = backend.connectAgents()
	if err != nil {
		t.Log(err)
	}

	if len(backend.Agents) != 1 {
		t.Error("Failed to get agent connection")
	}

	// Let's see what happens the second time
	// It should just return early since the agent already
	// exists in our list of agents
	err = backend.connectAgents()
	if err != nil {
		t.Log(err)
	}

	if len(backend.Agents) != 1 {
		t.Error("Failed to get agent connection")
	}

}

func TestSlaveRecords(t *testing.T) {
	sj := loadState(t)

	_, server := makeClientServer(t)
	defer server.Stop()

	// Let's try to have fun with consul config
	os.Setenv("CONSUL_HTTP_ADDR", server.HTTPAddr)
	defer os.Setenv("CONSUL_HTTP_ADDR", "")

	config := records.NewConfig()
	errch := make(chan error)
	version := "1.0"

	// Hopefully the ENV vars above should allow us
	// to override the defaults
	backend := New(config, errch, version)
	_, err := backend.Client.Agent().Self()
	if err != nil {
		t.Error("Failed to get consul client initialized")
	}

	err = backend.connectAgents()
	if err != nil {
		t.Log(err)
	}

	rg := &records.RecordGenerator{State: sj}
	// :D
	for _, slave := range rg.State.Slaves {
		slave.PID.Host = "127.0.0.1"
	}
	backend.insertSlaveRecords(rg.State.Slaves)

	// Should make a little more programmatic test
	for _, agent := range backend.Agents {
		services, err := agent.Services()
		if err != nil {
			t.Log(err)
			continue
		}
		for name, service := range services {
			log.Println("Name:", name)
			log.Println("ID:", service.ID)
			log.Println("Service:", service.Service)
			log.Println("Address:", service.Address)
			log.Println("Port:", service.Port)
		}
	}
}

func makeClientServer(t *testing.T) (*api.Client, *testutil.TestServer) {

	// Make client config
	conf := api.DefaultConfig()

	// Create server
	// Redirect logs to /dev/null cause we really dont care about consul agent ouput
	server := testutil.NewTestServerConfig(t, func(c *testutil.TestServerConfig) {
		c.LogLevel = "ERR"
		c.Stdout = ioutil.Discard
		c.Stderr = ioutil.Discard
	})
	conf.Address = server.HTTPAddr

	// Create client
	client, err := api.NewClient(conf)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	return client, server
}

func loadState(t *testing.T) state.State {
	var sj state.State
	b, err := ioutil.ReadFile("../../factories/fake.json")
	if err != nil {
		t.Fatal(err)
	} else if err = json.Unmarshal(b, &sj); err != nil {
		t.Fatal(err)
	}

	return sj
}
