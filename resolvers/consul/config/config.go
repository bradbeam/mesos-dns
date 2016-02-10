package config

import (
	consul "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/records"
)

type Config struct {
	Agent         string
	LookupOrder   []string
	ServicePrefix string
	consul.Config
}

func NewConfig(mcfg records.Config) *Config {
	// Probably need to update this with the merging of defined config options
	configfile := mcfg.Resolvers["consul"].(Config)
	cfg := &Config{
		Agent:         configfile.Agent,
		LookupOrder:   configfile.LookupOrder,
		ServicePrefix: configfile.ServicePrefix,
		consul.Config: consul.DefaultConfig(),
	}

	if configfile.Address != "" {
		cfg.Address = configfile.Address
	}
	if configfile.Scheme != "" {
		cfg.Scheme = configfile.Scheme
	}
	if configfile.Datacenter != "" {
		cfg.Datacenter = configfile.Datacenter
	}
	if configfile.Token != "" {
		cfg.Token = configfile.Token
	}

	return cfg
}
