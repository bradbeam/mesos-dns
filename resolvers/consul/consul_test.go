package consul

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
)

func TestNew(t *testing.T) {
	server, _ := backendSetup(t)
	defer server.Stop()
}

func TestConnectAgents(t *testing.T) {
	server, backend, _ := recordSetup(t)
	defer server.Stop()

	// Let's see what happens the second time
	// It should just return early since the agent already
	// exists in our list of agents
	err := backend.connectAgents()
	if err != nil {
		t.Log(err)
	}

	if len(backend.Agents) != 1 {
		t.Error("Failed to get agent connection")
	}

}

func TestSlaveRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	backend.insertSlaveRecords(sj.Slaves)

	// 2x Slaves
	// 1x Consul
	validateRecords(t, backend, 3)
}

func TestMasterRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	backend.insertMasterRecords(sj.Slaves, sj.Leader)

	// 3x Master
	// 1x Leader
	// 1x Consul
	validateRecords(t, backend, 5)
}

func TestFrameworkRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	backend.insertFrameworkRecords(sj.Frameworks)

	// 1x Marathon
	// 1x Consul
	validateRecords(t, backend, 2)
}

func makeClientServer(t *testing.T) *testutil.TestServer {

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

	return server
}

func loadState(t *testing.T) state.State {
	var sj state.State
	b, err := ioutil.ReadFile("../../factories/state.json")
	if err != nil {
		t.Fatal(err)
	} else if err = json.Unmarshal(b, &sj); err != nil {
		t.Fatal(err)
	}

	return sj
}

func backendSetup(t *testing.T) (*testutil.TestServer, *ConsulBackend) {
	server := makeClientServer(t)

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
	return server, backend
}

func recordSetup(t *testing.T) (*testutil.TestServer, *ConsulBackend, state.State) {
	sj := loadState(t)

	server, backend := backendSetup(t)
	err := backend.connectAgents()
	if err != nil {
		t.Error("Issue connecting to agents.", err)
	}

	rg := &records.RecordGenerator{State: sj}

	// :D
	// Do this for testing so we can have
	// a consul agent ( our dummy test server )
	// running on the same host as the mesos process
	for _, slave := range rg.State.Slaves {
		slave.PID.Host = "127.0.0.1"
	}
	rg.State.Leader = "master@127.0.0.1:5050"
	rg.State.Frameworks[0].PID.Host = "127.0.0.1"

	return server, backend, rg.State
}

func validateRecords(t *testing.T, backend *ConsulBackend, expected int) {
	// Should make a little more programmatic test
	for _, agent := range backend.Agents {
		services, err := agent.Services()
		if err != nil {
			t.Error("Unable to get list of services back from agent.", err)
			return
		}

		if len(services) != expected {
			t.Error("Did not get back", expected, "services. Got back", len(services))
		}
	}

}
