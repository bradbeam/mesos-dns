package consul

import (
	"net"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
)

type Backend struct {
	Agents    map[string]chan Record
	ErrorChan chan error
}

type Records struct{}

func New(config *Config, errch chan error, rg *records.RecordGenerator, version string) *Backend {
	addr, port, err := net.SplitHostPort(config.Address)
	if err != nil {
		logging.Error.Println("Failed to get consul host:port info")
		return nil
	}

	agent, err := consulInit(config, addr, port, rg.Config.RefreshSeconds)
	if err != nil {
		errch <- err
		return nil
	}

	// Only get LAN members
	members, err := agent.Members(false)
	if err != nil {
		errch <- err
		return
	}

	backend := &Backend{
		ErrorChan: errch,
	}

	// Iterate through all members and make sure connection is healthy
	// or initialize a new connection
	for _, member := range members {
		recordInput := make(chan Record)
		backend.Agents[member.Addr] = recordInput
		go consulAgent(recordInput)
	}

	return backend
}

func consulInit(config *Config, addr string, port string, refresh int) *capi.Agent {
	return &capi.Agent{}
}

func consulAgent(records chan Record) {

}

func (b *Backend) Reload() {
	mesosRecords := make(chan Record)
	frameworkRecords := make(chan Record)
	taskRecords := make(chan Record)

	go Dispatch(mesosRecords, frameworkRecords, taskRecords)

	go generateMesosRecords(mesosRecords)
	go generateFrameworkRecords(frameworkRecords)
	go generateTaskRecords(taskRecords)

}

func (b *Backend) Dispatch(mesoss chan Record, frameworks chan Record, tasks chan Record, errch chan error) {
	consulAgent := make(map[string]chan Record)
	records := make(map[string][]Record)

	for record := range mesoss {
		if ch, ok := b.Agents[record.Address]; ok {
			if agent.Healthy {
				consulAgent[record.SlaveID] = ch
			}
		}
		records[record.SlaveID] = append(records[record.SlaveID], record)
	}
	for record := range frameworks {
		records[record.SlaveID] = append(records[record.SlaveID], record)
	}
	for record := range tasks {
		records[record.SlaveID] = append(records[record.SlaveID], record)
	}

	for slaveid, agentCh := range consulAgent {
		go updateConsul(records[slaveid], agentCh)
	}

	// Save off records for cache comparison
}

func updateConsul(records []Record, agent chan Record) {
	for _, record := range records {
		agent <- record
	}
}
