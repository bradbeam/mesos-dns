package consul

import (
	"net"
	"strings"
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
		Agents:    make(map[string]chan Record),
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
	cfg := capi.DefaultConfig()
	cfg.Address = strings.Join([]string{addr, port}, ":")
	cfg.Datacenter = config.Datacenter
	cfg.Scheme = config.Scheme
	cfg.Token = config.Token
	cfg.HttpClient.Timeout = time.Second * time.Duration(refresh)

	client, err := capi.NewClient(cfg)
	if err != nil {
		logging.Error.Println("Failed to create new consul client for", cfg.Address)
		return nil, err
	}

	_, err = client.Agent().Self()
	if err != nil {
		logging.Error.Println("Failed getting self for", cfg.Address)
		return nil, err
	}

	logging.VeryVerbose.Println("Connected to consul agent", cfg.Address)
	return client.Agent(), err
}

func consulAgent(member *capi.AgentMember, config *Config, records chan Record, control chan struct{}, errch chan error) {
	_, port, err := net.SplitHostPort(config.Address)
	if err != nil {
		errch <- err
		return
	}

	count := 0

	agent := &Agent{
		Healthy: false,
	}

	for {
		if count%(config.CacheRefresh*3) == 0 {
			if !agent.Healthy {
				logging.VeryVerbose.Println("Reconnecting to consul at", member.Addr, port)
				cagent, err := consulInit(config, member.Addr, port, 5)
				if err != nil {
					errch <- err
					time.Sleep(1 * time.Second)
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
				logging.VeryVerbose.Println("Skipping record update because agent isnt healthy")
				continue
			}

			// Will need to flex on record type/action
			err := agent.ConsulAgent.ServiceRegister(record.Service)
			if err != nil {
				errch <- err
			}
		case <-control:
			return
		case <-time.After(5 * time.Second):
			count += 1
		}
	}
}

func (b *Backend) Reload(rg *records.RecordGenerator) {
	mesosRecords := make(chan Record)
	frameworkRecords := make(chan Record)
	taskRecords := make(chan Record)

	go b.Dispatch(mesosRecords, frameworkRecords, taskRecords)

	go generateMesosRecords(mesosRecords, rg)
	go generateFrameworkRecords(frameworkRecords, rg)
	go generateTaskRecords(taskRecords, rg)

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

func generateMesosRecords(ch chan Record, rg *records.RecordGenerator)     {}
func generateFrameworkRecords(ch chan Record, rg *records.RecordGenerator) {}
func generateTaskRecords(ch chan Record, rg *records.RecordGenerator)      {}
