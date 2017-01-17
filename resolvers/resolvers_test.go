package resolvers

import (
	"testing"
	"time"

	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
)

func TestResolvers(t *testing.T) {
	t.Log("Attempting to read config from ../factories/valid.json")
	c, err := records.ReadConfig("../factories/valid.json")
	if err != nil {
		t.Fatal("Error reading config file:", err)
	}

	// Initialize logging
	logging.SetupLogs()

	// First test with default values
	//t.Log("Initializing Resolvers with default config")
	//if err := CreateResolvers(c); err != nil {
	//	t.Errorf("Error returned from resolvers: %v", err)
	//}

	tcs := []testConfig{
		{nil, true},
		{map[string]interface{}{
			"DNSOn":      false,
			"ExternalOn": false,
			"HTTPOn":     false},
			true},
		{map[string]interface{}{
			"NotReal": "fake, fake",
			"Port":    25353},
			false},
	}

	for i, tc := range tcs {
		t.Logf("Initializitng iteration %v with 'builtin' config %v", i, tc.Settings)
		if tc.Settings != nil {
			c.Resolvers = map[string]interface{}{"builtin": tc.Settings}
		} else {
			c.Resolvers = nil
		}
		if err := CreateResolvers(c); err != nil {
			t.Fatalf("Error returned from resolvers: %v", err)
		}
	}
}

type testConfig struct {
	Settings interface{}
	Valid    bool
}

func CreateResolvers(config *records.Config) error {
	errch := make(chan error)
	sillych := make(chan error)
	version := "test"

	rg := records.NewRecordGenerator(config)
	rg.Config = config

	// Read from chans
	go func(errCh chan error, sillyCh chan error) {
		// Initialization errors should show in well under 10 seconds
		timeout := time.NewTicker(time.Second * time.Duration(10))
		for {
			select {
			case <-timeout.C:
				sillyCh <- nil
			case err := <-errch:
				sillyCh <- err
			}
		}
	}(errch, sillych)

	New(errch, rg, version)

	return <-sillych

}
