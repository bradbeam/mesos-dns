package consul

import (
	"encoding/json"
	"errors"
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

	backend.generateMesosRecords()

	// Each slave is a single id
	expected := make(map[string]int)
	expected["20160107-001256-134875658-5050-27524-S66"] = 1
	expected["20160107-001256-134875658-5050-27524-S3"] = 1
	expected["20160107-001256-134875658-5050-27524-S1"] = 1
	expected["20160107-001256-134875658-5050-27524-S2"] = 1
	expected["20160107-001256-134875658-5050-27524-S0"] = 1
	expected["master@127.0.0.2:5050"] = 1

	// 6 records ( 5x slaves, 1x leader )
	validateStateRecords(t, backend.MesosRecords, 6, expected)
}

func TestFrameworkRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	backend.generateMesosRecords()
	backend.generateFrameworkRecords()

	// Framework is only running on a single slave
	expected := make(map[string]int)
	expected["20160107-001256-134875658-5050-27524-S2"] = 1

	// 1 record ( marathon )
	validateStateRecords(t, backend.FrameworkRecords, 1, expected)
}

func TestTaskRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.generateMesosRecords()

	for _, framework := range backend.State.Frameworks {
		backend.generateTaskRecords(framework.Tasks)
	}

	// Each slave can have a different number of tasks running
	expected := make(map[string]int)
	expected["20160107-001256-134875658-5050-27524-S66"] = 2
	expected["20160107-001256-134875658-5050-27524-S3"] = 3
	expected["20160107-001256-134875658-5050-27524-S1"] = 3
	expected["20160107-001256-134875658-5050-27524-S2"] = 2
	expected["20160107-001256-134875658-5050-27524-S0"] = 2

	// 5 Records ( 5x slaves )
	validateStateRecords(t, backend.TaskRecords, 5, expected)
}

func TestHealthchecks(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	setupHealthChecks(t, backend)

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.generateMesosRecords()

	for _, framework := range backend.State.Frameworks {
		backend.generateTaskRecords(framework.Tasks)
	}

	// Each slave can have a different number of tasks running
	expected := make(map[string]int)
	expected["20160107-001256-134875658-5050-27524-S66"] = 2
	expected["20160107-001256-134875658-5050-27524-S3"] = 3
	expected["20160107-001256-134875658-5050-27524-S1"] = 3
	expected["20160107-001256-134875658-5050-27524-S2"] = 2
	expected["20160107-001256-134875658-5050-27524-S0"] = 2

	// 5 Records ( 5x slaves )
	validateStateRecords(t, backend.TaskRecords, 5, expected)
	expectedhc := make(map[string]int)
	expectedhc["mesos-dns:mesosmaster-r01-s01:myapp.98e56ea4-b4d3-11e5-b2bb-0242d4d0a230:31383"] = 0
	expectedhc["mesos-dns:mesosmaster-r01-s01:myapp.98e56ea4-b4d3-11e5-b2bb-0242d4d0a230:31384"] = 0
	expectedhc["mesos-dns:mesosmaster-r02-s02:myapp.98e40f12-b4d3-11e5-b2bb-0242d4d0a230:31383"] = 0
	expectedhc["mesos-dns:mesosmaster-r02-s02:myapp.98e40f12-b4d3-11e5-b2bb-0242d4d0a230:31384"] = 0
	expectedhc["mesos-dns:mesosslave-r01-s01:nginx-no-net.215c789f-c611-11e5-aca8-0242965d2034:31477"] = 0
	expectedhc["mesos-dns:mesosslave-r01-s01:nginx-host-net.7a43b7d6-c611-11e5-aca8-0242965d2034:31423"] = 0
	expectedhc["mesos-dns:mesosslave-r02-s02:nginx-no-port.4266d369-b9a7-11e5-b2bb-0242d4d0a230"] = 2
	expectedhc["mesos-dns:mesosslave-r02-s02:myapp.98e65905-b4d3-11e5-b2bb-0242d4d0a230:31383"] = 0
	expectedhc["mesos-dns:mesosslave-r02-s02:myapp.98e65905-b4d3-11e5-b2bb-0242d4d0a230:31384"] = 0
	expectedhc["mesos-dns:mesosmaster-r03-s03:nginx.2a8898a8-b9a7-11e5-b2bb-0242d4d0a230:31381"] = 0
	expectedhc["mesos-dns:mesosmaster-r03-s03:myapp.98de90d1-b4d3-11e5-b2bb-0242d4d0a230:31383"] = 0
	expectedhc["mesos-dns:mesosmaster-r03-s03:myapp.98de90d1-b4d3-11e5-b2bb-0242d4d0a230:31384"] = 0
	validateHealthRecords(t, backend.HealthChecks, expectedhc)
}

