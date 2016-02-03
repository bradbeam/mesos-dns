package consul

import (
	"log"
	"strconv"
	"strings"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
)

type ConsulBackend struct {
	Agents        map[string]*capi.Agent
	AgentPort     string
	Client        *capi.Client
	Config        *capi.Config
	LookupOrder   []string
	ServicePrefix string
	SlaveIDIP     map[string]string
}

func New(config capi.Config, errch chan error, rg *records.RecordGenerator, version string) *ConsulBackend {
	cfg := NewConfig()
	client, err := capi.NewClient(cfg)
	if err != nil {
		errch <- err
		return &ConsulBackend{}
	}

	// Since the consul api is dumb and wont return the http port
	// we'll assume all agents are running on the same port as
	// the initially specified consul server
	port := strings.Split(cfg.Address, ":")[1]

	return &ConsulBackend{
		Agents:        make(map[string]*capi.Agent),
		AgentPort:     port,
		Client:        client,
		Config:        cfg,
		LookupOrder:   []string{"docker", "netinfo", "host"},
		ServicePrefix: "mesos-dns",
		SlaveIDIP:     make(map[string]string),
	}

}

func (c *ConsulBackend) Reload(rg *records.RecordGenerator) {
	// Get agent members
	// and initialize client connections
	c.connectAgents()

	// Going on the assumption of revamped rg structs
	c.insertMasterRecords(rg.State.Slaves, rg.State.Leader)
	c.insertSlaveRecords(rg.State.Slaves)
	c.insertFrameworkRecords(rg.State.Frameworks)
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
		cfg := NewConfig()
		cfg.Address = agent.Addr + ":" + c.AgentPort
		client, err := capi.NewClient(cfg)
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

func (c *ConsulBackend) insertSlaveRecords(slaves []state.Slave) {
	for _, slave := range slaves {
		port, err := strconv.Atoi(slave.PID.Port)
		if err != nil {
			log.Println(err)
			continue
		}

		if _, ok := c.Agents[slave.PID.Host]; !ok {
			log.Println("Unknown consul agent", slave.PID.Host)
			continue
		}

		// We'll need this for service registration to the appropriate
		// slaves
		c.SlaveIDIP[slave.ID] = slave.PID.Host

		// Add slave to the pool of slaves
		// slave.mesos.service.consul
		err = c.Agents[slave.PID.Host].ServiceRegister(&capi.AgentServiceRegistration{
			ID:      c.ServicePrefix + ":" + slave.ID,
			Name:    "slave.mesos",
			Port:    port,
			Address: slave.PID.Host,
		})

		if err != nil {
			log.Println(err)
		}
	}
}

func (c *ConsulBackend) insertMasterRecords(slaves []state.Slave, leader string) {
	// Create a bogus Slave struct for the leader
	// master@10.10.10.8:5050
	lead := state.Slave{
		ID:       leader,
		Hostname: leader,
		PID: state.PID{
			&upid.UPID{
				Host: strings.Split(strings.Split(leader, "@")[1], ":")[0],
				Port: strings.Split(strings.Split(leader, "@")[1], ":")[1],
			},
		},
		Attrs: state.Attributes{
			Master: "true",
		},
		Active: true,
	}
	slaves = append(slaves, lead)
	for _, slave := range slaves {
		if slave.Attrs.Master == "false" {
			// Slave node
			continue
		}
		port, err := strconv.Atoi(slave.PID.Port)
		if err != nil {
			log.Println(err)
			continue
		}

		if _, ok := c.Agents[slave.PID.Host]; !ok {
			log.Println("Unknown consul agent", slave.PID.Host)
			continue
		}

		if slave.ID == leader {
			err = c.Agents[slave.PID.Host].ServiceRegister(&capi.AgentServiceRegistration{
				ID:      c.ServicePrefix + ":" + slave.ID,
				Name:    "leader.mesos",
				Port:    port,
				Address: slave.PID.Host,
			})

		} else {
			// Add slave to the pool of masters
			// master.mesos.service.consul
			err = c.Agents[slave.PID.Host].ServiceRegister(&capi.AgentServiceRegistration{
				ID:      c.ServicePrefix + ":" + slave.ID,
				Name:    "master.mesos",
				Port:    port,
				Address: slave.PID.Host,
			})

			if err != nil {
				log.Println(err)
			}
		}
	}
}

func (c *ConsulBackend) insertFrameworkRecords(frameworks []state.Framework) {
	for _, framework := range frameworks {

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
		err = c.Agents[framework.PID.Host].ServiceRegister(&capi.AgentServiceRegistration{
			ID:      c.ServicePrefix + ":" + framework.Name,
			Name:    framework.Name,
			Port:    port,
			Address: framework.PID.Host,
		})

		if err != nil {
			log.Println(err)
		}
	}
}

func (c *ConsulBackend) insertTaskRecords(framework string, tasks []state.Task) {
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

		// Create a service registration for every port
		for _, port := range task.Ports() {
			//log.Println("Registering task:", task.ID, address, port)
			p, err := strconv.Atoi(port)
			if err != nil {
				log.Println("Something stupid happenend and we cant convert", port, "to int")
				continue
			}
			err = c.Agents[c.SlaveIDIP[task.SlaveID]].ServiceRegister(&capi.AgentServiceRegistration{
				ID:      strings.Join([]string{c.ServicePrefix, task.ID, port}, ":"),
				Name:    strings.Join([]string{task.Name, framework}, "."),
				Port:    p,
				Address: address,
			})
			if err != nil {
				log.Println(err)
			}
		}
	}
}
