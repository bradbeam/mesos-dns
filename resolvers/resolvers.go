package resolvers

import (
	"strings"

	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolvers/bind"
	"github.com/mesosphere/mesos-dns/resolvers/builtin"
	"github.com/mesosphere/mesos-dns/resolvers/consul"
)

type Resolver interface {
	Reload(rg *records.RecordGenerator)
}

func New(config records.Config, errch chan error, version string) []Resolver {
    var conf interface{}
    var resolvers []Resolver

	for _, rType := range config.Resolvers {
        // Each backend receives their config as type interface{} or nil.
        switch strings.ToLower(rType) {
		case "consul":
            conf = config.ResolversConf["consul"]
		    resolvers = append(resolvers, consul.New(conf, errch, version))
		case "builtin":
            conf = config.ResolversConf["builtin"]
		    resolvers = append(resolvers, builtin.New(conf, errch, version))
		case "bind":
            conf = config.ResolversConf["bind"]
			resolvers = append(resolvers, bind.New(conf, errch, version))
		}
	}

	return resolvers
}
