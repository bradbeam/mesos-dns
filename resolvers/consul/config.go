package consul

import consul "github.com/hashicorp/consul/api"

type Config consul.Config

func NewConfig() *consul.Config {
	// Probably need to update this with the merging of defined config options
	return consul.DefaultConfig()
}
