package consul

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
	consulconfig "github.com/mesosphere/mesos-dns/resolvers/consul/config"
)

type ConsulBackend struct {
	Agents                map[string]*consul.Agent
	AgentPort             string
	Client                *consul.Client
	Config                *consul.Config
	LookupOrder           []string
	Refresh               int
	ServicePrefix         string
	SlaveIDIP             map[string]string
	SlaveIPID             map[string]string
	SlaveIDHostname       map[string]string
	State                 state.State
	StateFrameworkRecords map[string][]*consul.AgentServiceRegistration
	StateMesosRecords     map[string][]*consul.AgentServiceRegistration
	StateTaskRecords      map[string][]*consul.AgentServiceRegistration
	StateHealthChecks     map[string][]*AgentCheckRegistrationSlave
}

type AgentCheckRegistrationSlave struct {
	TaskID string
	Regs   []*consul.AgentCheckRegistration
}

func New(config records.Config, errch chan error, version string) *ConsulBackend {
	cfg := consulconfig.NewConfig()
	client, err := consul.NewClient(cfg)
	if err != nil {
		errch <- err
		return &ConsulBackend{}
	}

	// Since the consul api is dumb and wont return the http port
	// we'll assume all agents are running on the same port as
	// the initially specified consul server
	port := strings.Split(cfg.Address, ":")[1]

	return &ConsulBackend{
		Agents:          make(map[string]*consul.Agent),
		AgentPort:       port,
		Client:          client,
		Config:          cfg,
		LookupOrder:     []string{"docker", "netinfo", "host"},
		Refresh:         5,
		ServicePrefix:   "mesos-dns",
		SlaveIDIP:       make(map[string]string),
		SlaveIPID:       make(map[string]string),
		SlaveIDHostname: make(map[string]string),
		State:           state.State{},
		StateFrameworkRecords: make(map[string][]*consul.AgentServiceRegistration),
		StateMesosRecords:     make(map[string][]*consul.AgentServiceRegistration),
		StateTaskRecords:      make(map[string][]*consul.AgentServiceRegistration),
		StateHealthChecks:     make(map[string][]*AgentCheckRegistrationSlave),
	}

}

func (c *ConsulBackend) Reload(rg *records.RecordGenerator, err error) {
	// Get a snapshot of state.json
	c.State = rg.State
	// Get agent members
	// and initialize client connections
	c.connectAgents()

	// Generate all of the necessary records
	c.generateMesosRecords()
	c.generateFrameworkRecords()
	for _, framework := range rg.State.Frameworks {
		c.generateTaskRecords(framework.Tasks)
	}

	// Register records with consul
	c.Register()

	// Clean up old/orphaned records
	c.Cleanup()
}

func (c *ConsulBackend) connectAgents() error {
	// Only get LAN members
	members, err := c.Client.Agent().Members(false)
	if err != nil {
		// Do something
		return err
	}

	for _, agent := range members {
		// Test connection to each agent and reconnect as needed
		if _, ok := c.Agents[agent.Addr]; ok {
			_, err := c.Agents[agent.Addr].Self()
			if err == nil {
				continue
			} else {
				return err
			}
		}
		cfg := consulconfig.NewConfig()
		cfg.Address = agent.Addr + ":" + c.AgentPort
		client, err := consul.NewClient(cfg)
		if err != nil {
			// How do we want to handle consul agent not being responsive
			return err
		}

		// Do a sanity check that we are connected to agent
		_, err = client.Agent().Self()
		if err != nil {
			// Dump agent?
			return err
		} else {
			c.Agents[agent.Addr] = client.Agent()
		}

	}

	return nil
}

func (c *ConsulBackend) generateMesosRecords() {
	// Create a bogus Slave struct for the leader
	// master@10.10.10.8:5050
	lead := state.Slave{
		ID:       c.State.Leader,
		Hostname: c.State.Leader,
		PID: state.PID{
			&upid.UPID{
				Host: strings.Split(strings.Split(c.State.Leader, "@")[1], ":")[0],
				Port: strings.Split(strings.Split(c.State.Leader, "@")[1], ":")[1],
			},
		},
		Attrs: state.Attributes{
			Master: "true",
		},
		Active: true,
	}

	slaves := c.State.Slaves
	slaves = append(slaves, lead)
	for _, slave := range slaves {
		tags := []string{"slave", c.SlaveIDHostname[slave.ID]}

		if slave.Attrs.Master == "true" {
			tags = append(tags, "master")
		}
		if slave.ID == lead.ID {
			tags = append(tags, "leader")
		}

		port, err := strconv.Atoi(slave.PID.Port)
		if err != nil {
			log.Println(err)
			continue
		}

		// We'll need this for service registration to the appropriate
		// slaves
		c.SlaveIDIP[slave.ID] = slave.PID.Host
		c.SlaveIPID[slave.PID.Host] = slave.ID

		// Pull out only the hostname, not the FQDN
		c.SlaveIDHostname[slave.ID] = strings.Split(slave.Hostname, ".")[0]

		c.StateMesosRecords[slave.ID] = append(c.StateMesosRecords[slave.ID], createService(strings.Join([]string{c.ServicePrefix, slave.ID}, ":"), "mesos", slave.PID.Host, port, tags))
	}
}

