package consul

import (
	"strconv"

	consul "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/records"
	consulconfig "github.com/mesosphere/mesos-dns/resolvers/consul/config"
)

type ConsulBackend struct {
	Client *consul.Client
	Agents map[string]*consul.Agent
}

func New(config records.Config, errch chan error, version string) *ConsulBackend {
	client, err := consul.NewClient(consulconfig.NewConfig())
	if err != nil {
		errch <- err
		return &ConsulBackend{}
	}

	return &ConsulBackend{
		Client: client,
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

func (c *ConsulBackend) connectAgents() {
	members, err := c.Client.Agent().Members(false)
	if err != nil {
		// Do something
	}
	// Only get LAN members
	for _, agent := range members {

		// Not sure what agent.Name translates to; may want to
		// switch up to agent.Address
		// Test connection to each agent and reconnect as needed
		if _, ok := c.Agents[agent.Name]; ok {
			_, err := c.Agents[agent.Name].Self()
			if err == nil {
				continue
			}
		}
		cfg := consulconfig.NewConfig()
		cfg.Address = agent.Addr + ":" + strconv.Itoa(int(agent.Port))
		client, err := consul.NewClient(cfg)
		if err != nil {
			// How do we want to handle consul agent not being responsive

		}

		// Do a sanity check that we are connected to agent
		_, err = client.Agent().Self()
		if err != nil {
			// Dump agent?
		} else {
			c.Agents[agent.Name] = client.Agent()
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

func (c *ConsulBackend) insertSlaveRecords(rg *records.RecordGenerator) {
	// Note:
	// We'll want to look at using the TTL portion of checks
	// to take care of obsoleting old records
	serviceprefix := "mesosdns"
	for _, slave := range rg.Slaves {
		err := agent.ServiceRegister(&consul.AgentServiceRegistration{
			ID:      serviceprefix + "-" + slave.Name,
			Name:    slave.Name,
			Port:    slave.Port,
			Address: slave.Address,
			Check:   &consul.AgentServiceCheck{},
		})
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

/*
	// I think this might get ugly
	// // Iterate through all our slave IPs to try to map them up with consul agents
	for id, addr := range rg.SlaveIPs {
		for _, agent := range Agents {
			if Agent.Addr == addr {
				AgentSlave[agent] = addr
				SlaveAgent[addr] = agent
			}
		}
	}

	for record, ip := range rg.As {
		for addr, agent := range SlaveAgent {
			if ip == addr {
				/*
					// insert canonical A records
					canonical := ctx.taskName + "-" + ctx.taskID + "-" + ctx.slaveID + "." + fname
					arec := ctx.taskName + "." + fname
					rg.insertRR(arec+tail, ctx.taskIP, "A")
					rg.insertRR(canonical+tail, ctx.taskIP, "A")
					rg.insertRR(arec+".slave"+tail, ctx.slaveIP, "A")
					rg.insertRR(canonical+".slave"+tail, ctx.slaveIP, "A")

*/

// Need to register with agent
/*
	err := agent.ServiceRegister(&consul.AgentServiceRegistration{
		ID:      service.ID,
		Name:    service.Name,
		Port:    service.Port,
		Address: service.Address,
		Check: &consulapi.AgentServiceCheck{
			TTL:      service.Check.TTL,
			Script:   service.Check.Script,
			HTTP:     service.Check.HTTP,
			Interval: service.Check.Interval,
		},
	})
*/
/*
				if err != nil {
					log.Println(err)
				}
			}
		}
	}
}
*/
