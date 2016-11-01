package consul_test

import (
	"encoding/json"
	"io/ioutil"
	"sync"
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
	"github.com/mesosphere/mesos-dns/resolvers/consul"
)

const LOCALSLAVENAME string = "1234"
const LOCALSLAVEIP string = "127.0.0.1"
const LOCALSLAVEID string = "myslaveid1234"

func TestNew(t *testing.T) {
	server := startConsul(t)
	defer server.Stop()

	config := &consul.Config{
		Address:      "127.0.0.1:8500",
		CacheRefresh: 2,
	}
	errch := make(chan error)
	rg := &records.RecordGenerator{
		Config: &records.Config{
			RefreshSeconds: 1,
		},
	}
	version := ""
	logging.SetupLogs()

	var wg sync.WaitGroup

	wg.Add(1)
	go func(errch chan error) {
		err := readErrorChan(errch)
		if err == nil {
			t.Error("Connected to a nonexistant consul node?")
		} else {
			t.Log(err)
		}
		wg.Done()
	}(errch)

	t.Log("Test failure to connect to local consul agent")
	brokenbackend := consul.New(config, errch, rg, version)
	wg.Wait()

	if brokenbackend != nil {
		t.Error("Created consul backend out of magic pixie dust")
	}

	t.Log("Test successful connection to local consul agent")
	errCh := make(chan error)
	wg.Add(1)
	go func(errch chan error) {
		err := readErrorChan(errch)
		if err != nil {
			t.Error(err)
		}
		wg.Done()
	}(errCh)

	cfg := &consul.Config{
		Address:      "127.0.0.1:8500",
		CacheRefresh: 2,
	}
	cfg.Address = server.HTTPAddr

	backend := consul.New(cfg, errCh, rg, version)
	wg.Wait()
	if backend == nil {
		t.Error("Failed to create backend")
	}
	if len(backend.Agents) != 1 {
		t.Error("Failed to create backend: not enough agents")
	}

	// Cleanup -- shut down old goroutines/connections to consul
	for _, controlCh := range backend.Control {
		close(controlCh)
	}
	close(backend.ConsulKVControl)
}

func TestDispatch(t *testing.T) {
	server := startConsul(t)
	defer server.Stop()

	logging.SetupLogs()

	config := &consul.Config{
		Address:       server.HTTPAddr,
		CacheRefresh:  2,
		ServicePrefix: "mesos-dns",
	}

	errch := make(chan error)
	go func(errch chan error) {
		for err := range errch {
			if err != nil {
				t.Log(err)
			}
		}
	}(errch)

	/*
		mesosCh := make(chan consul.Record)
		frameworkCh := make(chan consul.Record)
		taskCh := make(chan consul.Record)
		counter := 0
	*/

	kvs := setupHealthChecks(t, server.HTTPAddr)
	if kvs == nil {
		t.Fatal("Failed to get KVPairs from consul")
	}

	var sj state.State
	b, err := ioutil.ReadFile("test/state.json")
	if err != nil {
		t.Fatal(err)
	} else if err = json.Unmarshal(b, &sj); err != nil {
		t.Fatal(err)
	}
	for id, slave := range sj.Slaves {
		if slave.ID == "20160107-001256-134875658-5050-27524-S3" {
			sj.Slaves[id].PID.Host = "127.0.0.1"
		}
	}
	rg := &records.RecordGenerator{
		State:  sj,
		Config: records.NewConfig(),
	}
	rg.Config.IPSources = append(rg.Config.IPSources, "label:CalicoDocker.NetworkSettings.IPAddress")

	backend := consul.New(config, errch, rg, "")
	if len(backend.Cache) != 0 {
		t.Error("Backend cache has items in it after initialization")
	}

	backend.Reload(rg)

	// IDK if there's a better way to wait for dispatch to do it's thing
	time.Sleep(5 * time.Second)

	cfg := capi.DefaultConfig()
	cfg.Address = server.HTTPAddr
	client, err := capi.NewClient(cfg)
	if err != nil {
		t.Error("Failed to create consul connection")
	}

	t.Log("Checking registered services in consul")
	services, err := client.Agent().Services()
	if err != nil {
		t.Error(err)
	}
	if len(services) != 6 {
		t.Error("Failed to get back 6 services from consul")
		for k, v := range services {
			t.Log(k)
			t.Log(v)
		}
	}

	t.Log("Checking registered checks in consul")
	checks, err := client.Agent().Checks()
	if err != nil {
		t.Error(err)
	}

	if len(checks) != 4 {
		t.Error("Failed to get back 4 checks from consul")
		for k, v := range checks {
			t.Log(k)
			t.Log(v)
		}
	}

	/*
		t.Log("Checking cache")
		if len(backend.Cache) == 0 {
			t.Error("Backend cache does not have items in it after reload")
			t.Logf("%+v", backend.Cache)
		}
	*/

	// This should be noop since we'll hit the cache
	backend.Reload(rg)
	time.Sleep(5 * time.Second)

	// Cleanup -- shut down old goroutines/connections to consul
	for _, controlCh := range backend.Control {
		close(controlCh)
	}
}

