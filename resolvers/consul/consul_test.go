package consul

import (
	"encoding/json"
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

func TestMesosRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	backend.insertMesosRecords(sj.Slaves, sj.Leader)

	// 3x Master
	// 2x Master
	// 1x Leader
	// 1x Consul
	validateRecords(t, backend, 7)
}

func TestFrameworkRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	backend.insertFrameworkRecords(sj.Frameworks)

	// 1x Marathon
	// 1x Consul
	validateRecords(t, backend, 2)
}

func TestTaskRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.insertMesosRecords(sj.Slaves, sj.Leader)

	for _, framework := range sj.Frameworks {
		backend.insertTaskRecords(framework.Name, framework.Tasks)
	}

	// 1x Consul
	// 5x slave
	// 1x nginx
	// 1x nginx-host
	// 1x nginx-none
	// 1x nginx-bridge
	// 2x4 fluentd
	validateRecords(t, backend, 19)
}

func TestCleanupRecords(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.insertMesosRecords(sj.Slaves, sj.Leader)

	for _, framework := range sj.Frameworks {
		backend.insertTaskRecords(framework.Name, framework.Tasks)
	}

	// We'll go ahead and have all the tasks there
	validateRecords(t, backend, 19)

	time.Sleep(time.Duration(2*backend.Refresh)*time.Second + 1)
	backend.Cleanup()
	// All but the consul record should be removed
	validateRecords(t, backend, 1)

}

func TestHealthchecks(t *testing.T) {
	server, backend, sj := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.insertMesosRecords(sj.Slaves, sj.Leader)
	backend.insertFrameworkRecords(sj.Frameworks)
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
		t.Log("Creating healthcheck healthchecks/" + check.ID)
		p := &api.KVPair{Key: "healthchecks/" + check.ID, Value: b}
		_, err = kv.Put(p, nil)
		if err != nil {
			t.Error(err)
		}
	}

	for _, framework := range sj.Frameworks {
		backend.insertTaskRecords(framework.Name, framework.Tasks)
	}

	// 1x Consul
	// 5x slave
	// 1x nginx
	// 1x nginx-host
	// 1x nginx-none
	// 1x nginx-bridge
	// 2x4 fluentd
	validateRecords(t, backend, 20)

	//validateRecords(t, backend, 12)
	// Probably need to create a validateChecks func
	validateChecks(t, backend, 2)
	/*
		for _, agent := range backend.Agents {
			services, err := agent.Services()
			if err != nil {
				t.Error("Unable to get list of services back from agent.", err)
				return
			}

			for k, info := range services {
				t.Error(" -", k, "=>", info.Address, "=>", info.Service, "=>", info.ID)
			}

		}
	*/
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
