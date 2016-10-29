package consul_test

import (
	"io/ioutil"
	"strconv"
	"sync"
	"testing"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/testutil"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
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
	backend := consul.New(config, errch, rg, version)
	wg.Wait()

	if backend != nil {
		t.Error("Created consul backend out of magic pixie dust")
	}

	t.Log("Test successful connection to local consul agent")
	wg.Add(1)
	go func(errch chan error) {
		err := readErrorChan(errch)
		if err != nil {
			t.Error(err)
		}
		wg.Done()
	}(errch)

	config.Address = server.HTTPAddr
	backend = consul.New(config, errch, rg, version)
	wg.Wait()
	if backend == nil {
		t.Error("Failed to create backend")
	}
	if len(backend.Agents) != 1 {
		t.Error("Failed to create backend: not enough agents")
	}
}

func TestDispatch(t *testing.T) {
	server, backend, errch := setupBackend(t)
	defer server.Stop()

	go func(errch chan error) {
		for err := range errch {
			if err != nil {
				t.Log(err)
			}
		}
	}(errch)

	mesosCh := make(chan consul.Record)
	frameworkCh := make(chan consul.Record)
	taskCh := make(chan consul.Record)

	counter := 0
	for _, channel := range []chan consul.Record{mesosCh, frameworkCh, taskCh} {
		record := consul.Record{
			Address: LOCALSLAVEIP,
			SlaveID: LOCALSLAVEID,
			Service: &capi.AgentServiceRegistration{
				ID:      "dispatchtest" + strconv.Itoa(counter),
				Name:    "dispatchtest",
				Address: LOCALSLAVEIP,
			},
		}

		go func(ch chan consul.Record, rec consul.Record) {
			ch <- rec
			close(ch)
		}(channel, record)
		counter++
	}

	go backend.Dispatch(mesosCh, frameworkCh, taskCh)

	// IDK if there's a better way to wait for dispatch to do it's thing
	time.Sleep(5 * time.Second)
	cfg := capi.DefaultConfig()
	cfg.Address = server.HTTPAddr
	client, err := capi.NewClient(cfg)
	if err != nil {
		t.Error("Failed to create consul connection")
	}

	services, err := client.Agent().Services()
	if err != nil {
		t.Error(err)
	}
	if len(services) != 4 {
		t.Error("Failed to get back 4 services from consul")
		for k, v := range services {
			t.Log(k)
			t.Log(v)
		}
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
	//logging.VeryVerboseFlag = true
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