func setupBackend(t *testing.T) (*testutil.TestServer, *consul.Backend, chan error) {
	server := startConsul(t)

	config := &consul.Config{
		Address:      "127.0.0.1:8500",
		CacheRefresh: 2,
	}
	errch := make(chan error)
	rg := &records.RecordGenerator{
		Config: &records.Config{
			RefreshSeconds: 1,
		},
	}
	version := ""

	// enable debug logging
	logging.VeryVerboseFlag = true
	// Initialize logger
	logging.SetupLogs()

	config.Address = server.HTTPAddr
	backend := consul.New(config, errch, rg, version)

	if backend == nil {
		t.Error("Failed to create backend")
	}
	if len(backend.Agents) != 1 {
		t.Error("Failed to create backend: not enough agents")
	}

	return server, backend, errch

}

func startConsul(t *testing.T) *testutil.TestServer {
	// Make client config
	conf := capi.DefaultConfig()

	// Create server
	// Redirect logs to /dev/null cause we really dont care about consul agent ouput
	server := testutil.NewTestServerConfig(t, func(c *testutil.TestServerConfig) {
		c.NodeName = LOCALSLAVENAME
		c.LogLevel = "INFO"
		c.Stdout = ioutil.Discard
		c.Stderr = ioutil.Discard
	})
	conf.Address = server.HTTPAddr

	return server
}

func setupHealthChecks(t *testing.T, addr string) capi.KVPairs {
	// Post KV for consul healthchecks
	// nginx/port
	// nginx/http
	t.Log("Loading consul KV HealthChecks")
	cfg := capi.DefaultConfig()
	cfg.Address = addr
	client, err := capi.NewClient(cfg)
	if err != nil {
		t.Error("Failed to create consul connection")
		return nil
	}

	kv := client.KV()
	nport := &capi.AgentCheckRegistration{
		ID:   "nginx/port",
		Name: "nginx/port",
		AgentServiceCheck: capi.AgentServiceCheck{
			TCP:      "{IP}:80",
			Interval: "5s",
		},
	}

	nhttp := &capi.AgentCheckRegistration{
		ID:   "nginx/http",
		Name: "nginx/http",
		AgentServiceCheck: capi.AgentServiceCheck{
			HTTP:     "http://localhost",
			Interval: "5s",
		},
	}

	for _, check := range []*capi.AgentCheckRegistration{nport, nhttp} {
		b, err := json.Marshal(check)
		p := &capi.KVPair{Key: "healthchecks/" + check.ID, Value: b}
		_, err = kv.Put(p, nil)
		if err != nil {
			t.Error(err)
		}
	}

	kvs, _, err := kv.List("healthchecks/", nil)
	if err != nil {
		t.Error(err)
	}
	return kvs
}
