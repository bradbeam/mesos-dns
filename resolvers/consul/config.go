package consul

import (
	capi "github.com/hashicorp/consul/api"
)

type Config struct {
	LookupOrder   []string
	ServicePrefix string
	APIConfig     *capi.Config
}

func NewConfig() *Config {
	return &Config{
		LookupOrder:   []string{"docker", "netinfo", "host"},
		ServicePrefix: "mesos-dns",
		APIConfig:     capi.DefaultConfig(),
	}
}
