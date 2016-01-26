package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mesos/mesos-go/detector"
	"github.com/mesosphere/mesos-dns/detect"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolvers"
	"github.com/mesosphere/mesos-dns/util"
)

func main() {
	var versionFlag bool

	util.PanicHandlers = append(util.PanicHandlers, func(_ interface{}) {
		// by default the handler already logs the panic
		os.Exit(1)
	})

	// parse flags
	cjson := flag.String("config", "config.json", "path to config file (json)")
	flag.BoolVar(&versionFlag, "version", false, "output the version")
	flag.Parse()

	// -version
	if versionFlag {
		fmt.Println(Version)
		os.Exit(0)
	}

	// initialize logging
	logging.SetupLogs()

	errch := make(chan error)
	config := records.SetConfig(*cjson)
	reload := time.NewTicker(time.Second * time.Duration(config.RefreshSeconds))
	zkTimeout := time.Second * time.Duration(config.ZkDetectionTimeout)
	timeout := time.AfterFunc(zkTimeout, func() {
		if zkTimeout > 0 {
			errch <- fmt.Errorf("master detection timed out after %s", zkTimeout)
		}
	})

	// initialize backends
	changed := detectMasters(config.Zk, config.Masters)
	masters := append([]string{""}, config.Masters...)
	resolvers := resolvers.New(config, errch, Version)

	defer reload.Stop()
	defer util.HandleCrash()
	// Main event loop
	for {
		select {
		case <-reload.C:
			rg := records.NewRecordGenerator(time.Duration(config.StateTimeoutSeconds) * time.Second)
			err := rg.ParseState(config, masters...)

			for _, resolver := range resolvers {
				// TODO: Passing an error is probably not the best way to do this
				resolver.Reload(rg, err)
			}
		case masters = <-changed:
			if len(masters) == 0 || masters[0] == "" { // no leader
				timeout.Reset(zkTimeout)
			} else {
				timeout.Stop()
			}
			logging.VeryVerbose.Printf("new masters detected: %v", masters)

			rg := records.NewRecordGenerator(time.Duration(config.StateTimeoutSeconds) * time.Second)
			err := rg.ParseState(config, masters...)

			for _, resolver := range resolvers {
				resolver.Reload(rg, err)
			}
		case err := <-errch:
			logging.Error.Fatal(err)
		}
	}
}

func detectMasters(zk string, masters []string) <-chan []string {
	changed := make(chan []string, 1)
	if zk != "" {
		logging.Verbose.Println("Starting master detector for ZK ", zk)
		if md, err := detector.New(zk); err != nil {
			log.Fatalf("failed to create master detector: %v", err)
		} else if err := md.Detect(detect.NewMasters(masters, changed)); err != nil {
			log.Fatalf("failed to initialize master detector: %v", err)
		}
	} else {
		changed <- masters
	}
	return changed
}
