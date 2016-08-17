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
	"github.com/mesosphere/mesos-dns/utils"
)

func main() {
	utils.PanicHandlers = append(utils.PanicHandlers, func(_ interface{}) {
		// by default the handler already logs the panic
		os.Exit(1)
	})

	var versionFlag bool

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
	initAuth()

	// initialize resolver
	config := records.SetConfig(*cjson)
	errch := make(chan error)

	// initialize a RecordGenerator for use by initializing resolvers
	rg := records.NewRecordGenerator(records.WithConfig(config))
	// initialize backends
	rs := resolvers.New(errch, rg, Version)

	changed := detectMasters(config.Zk, config.Masters)
	reload := time.NewTicker(time.Second * time.Duration(config.RefreshSeconds))
	zkTimeout := time.Second * time.Duration(config.ZkDetectionTimeout)
	timeout := time.AfterFunc(zkTimeout, func() {
		if zkTimeout > 0 {
			errch <- fmt.Errorf("master detection timed out after %s", zkTimeout)
		}
	})

	defer reload.Stop()
	defer utils.HandleCrash()
	for {
		select {
		case <-reload.C:
			reloadResolvers(config, errch, rs)
		case masters := <-changed:
			if len(masters) == 0 || masters[0] == "" { // no leader
				timeout.Reset(zkTimeout)
			} else {
				timeout.Stop()
			}
			logging.VeryVerbose.Printf("new masters detected: %v", masters)
			config.Masters = masters
			reloadResolvers(config, errch, rs)

			//res.SetMasters(masters)
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
		logging.Verbose.Println("No zk servers passed to detectMasters. Masters left unchanged.")
		changed <- masters
	}
	return changed
}

func reloadResolvers(config records.Config, errch chan error, rs []resolvers.Resolver) {
	rg := records.NewRecordGenerator(records.WithConfig(config))
	err := rg.ParseState(config.Masters...)

	if err != nil {
		logging.Error.Printf("Warning: Error generating records: %v; keeping old DNS state", err)
		errch <- err
	} else {
		for _, resolver := range rs {
			resolver.Reload(rg)
		}
	}
}
