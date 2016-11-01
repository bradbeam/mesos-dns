package consul

import (
	"net"
	"strings"
	"sync"
	"time"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
)

type Backend struct {
	Agents    map[string]chan Record
	Config    *Config
	Control   map[string]chan struct{}
	ConsulKV  chan capi.KVPairs
	ErrorChan chan error

	sync.Mutex
	Cache map[string][]Record
	count int
}

type Record struct {
	Action  string
	Address string
	SlaveID string

	Service *capi.AgentServiceRegistration
	Check   *capi.AgentCheckRegistration
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

	client, err := consulInit(config, addr, port, rg.Config.RefreshSeconds)
	if err != nil {
		errch <- err
		return nil
	}

	// Only get LAN members
	members, err := client.Agent().Members(false)
	if err != nil {
		errch <- err
		return nil
	}

	backend := &Backend{
		ErrorChan: errch,
		Agents:    make(map[string]chan Record),
		Cache:     make(map[string][]Record),
		Config:    config,
		Control:   make(map[string]chan struct{}),
	}

	kvCh := make(chan capi.KVPairs)
	go pollConsulKVHC(client, config, kvCh, errch)
	backend.ConsulKV = kvCh

	// Iterate through all members and make sure connection is healthy
	// or initialize a new connection
	for _, member := range members {
		recordInput := make(chan Record)
		backend.Agents[member.Addr] = recordInput
		controlCh := make(chan struct{})
		backend.Control[member.Addr] = controlCh
		go consulAgent(member, config, recordInput, controlCh, errch)
	}

	return backend
}

func consulInit(config *Config, addr string, port string, refresh int) (*capi.Client, error) {
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
	return client, err
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

	// go maintainConsul()

	for {
		if count%(config.CacheRefresh*3) == 0 {
			if !agent.Healthy {
				logging.VeryVerbose.Println("Reconnecting to consul at", member.Addr, port)
				client, err := consulInit(config, member.Addr, port, 5)
				if err != nil {
					errch <- err
					time.Sleep(1 * time.Second)
					continue
				}
				agent.Healthy = true
				agent.ConsulAgent = client.Agent()
			}
		}

		select {
		case record := <-records:
			// Update Consul
			if !agent.Healthy {
				logging.VeryVerbose.Println("Skipping record update because agent isnt healthy")
				continue
			}

			switch record.Action {
			case "add":
				if record.Service != nil {
					logging.VeryVerbose.Println("Registering service", record.Service.ID)
					err := agent.ConsulAgent.ServiceRegister(record.Service)
					if err != nil {
						agent.Healthy = false
						errch <- err
					}
				}
				if record.Check != nil {
					logging.VeryVerbose.Println("Registering check", record.Check.ID)
					err := agent.ConsulAgent.CheckRegister(record.Check)
					if err != nil {
						agent.Healthy = false
						errch <- err
					}
				}
			case "remove":
				if record.Service != nil {
					logging.VeryVerbose.Println("DeRegistering service", record.Service.ID)
					err := agent.ConsulAgent.ServiceDeregister(record.Service.ID)
					if err != nil {
						agent.Healthy = false
						errch <- err
					}
				}
				if record.Check != nil {
					logging.VeryVerbose.Println("DeRegistering check", record.Check.ID)
					err := agent.ConsulAgent.CheckDeregister(record.Check.ID)
					if err != nil {
						agent.Healthy = false
						errch <- err
					}
				}
			}
		case <-control:
			return
		case <-time.After(5 * time.Second):
			count += 1
		}
	}
}

func (b *Backend) Reload(rg *records.RecordGenerator) {
	// Data channels for generated ServiceRegistrations
	mesosRecords := make(chan Record)
	frameworkRecords := make(chan Record)
	taskRecords := make(chan Record)

	// Metadata Channels to allow frameworks/tasks to look up
	// various slave identification
	mesosFrameworks := make(chan map[string]string)
	mesosTasks := make(chan map[string]SlaveInfo)

	b.Lock()
	b.count++
	b.Unlock()

	go b.Dispatch(mesosRecords, frameworkRecords, taskRecords)

	go generateMesosRecords(mesosRecords, rg, b.Config.ServicePrefix, mesosFrameworks, mesosTasks)
	go generateFrameworkRecords(frameworkRecords, rg, b.Config.ServicePrefix, mesosFrameworks)
	go generateTaskRecords(taskRecords, rg, b.Config.ServicePrefix, mesosTasks, b.ConsulKV)

}

func (b *Backend) Dispatch(mesoss chan Record, frameworks chan Record, tasks chan Record) {
	consulAgent := make(map[string]chan Record)
	records := make(map[string][]Record)
	slaveLookup := make(map[string]string)

	// Do some additional setup/initialization
	for record := range mesoss {
		if ch, ok := b.Agents[record.Service.Address]; ok {
			consulAgent[record.SlaveID] = ch
			slaveLookup[record.Service.Address] = record.SlaveID
		}

		b.Lock()
		if _, ok := b.Cache[record.SlaveID]; !ok {
			b.Cache[record.SlaveID] = []Record{}
		}
		b.Unlock()

		records[record.SlaveID] = append(records[record.SlaveID], record)
	}

	// Create []Record to send to each agent
	// TODO find framework records
	for record := range frameworks {
		// We'll look up slave by IP because frameworks aren't tied to a
		// slave :(
		if slaveid, ok := slaveLookup[record.Address]; ok {
			records[slaveid] = append(records[slaveid], record)
		}

		// Discard record if we cant identify a slave to associate it with
		continue
	}
	for record := range tasks {
		records[record.SlaveID] = append(records[record.SlaveID], record)
	}

	for slaveid, agentCh := range consulAgent {
		// Filter the list of []Record we sent to each consul agent
		// to be only the differences between this iteration and last
		delta := getDeltaRecords(b.Cache[slaveid], records[slaveid], "add")
		delta = append(delta, getDeltaRecords(records[slaveid], b.Cache[slaveid], "remove")...)

		go updateConsul(delta, agentCh)
	}

	// Save off records for cache comparison
	// Check to see if we need to drop/refresh our cache
	b.Lock()
	if b.count%b.Config.CacheRefresh == 0 {
		b.Cache = make(map[string][]Record)
	} else {
		b.Cache = records
	}
	b.Unlock()
}

func updateConsul(records []Record, agent chan Record) {
	for _, record := range records {
		agent <- record
	}
}

func pollConsulKVHC(client *capi.Client, config *Config, kvCh chan capi.KVPairs, errch chan error) {
	var kvs capi.KVPairs
	var err error
	count := 0
	for {
		count += 1
		// Always send a value through to make sure we dont make tasks wait on us

		if count%config.CacheRefresh == 1 {
			kvs, _, err = client.KV().List("healthchecks/", nil)
			if err != nil {
				errch <- err
				continue
			}
		}

		kvCh <- kvs
		time.Sleep(time.Second * time.Duration(config.CacheRefresh))
	}
}
