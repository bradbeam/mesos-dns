package resolvers

import (
	"strings"

	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolvers/bind"
	"github.com/mesosphere/mesos-dns/resolvers/builtin"
	"github.com/mesosphere/mesos-dns/resolvers/consul"
)

type Resolver interface {
	Reload(rg *records.RecordGenerator, err error)
	// Probably want to add a config tostring
}

func New(config records.Config, errch chan error, version string) []Resolver {
	var resolvers []Resolver

	for _, rType := range config.Resolvers {
		switch strings.ToLower(rType) {
		case "consul":
			resolvers = append(resolvers, consul.New(config, errch, version))
		case "builtin":
			resolvers = append(resolvers, builtin.New(config, errch, version))
		case "bind":
			resolvers = append(resolvers, bind.New(config, errch, version))
		}
	}

	return resolvers
}
