package consul

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"testing"
	"time"

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
	server, backend := recordSetup(t)
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

func TestMesosRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	backend.insertMesosRecords()

	// 5x Slave
	// 1x Consul
	validateRecords(t, backend, 7)
}

func TestFrameworkRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	backend.insertFrameworkRecords()

	// 1x Marathon
	// 1x Consul
	validateRecords(t, backend, 2)
}

func TestTaskRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.insertMesosRecords()

	for _, framework := range backend.State.Frameworks {
		backend.insertTaskRecords(framework.Tasks)
	}

	// 1x Consul
	// 5x slave
	// 1x nginx
	// 1x nginx-host
	// 1x nginx-none
	// 1x nginx-bridge
	// 2x4 myapp
	validateRecords(t, backend, 19)
}

func TestCleanupRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	// Shorten our TTL for testing
	backend.Refresh = 1
	rg := &records.RecordGenerator{State: backend.State}
	// This should handle all the record generation
	// and do a cleanup afterwards
	backend.Reload(rg, errors.New(""))

	// We'll go ahead and verify all the tasks there
	validateRecords(t, backend, 20)
	// Try a sample refresh cycle
	time.Sleep(time.Duration(backend.Refresh) * time.Second)
	// Nothing should change aside from TTL stamp
	backend.Reload(rg, errors.New(""))
	validateRecords(t, backend, 20)
	// Wait for TTLs to expire
	time.Sleep(time.Duration(2*backend.Refresh)*time.Second + 1)
	// Do an explicit cleanup
	backend.Cleanup()
	// All but the consul record should be removed
	validateRecords(t, backend, 1)

}

func TestHealthchecks(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	// Post KV for consul healthchecks
	// nginx/port
	// nginx/http
	kv := backend.Client.KV()
	nport := &api.AgentCheckRegistration{
		ID:   "nginx/port",
		Name: "nginx/port",
		AgentServiceCheck: api.AgentServiceCheck{
			TCP:      "localhost:80",
			Interval: "5s",
		},
	}

	nhttp := &api.AgentCheckRegistration{
		ID:   "nginx/http",
		Name: "nginx/http",
		AgentServiceCheck: api.AgentServiceCheck{
			HTTP:     "http://localhost",
			Interval: "5s",
		},
	}

	for _, check := range []*api.AgentCheckRegistration{nport, nhttp} {
		b, err := json.Marshal(check)
		p := &api.KVPair{Key: "healthchecks/" + check.ID, Value: b}
		_, err = kv.Put(p, nil)
		if err != nil {
			t.Error(err)
		}
	}

	rg := &records.RecordGenerator{State: backend.State}
	backend.Reload(rg, errors.New(""))

	// 1x Consul
	// 5x slave
	// 1x nginx
	// 1x nginx-host
	// 1x nginx-none
	// 1x nginx-bridge
	// 2x4 myapp
	validateRecords(t, backend, 20)

	validateChecks(t, backend, 2)
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
	b, err := ioutil.ReadFile("test/state.json")
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

func recordSetup(t *testing.T) (*testutil.TestServer, *ConsulBackend) {
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
	backend.State = rg.State

	return server, backend
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
			t.Error("Services:")
			for k, info := range services {
				t.Error(" -", k, "=>", info.Address)
			}

		}
	}

}

func validateChecks(t *testing.T, backend *ConsulBackend, expected int) {
	for _, agent := range backend.Agents {
		checks, err := agent.Checks()
		if err != nil {
			t.Error(err)
		}

		if len(checks) != expected {
			t.Error("Did not get back", expected, "checks. Got back", len(checks))
			t.Error("Checks:")
			for k, info := range checks {
				t.Error(" -", k, "=>", info.ServiceID)
			}

		}
	}
}