func TestRegister(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.generateMesosRecords()
	backend.generateFrameworkRecords()

	for _, framework := range backend.State.Frameworks {
		backend.generateTaskRecords(framework.Tasks)
	}
	backend.Register()

	// 1x slave
	// 2x myapp
	// 1x nginx
	// 1x consul
	validateRecords(t, backend, 5)
}

func TestCleanupRecords(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	setupHealthChecks(t, backend)
	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID

	rg := &records.RecordGenerator{State: backend.State}
	backend.Reload(rg, errors.New(""))

	service := createService("REMOVEMESERVICE", "REMOVEMESERVICE", "127.0.0.1", 0, []string{})
	err := backend.Agents["127.0.0.1"].ServiceRegister(service)
	if err != nil {
		t.Error("Failed to create bogus service", err)
	}
	backend.TaskRecords[backend.SlaveIPID["127.0.0.1"]].Previous = append(backend.TaskRecords[backend.SlaveIPID["127.0.0.1"]].Previous, service)
	err = backend.Agents["127.0.0.1"].CheckRegister(&api.AgentCheckRegistration{
		ID:                "REMOVEMECHECK",
		Name:              "REMOVEMECHECK",
		ServiceID:         "mesos-dns:mesosslave-r02-s02:nginx-no-port.4266d369-b9a7-11e5-b2bb-0242d4d0a230",
		AgentServiceCheck: api.AgentServiceCheck{TTL: "500s"},
	})
	if err != nil {
		t.Error("Failed to create bogus healthcheck", err)
	}
	backend.Cleanup()
}

func TestCache(t *testing.T) {
	server, backend := recordSetup(t)
	defer server.Stop()

	// Need to do this to populate backend.SlaveIDIP
	// so we can pull appropriate slave ip mapping
	// by slave.ID
	backend.generateMesosRecords()
	backend.generateFrameworkRecords()
	setupHealthChecks(t, backend)

	for _, framework := range backend.State.Frameworks {
		backend.generateTaskRecords(framework.Tasks)
	}

	rg := &records.RecordGenerator{State: backend.State}
	backend.Reload(rg, errors.New(""))

	// 5 records ( 1x slave, 2x myapp, 1x nginx, 1x consul)
	validateRecords(t, backend, 5)

	service := createService("REMOVEMESERVICE", "REMOVEMESERVICE", "127.0.0.1", 0, []string{})
	backend.TaskRecords[backend.SlaveIPID["127.0.0.1"]].Previous = append(backend.TaskRecords[backend.SlaveIPID["127.0.0.1"]].Previous, service)
	delta := getDeltaServices(backend.TaskRecords[backend.SlaveIPID["127.0.0.1"]].Current, backend.TaskRecords[backend.SlaveIPID["127.0.0.1"]].Previous)
	if len(delta) != 1 {
		t.Error("")
	}
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
		if slave.ID == "20160107-001256-134875658-5050-27524-S3" {
			slave.PID.Host = "127.0.0.1"
		}
	}
	rg.State.Leader = "master@127.0.0.2:5050"
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

func validateStateRecords(t *testing.T, records map[string]*ConsulRecords, expectedrecs int, expected map[string]int) {
	if len(records) != expectedrecs {
		t.Error("Did not get back", expectedrecs, "records. Got back", len(records))
		for id := range records {
			t.Error("-", id)
		}
		return
	}

	for id, asr := range records {
		// Success
		if len(asr.Current) == expected[id] {
			continue
		}

		// Failure
		t.Error("Did not get back", expected[id], "state records. Got back", len(asr.Current))
		t.Error(id)
		for _, info := range asr.Current {
			t.Error(" -", info.ID, info.Name, info.Address)
		}
	}
}

func validateHealthRecords(t *testing.T, records map[string]*ConsulChecks, expected map[string]int) {
	for id, acr := range records {
		for _, info := range acr.Current {
			if len(info.Regs) == expected[info.TaskID] {
				continue
			}

			t.Error("Did not get back", expected[id], "healthcheck records. Got back", len(acr.Current))
			t.Error(" -", info.TaskID)
		}
	}
}

func setupHealthChecks(t *testing.T, backend *ConsulBackend) {
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

}
