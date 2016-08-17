package resolvers

import (
	"strings"

	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolvers/builtin"
	"github.com/mesosphere/mesos-dns/utils"
)

// The resolver interface is the means by which additional backends can be added
// Resolver wraps the Reload method that takes a RecordGenerator struct. The
// RecordGenerator contains the original and parsed state.json information. The
// Reload method is called on a periodic basis with updated state information.
type Resolver interface {
	Reload(rg *records.RecordGenerator)
}

// New initializes all configured backends and returns a slice of all the
// configured backends
func New(errch chan error, rg *records.RecordGenerator, version string) []Resolver {
	var resolvers []Resolver

	for k, v := range rg.Config.Backends {
		// Each backend receives their config as type interface{} or nil.
		switch strings.ToLower(k) {
		case "builtin":
			conf := builtin.NewConfig()
			utils.Merge(v, conf)
			// Print out here after the merge to show the merged config
			logging.VeryVerbose.Printf("Initializing resolver %s with config %+v\n", k, conf)
			resolvers = append(resolvers, builtin.New(version, errch, conf, rg))
		default:
			logging.Verbose.Printf("Unrecognized resolver %s", k)
		}
	}

	return resolvers
}
