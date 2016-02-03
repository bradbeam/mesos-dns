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
	Agents          map[string]*consul.Agent
	AgentPort       string
	Client          *consul.Client
	Config          *consul.Config
	LookupOrder     []string
	Refresh         int
	ServicePrefix   string
	SlaveIDIP       map[string]string
	SlaveIDHostname map[string]string
	State           state.State
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
		SlaveIDHostname: make(map[string]string),
		State:           state.State{},
	}

}

func (c *ConsulBackend) Reload(rg *records.RecordGenerator, err error) {
	// Get a snapshot of state.json
	c.State = rg.State
	// Get agent members
	// and initialize client connections
	c.connectAgents()

	// Going on the assumption of revamped rg structs
	c.insertMesosRecords()
	c.insertFrameworkRecords()
	for _, framework := range rg.State.Frameworks {
		c.insertTaskRecords(framework.Tasks)
	}

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

func (c *ConsulBackend) insertMesosRecords() {
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

		// Pull out only the hostname, not the FQDN
		c.SlaveIDHostname[slave.ID] = strings.Split(slave.Hostname, ".")[0]

		if _, ok := c.Agents[slave.PID.Host]; !ok {
			log.Println("Unknown consul agent", slave.PID.Host)
			continue
		}

		service := createService(strings.Join([]string{c.ServicePrefix, slave.ID}, ":"), "mesos", slave.PID.Host, port, tags)
		err = c.Agents[slave.PID.Host].ServiceRegister(service)

		if err != nil {
			log.Println(err)
		}
	}
}

func (c *ConsulBackend) insertFrameworkRecords() {
	for _, framework := range c.State.Frameworks {

		// task, pid, name, hostname
		port, err := strconv.Atoi(framework.PID.Port)
		if err != nil {
			log.Println(err)
			continue
		}

		if _, ok := c.Agents[framework.PID.Host]; !ok {
			log.Println("Unknown consul agent", framework.PID.Host)
			continue
		}

		// Add slave to the pool of slaves
		// slave.mesos.service.consul
		service := createService(strings.Join([]string{c.ServicePrefix, framework.Name}, ":"), framework.Name, framework.PID.Host, port, []string{})
		err = c.Agents[framework.PID.Host].ServiceRegister(service)
		if err != nil {
			log.Println(err)
		}
	}
}

func (c *ConsulBackend) insertTaskRecords(tasks []state.Task) {
	for _, task := range tasks {
		if task.State != "TASK_RUNNING" {
			continue
		}
		// Might be able to use task.SlaveIP, but in testing this reutrns ""

		// Get the right consul agent to register the service with
		if _, ok := c.Agents[c.SlaveIDIP[task.SlaveID]]; !ok {
			log.Println("Unknown consul agent", task.SlaveID, "for task", task.ID)
			continue
		}

		address := c.getAddress(task)

		healthchecks := c.getHealthChecks(task)

		var services []*consul.AgentServiceRegistration

		// Create a service registration for every port
		if len(task.Ports()) == 0 {
			// Create a service registration for the base service
			services = append(services, createService(strings.Join([]string{c.ServicePrefix, c.SlaveIDHostname[task.SlaveID], task.ID}, ":"), task.Name, address, 0, []string{c.SlaveIDHostname[task.SlaveID]}))
		} else {
			for _, port := range task.Ports() {
				p, err := strconv.Atoi(port)
				if err != nil {
					log.Println("Something stupid happenend and we cant convert", port, "to int")
					continue
				}
				// Create a service registration for each port
				services = append(services, createService(strings.Join([]string{c.ServicePrefix, c.SlaveIDHostname[task.SlaveID], task.ID, port}, ":"), task.Name, address, p, []string{c.SlaveIDHostname[task.SlaveID]}))
			}
		}

		// Register Healthchecks
		for _, service := range services {
			//log.Println("Registering service", service.ID)
			err := c.Agents[c.SlaveIDIP[task.SlaveID]].ServiceRegister(service)
			if err != nil {
				log.Println(err)
				continue
			}

			for _, healthcheck := range healthchecks {

				//log.Println("Registering healthcheck", healthcheck.ID, "for service", service.ID)
				healthcheck.ServiceID = service.ID
				err = c.Agents[c.SlaveIDIP[task.SlaveID]].CheckRegister(healthcheck)
				if err != nil {
					log.Println(err)
				}
			}
		}
	}
}

func (c *ConsulBackend) Cleanup() {
	var wg sync.WaitGroup
	for _, agent := range c.Agents {
		// Set up a goroutune to handle each agent
		wg.Add(1)
		go func() {
			services, err := agent.Services()
			if err != nil {
				log.Println(err)
				wg.Done()
				return
			}

			for service, serviceinfo := range services {
				// All services registered by mesos-dns will have the
				// service prefix be c.ServicePrefix
				if c.ServicePrefix != strings.Split(service, ":")[0] {
					continue
				}
				for _, tag := range serviceinfo.Tags {
					timestamp := strings.Split(tag, "%")
					if "timestamp" != timestamp[0] {
						continue
					}
					servicets, err := time.Parse(time.RFC3339Nano, timestamp[1])
					if err != nil {
						log.Println(err)
						continue
					}
					// Allow record to be 2x time.refresh before purging it
					if time.Now().After(servicets.Add(time.Duration(c.Refresh*2) * time.Second)) {
						err := agent.ServiceDeregister(service)
						if err != nil {
							log.Println(err)
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

func (c *ConsulBackend) getHealthChecks(task state.Task) []*consul.AgentCheckRegistration {

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