func (c *ConsulBackend) generateFrameworkRecords() {
	for _, framework := range c.State.Frameworks {

		// task, pid, name, hostname
		port, err := strconv.Atoi(framework.PID.Port)
		if err != nil {
			log.Println(err)
			continue
		}

		// Silly
		var slaveid string
		for id, hostname := range c.SlaveIDHostname {
			if hostname == strings.Split(framework.Hostname, ".")[0] {
				slaveid = id
			}
		}

		if slaveid == "" {
			log.Println("Unable to find slave for", framework.Name)
			continue
		}

		// Add slave to the pool of slaves
		// slave.mesos.service.consul
		c.StateFrameworkRecords[slaveid] = append(c.StateFrameworkRecords[framework.PID.Host], createService(strings.Join([]string{c.ServicePrefix, framework.Name}, ":"), framework.Name, framework.PID.Host, port, []string{}))
	}
}

func (c *ConsulBackend) generateTaskRecords(tasks []state.Task) {
	for _, task := range tasks {
		if task.State != "TASK_RUNNING" {
			continue
		}
		// Might be able to use task.SlaveIP, but in testing this reutrns ""

		address := c.getAddress(task)

		// Create a service registration for every port
		if len(task.Ports()) == 0 {

			// Create a service registration for the base service
			id := strings.Join([]string{c.ServicePrefix, c.SlaveIDHostname[task.SlaveID], task.ID}, ":")
			c.StateTaskRecords[task.SlaveID] = append(c.StateTaskRecords[task.SlaveID], createService(id, task.Name, address, 0, []string{c.SlaveIDHostname[task.SlaveID]}))
			healthchecks := c.getHealthChecks(task, id)
			c.StateHealthChecks[task.SlaveID] = append(c.StateHealthChecks[task.SlaveID], &AgentCheckRegistrationSlave{
				TaskID: id,
				Regs:   healthchecks,
			})
		} else {

			// Create a service registration for each port
			for _, port := range task.Ports() {
				p, err := strconv.Atoi(port)
				if err != nil {
					log.Println("Something stupid happenend and we cant convert", port, "to int")
					continue
				}
				id := strings.Join([]string{c.ServicePrefix, c.SlaveIDHostname[task.SlaveID], task.ID, port}, ":")
				c.StateTaskRecords[task.SlaveID] = append(c.StateTaskRecords[task.SlaveID], createService(id, task.Name, address, p, []string{c.SlaveIDHostname[task.SlaveID]}))
				healthchecks := c.getHealthChecks(task, id)
				c.StateHealthChecks[task.SlaveID] = append(c.StateHealthChecks[task.SlaveID], &AgentCheckRegistrationSlave{
					TaskID: id,
					Regs:   healthchecks,
				})
			}
		}
	}
}

func (c *ConsulBackend) Register() {

	// TODO: Review this to make sure it makes sense
	// Seems pretty friggin complicated and UGLY
	var wg sync.WaitGroup

	for agentid, _ := range c.Agents {
		// Launch a goroutine per agent
		wg.Add(1)
		go func() {
			for _, services := range [][]*consul.AgentServiceRegistration{c.StateMesosRecords[c.SlaveIPID[agentid]], c.StateFrameworkRecords[c.SlaveIPID[agentid]], c.StateTaskRecords[c.SlaveIPID[agentid]]} {
				for _, service := range services {
					// Register the service
					err := c.Agents[agentid].ServiceRegister(service)
					if err != nil {
						log.Println("Failed to register service", err)
						continue
					}

					// Register any healthchecks for the service
					for _, hc := range c.StateHealthChecks[c.SlaveIPID[agentid]] {
						if hc.TaskID != service.ID {
							continue
						}
						for _, hcreg := range hc.Regs {
							err = c.Agents[agentid].CheckRegister(hcreg)
							if err != nil {
								log.Println("Failed to register healthcheck for service", service.ID, err)
								continue
							}
						}

					}
				}
			}

			wg.Done()
		}()
		wg.Wait()
	}
}

