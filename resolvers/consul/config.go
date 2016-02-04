package consul

import capi "github.com/hashicorp/consul/api"

func NewConfig() *capi.Config {
	// Probably need to update this with the merging of defined config options
	return capi.DefaultConfig()
}
