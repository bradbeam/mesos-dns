package consul

import (
	"github.com/mesosphere/mesos-dns/records"
)

type ConsulClient struct {
}

func New(config records.Config, errch chan error, version string) *ConsulClient {
	client := &ConsulClient{}

	return client
}

func (client *ConsulClient) Reload(rg *records.RecordGenerator, err error) {
}