func (c *ConsulBackend) Cleanup() {
	var wg sync.WaitGroup
	for agentid, agent := range c.Agents {
		// Set up a goroutune to handle each agent
		wg.Add(1)
		go func() {
			// Pull back services registered with agent
			services, err := agent.Services()
			if err != nil {
				log.Println(err)
				wg.Done()
				return
			}

			// Pull back healthchecks registered with agent
			checks, err := agent.Checks()
			if err != nil {
				log.Println(err)
				wg.Done()
				return
			}

			for service, info := range services {
				if service == "consul" {
					continue
				}
				found := false
				for _, stateservices := range [][]*consul.AgentServiceRegistration{c.StateMesosRecords[c.SlaveIPID[agentid]], c.StateFrameworkRecords[c.SlaveIPID[agentid]], c.StateTaskRecords[c.SlaveIPID[agentid]]} {
					// All services registered by mesos-dns will have the
					// service prefix be c.ServicePrefix
					if c.ServicePrefix != strings.Split(service, ":")[0] {
						continue
					}

					for _, stateservice := range stateservices {
						if info.ID == stateservice.ID {
							found = true
							break
						}
					}

					if found {
						break
					}
				}

				if !found {
					log.Println("Deregistering service", info.ID)
					err := agent.ServiceDeregister(info.ID)
					if err != nil {
						log.Println("Failed to deregister", info.ID, "from agent", agentid)
					}
				}

				// Check healthchecks
				for checkid, check := range checks {
					foundcheck := false
					if check.ServiceID != info.ID {
						continue
					}

					for _, statechecks := range c.StateHealthChecks[c.SlaveIPID[agentid]] {
						if statechecks.TaskID != info.ID {
							continue
						}
						for _, stateregs := range statechecks.Regs {
							if stateregs.ID == check.CheckID {
								foundcheck = true
								break
							}
						}

						if foundcheck {
							break
						}
					}
					if !foundcheck {
						log.Println("Deregistering check", checkid)
						err := agent.CheckDeregister(check.CheckID)
						if err != nil {
							log.Println("Failed to deregister check", checkid)
						}
					}
				}
			}
			wg.Done()
		}()
		wg.Wait()
	}
}

func (c *ConsulBackend) getAddress(task state.Task) string {

	var address string
	for _, lookup := range c.LookupOrder {
		lookupkey := strings.Split(lookup, ":")
		switch lookupkey[0] {
		case "mesos":
			address = task.IP("mesos")
		case "docker":
			address = task.IP("docker")
		case "netinfo":
			address = task.IP("netinfo")
		case "host":
			address = task.IP("host")
		case "label":
			if len(lookupkey) != 2 {
				log.Println("Lookup order label is not in proper format `label:labelname`")
				continue
			}
			addresses := state.StatusIPs(task.Statuses, state.Labels(lookupkey[1]))
			if len(addresses) > 0 {
				address = addresses[0]
			}
		}

		if address != "" {
			break
		}
	}

	// If still empty, set to host IP
	if address == "" {
		address = c.SlaveIDIP[task.SlaveID]
	}

	return address
}

func (c *ConsulBackend) getHealthChecks(task state.Task, id string) []*consul.AgentCheckRegistration {

	// Look up KV for defined healthchecks
	kv := c.Client.KV()
	var hc []*consul.AgentCheckRegistration
	for _, label := range task.Labels {
		if label.Key != "ConsulHealthCheckKeys" {
			continue
		}
		for _, endpoint := range strings.Split(label.Value, ",") {
			pair, _, err := kv.Get("healthchecks/"+endpoint, nil)
			// If key does not exist in consul, pair is nil
			if pair == nil || err != nil {
				log.Println("Healthcheck healthchecks/"+endpoint, "not found in consul, skipping")
				continue
			}
			check := &consul.AgentCheckRegistration{}
			err = json.Unmarshal(pair.Value, check)
			if err != nil {
				log.Println(err)
				continue
			}
			check.ID = strings.Join([]string{check.ID, id}, ":")
			check.ServiceID = id
			hc = append(hc, check)
		}
	}
	// Maybe keep this in for debug logging
	//log.Println("Found", len(hc), "healthchecks for", task.Name)

	return hc

}

func createService(id string, name string, address string, port int, tags []string) *consul.AgentServiceRegistration {
	timestamp := time.Now().Format(time.RFC3339Nano)
	asr := &consul.AgentServiceRegistration{
		ID:      id,
		Name:    name,
		Address: address,
		Tags:    append([]string{"timestamp%" + timestamp}, tags...),
	}
	if port > 0 {
		asr.Port = port
	}

	return asr
}
