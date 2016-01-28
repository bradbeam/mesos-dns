package consul

import (
	"log"
	"strconv"
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
	consulconfig "github.com/mesosphere/mesos-dns/resolvers/consul/config"
)

type ConsulBackend struct {
	Client *consul.Client
	Config *consul.Config
	Agents map[string]*consul.Agent
}

func New(config records.Config, errch chan error, version string) *ConsulBackend {
	cfg := consulconfig.NewConfig()
	client, err := consul.NewClient(cfg)
	if err != nil {
		errch <- err
		return &ConsulBackend{}
	}

	return &ConsulBackend{
		Client: client,
		Config: cfg,
		Agents: make(map[string]*consul.Agent),
	}

}

func (c *ConsulBackend) Reload(rg *records.RecordGenerator, err error) {
	// Get agent members
	// and initialize client connections
	c.connectAgents()

	// Going on the assumption of revamped rg structs
	// Want to create
	// leader.mesos.service.consul
	// master.mesos.service.consul
	// slave.mesos.service.consul
	// marathon.service.consul
	// task.marathon.service.consul
	// task.slave.service.consul

	//	c.insertMasterRecords(rg)
	//	c.insertSlaveRecords(rg)
}

func (c *ConsulBackend) connectAgents() error {
	// Only get LAN members
	members, err := c.Client.Agent().Members(false)
	if err != nil {
		// Do something
		return err
	}

	// Since the consul api is dumb and wont return the http port
	// we'll assume all agents are running on the same port as
	// the initially specified consul server
	port := strings.Split(c.Config.Address, ":")[1]

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
		cfg.Address = agent.Addr + ":" + port
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

func (c *ConsulBackend) insertSlaveRecords(slaves []state.Slave) {
	serviceprefix := "mesos-dns"
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

		// Add slave to the pool of slaves
		// slave.service.consul
		err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + ":" + slave.ID,
			Name:    "slave.mesos",
			Port:    port,
			Address: slave.PID.Host,
		})

		if err != nil {
			log.Println(err)
		}

		// Create a way to lookup the local slave entry
		// local.slave.service.consul
		err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + ":local-" + slave.ID,
			Name:    "local.slave",
			Port:    port,
			Address: slave.PID.Host,
		})

		if err != nil {
			log.Println(err)
		}
	}

}

/*
func (c *ConsulBackend) insertMasterRecords(rg *records.RecordGenerator) {
	// Note:
	// We'll want to look at using the TTL portion of checks
	// to take care of obsoleting old records
	serviceprefix := "mesosdns"
	for _, master := range rg.Masters {

		err := agent.ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + "-" + master.Name,
			Name:    master.Name,
			Port:    master.Port,
			Address: master.Address,
			Check:   &consul.AgentServiceCheck{},
		})
		if master.Leader {
			// Probably need to massage this a little
			err = agent.ServiceRegister(&consul.AgentServiceRegistration{
				ID:      serviceprefix + "-leader-" + master.Name,
				Name:    "leader" + master.Name,
				Port:    master.Port,
				Address: master.Address,
				Check:   &consul.AgentServiceCheck{},
			})
		}

		// Update TTL for record/service
	}

}

func (c *ConsulBackend) insertFrameworkRecords(rg *records.RecordGenerator) {
	// Note:
	// We'll want to look at using the TTL portion of checks
	// to take care of obsoleting old records
	serviceprefix := "mesosdns"
	for _, framework := range rg.Frameworks {
		err := agent.ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + "-" + framework.Name,
			Name:    framework.Name,
			Port:    framework.Port,
			Address: framework.Address,
			Check:   &consul.AgentServiceCheck{},
		})
		// Update TTL for record/service
	}

}
*/
