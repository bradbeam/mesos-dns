package consul

import (
	"net"
	"strconv"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
)

type Backend struct {
	Agents    map[string]chan Record
	Control   map[string]chan struct{}
	ErrorChan chan error
}

type Record struct {
	Address string
	SlaveID string

	Service *capi.AgentServiceRegistration
}

type Agent struct {
	Healthy     bool
	ConsulAgent *capi.Agent
}

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
		return nil
	}

	backend := &Backend{
		ErrorChan: errch,
	}

	// Iterate through all members and make sure connection is healthy
	// or initialize a new connection
	for _, member := range members {
		recordInput := make(chan Record)
		backend.Agents[member.Addr] = recordInput
		controlCh := make(chan struct{})
		go consulAgent(member, config, recordInput, controlCh, errch)
	}

	return backend
}

func consulInit(config *Config, addr string, port string, refresh int) (*capi.Agent, error) {
	return &capi.Agent{}, nil
}

func consulAgent(member *capi.AgentMember, config *Config, records chan Record, control chan struct{}, errch chan error) {
	count := 0

	agent := &Agent{
		Healthy: false,
	}

	for {
		count += 1
		if count%(config.CacheRefresh*3) == 0 {
			if !agent.Healthy {
				cagent, err := consulInit(config, member.Addr, strconv.Itoa(int(member.Port)), 5)
				if err != nil {
					errch <- err
					continue
				}
				agent.Healthy = true
				agent.ConsulAgent = cagent
			}
		}

		select {
		case record := <-records:
			// Update Consul
			if !agent.Healthy {
				continue
			}
			// Will need to flex on record type/action
			agent.ConsulAgent.ServiceRegister(record.Service)
		case <-control:
			return
		case <-time.After(1 * time.Second):
		}
	}
}

func (b *Backend) Reload() {
	mesosRecords := make(chan Record)
	frameworkRecords := make(chan Record)
	taskRecords := make(chan Record)

	go b.Dispatch(mesosRecords, frameworkRecords, taskRecords)

	go generateMesosRecords(mesosRecords)
	go generateFrameworkRecords(frameworkRecords)
	go generateTaskRecords(taskRecords)

}

func (b *Backend) Dispatch(mesoss chan Record, frameworks chan Record, tasks chan Record) {
	consulAgent := make(map[string]chan Record)
	records := make(map[string][]Record)

	for record := range mesoss {
		if ch, ok := b.Agents[record.Address]; ok {
			consulAgent[record.SlaveID] = ch
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

func generateMesosRecords(ch chan Record)     {}
func generateFrameworkRecords(ch chan Record) {}
func generateTaskRecords(ch chan Record)      {}
