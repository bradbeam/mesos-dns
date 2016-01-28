package consul

import (
	"log"
	"strconv"
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
	consulconfig "github.com/mesosphere/mesos-dns/resolvers/consul/config"
)

type ConsulBackend struct {
	Client    *consul.Client
	AgentPort string
	Config    *consul.Config
	Agents    map[string]*consul.Agent
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
		Client:    client,
		Config:    cfg,
		AgentPort: port,
		Agents:    make(map[string]*consul.Agent),
	}

}

func (c *ConsulBackend) Reload(rg *records.RecordGenerator, err error) {
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

func (c *ConsulBackend) insertSlaveRecords(slaves []state.Slave) {
	serviceprefix := "mesos-dns"
	for _, slave := range slaves {
		if slave.Attrs.Master == "true" {
			// Master node, so skip
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

		// Add slave to the pool of slaves
		// slave.mesos.service.consul
		err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + ":" + slave.ID,
			Name:    "slave.mesos",
			Port:    port,
			Address: slave.PID.Host,
		})

		if err != nil {
			log.Println(err)
		}

		// Need to rethink this
		// Maybe <hostname>.slave.mesos? idk
		// but record will be distributed among consul agents
		// so having local.x is kind of pointless as it'll be
		// the same as slave.mesos
		/*
			// Create a way to lookup the local slave entry
			// local.slave.mesos.service.consul
			err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
				ID:      serviceprefix + ":local-" + slave.ID,
				Name:    "local.slave.mesos",
				Port:    port,
				Address: slave.PID.Host,
			})

			if err != nil {
				log.Println(err)
			}
		*/
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
	serviceprefix := "mesos-dns"
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
			err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
				ID:      serviceprefix + ":" + slave.ID,
				Name:    "leader.mesos",
				Port:    port,
				Address: slave.PID.Host,
			})

		} else {
			// Add slave to the pool of masters
			// master.mesos.service.consul
			err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
				ID:      serviceprefix + ":" + slave.ID,
				Name:    "master.mesos",
				Port:    port,
				Address: slave.PID.Host,
			})

			if err != nil {
				log.Println(err)
			}

			// Need to rethink this
			// Maybe <hostname>.slave.mesos? idk
			// but record will be distributed among consul agents
			// so having local.x is kind of pointless as it'll be
			// the same as slave.mesos
			/*
				// Create a way to lookup the local slave entry
				// local.master.mesos.service.consul
				err = c.Agents[slave.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
					ID:      serviceprefix + ":local-" + slave.ID,
					Name:    "local.master.mesos",
					Port:    port,
					Address: slave.PID.Host,
				})

				if err != nil {
					log.Println(err)
				}
			*/
		}
	}

}

func (c *ConsulBackend) insertFrameworkRecords(frameworks []state.Framework) {
	serviceprefix := "mesos-dns"
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
		err = c.Agents[framework.PID.Host].ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + ":" + framework.Name,
			Name:    framework.Name,
			Port:    port,
			Address: framework.PID.Host,
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
