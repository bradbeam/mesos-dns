package consul

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"

	consul "github.com/hashicorp/consul/api"
	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
	consulconfig "github.com/mesosphere/mesos-dns/resolvers/consul/config"
)

type ConsulBackend struct {
	Agents            map[string]*consul.Agent
	AgentPort         string
	Client            *consul.Client
	Config            *consul.Config
	LookupOrder       []string
	Refresh           int
	ServicePrefix     string
	SlaveIDIP         map[string]string
	SlaveIPID         map[string]string
	SlaveIDHostname   map[string]string
	State             state.State
	FrameworkRecords  map[string]*ConsulRecords
	MesosRecords      map[string]*ConsulRecords
	TaskRecords       map[string]*ConsulRecords
	StateHealthChecks map[string][]*AgentCheckRegistrationSlave
	/*
		PrevStateFrameworkRecords map[string][]*consul.AgentServiceRegistration
		PrevStateMesosRecords     map[string][]*consul.AgentServiceRegistration
		PrevStateTaskRecords      map[string][]*consul.AgentServiceRegistration
	*/
	PrevStateHealthChecks map[string][]*AgentCheckRegistrationSlave
}

type AgentCheckRegistrationSlave struct {
	TaskID string
	Regs   []*consul.AgentCheckRegistration
}

type ConsulRecords struct {
	Current  []*consul.AgentServiceRegistration
	Previous []*consul.AgentServiceRegistration
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
		Agents:                make(map[string]*consul.Agent),
		AgentPort:             port,
		Client:                client,
		Config:                cfg,
		LookupOrder:           []string{"docker", "netinfo", "host"},
		Refresh:               5,
		ServicePrefix:         "mesos-dns",
		SlaveIDIP:             make(map[string]string),
		SlaveIPID:             make(map[string]string),
		SlaveIDHostname:       make(map[string]string),
		State:                 state.State{},
		FrameworkRecords:      make(map[string]*ConsulRecords),
		MesosRecords:          make(map[string]*ConsulRecords),
		TaskRecords:           make(map[string]*ConsulRecords),
		StateHealthChecks:     make(map[string][]*AgentCheckRegistrationSlave),
		PrevStateHealthChecks: make(map[string][]*AgentCheckRegistrationSlave),
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

		// Make sure we've got everything initialized
		if _, ok := c.MesosRecords[slave.ID]; !ok {
			c.MesosRecords[slave.ID] = &ConsulRecords{}
		}

		c.MesosRecords[slave.ID].Current = append(c.MesosRecords[slave.ID].Current, createService(strings.Join([]string{c.ServicePrefix, slave.ID}, ":"), "mesos", slave.PID.Host, port, tags))

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

		// Make sure we've got everything initialized
		if _, ok := c.FrameworkRecords[slaveid]; !ok {
			c.FrameworkRecords[slaveid] = &ConsulRecords{}
		}

		c.FrameworkRecords[slaveid].Current = append(c.FrameworkRecords[slaveid].Current, createService(strings.Join([]string{c.ServicePrefix, framework.Name}, ":"), framework.Name, framework.PID.Host, port, []string{}))
	}
}

func (c *ConsulBackend) generateTaskRecords(tasks []state.Task) {
	for _, task := range tasks {
		if task.State != "TASK_RUNNING" {
			continue
		}
		// Might be able to use task.SlaveIP, but in testing this reutrns ""

		address := c.getAddress(task)

		// Make sure we've got everything initialized
		if _, ok := c.TaskRecords[task.SlaveID]; !ok {
			c.TaskRecords[task.SlaveID] = &ConsulRecords{}
		}
		// Create a service registration for every port
		if len(task.Ports()) == 0 {

			// Create a service registration for the base service
			id := strings.Join([]string{c.ServicePrefix, c.SlaveIDHostname[task.SlaveID], task.ID}, ":")
			c.TaskRecords[task.SlaveID].Current = append(c.TaskRecords[task.SlaveID].Current, createService(id, task.Name, address, 0, []string{c.SlaveIDHostname[task.SlaveID]}))
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
				c.TaskRecords[task.SlaveID].Current = append(c.TaskRecords[task.SlaveID].Current, createService(id, task.Name, address, p, []string{c.SlaveIDHostname[task.SlaveID]}))
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
		slaveid := c.SlaveIPID[agentid]
		wg.Add(1)
		go func() {
			services := []*consul.AgentServiceRegistration{}

			// May not have any records for specific slave
			if _, ok := c.MesosRecords[slaveid]; ok {
				services = append(services, getDeltaServices(c.MesosRecords[slaveid].Previous, c.MesosRecords[slaveid].Current)...)
			}
			if _, ok := c.FrameworkRecords[slaveid]; ok {
				services = append(services, getDeltaServices(c.FrameworkRecords[slaveid].Previous, c.FrameworkRecords[slaveid].Current)...)
			}
			if _, ok := c.TaskRecords[slaveid]; ok {
				services = append(services, getDeltaServices(c.TaskRecords[slaveid].Previous, c.TaskRecords[slaveid].Current)...)
			}

			log.Println("Changes:", len(services))
			for _, service := range services {
				err := c.Agents[agentid].ServiceRegister(service)
				if err != nil {
					log.Println("Failed to register service", service.ID, "on agent", agentid)
					log.Println("Error:", err)
					continue
				}
			}
			wg.Done()
		}()
		wg.Wait()
	}
}
func (c *ConsulBackend) Cleanup() {

	// TODO: Review this to make sure it makes sense
	// Seems pretty friggin complicated and UGLY
	var wg sync.WaitGroup

	for agentid, _ := range c.Agents {
		// Launch a goroutine per agent
		slaveid := c.SlaveIPID[agentid]
		wg.Add(1)
		go func() {
			services := []*consul.AgentServiceRegistration{}

			// May not have any records for specific slave
			if _, ok := c.MesosRecords[slaveid]; ok {
				services = append(services, getDeltaServices(c.MesosRecords[slaveid].Current, c.MesosRecords[slaveid].Previous)...)
			}
			if _, ok := c.FrameworkRecords[slaveid]; ok {
				services = append(services, getDeltaServices(c.FrameworkRecords[slaveid].Current, c.FrameworkRecords[slaveid].Previous)...)
			}
			if _, ok := c.TaskRecords[slaveid]; ok {
				services = append(services, getDeltaServices(c.TaskRecords[slaveid].Current, c.TaskRecords[slaveid].Previous)...)
			}

			log.Println("Changes:", len(services))
			for _, service := range services {
				err := c.Agents[agentid].ServiceDeregister(service.ID)
				if err != nil {
					log.Println("Failed to deregister service", service.ID, "on agent", agentid)
					log.Println("Error:", err)
					continue
				}
			}
			wg.Done()
		}()
		wg.Wait()
	}
}

//for _, service := range []*ConsulRecords{c.MesosRecords[c.SlaveIPID[agentid]], c.FrameworkRecords[c.SlaveIPID[agentid]], c.TaskRecords[c.SlaveIPID[agentid]]} {
//for _, service := range services {
/*
	service := c.MesosRecords[c.SlaveIPID[agentid]]
	log.Println("Current", service.Current)
	log.Println("Previous", service.Previous)
	c.MesosRecords[c.SlaveIPID[agentid]].Previous = c.MesosRecords[c.SlaveIPID[agentid]].Current
	service.Current = []*consul.AgentServiceRegistration{}
	log.Println("Current", service.Current)

//for _, service := range []*ConsulRecords{c.MesosRecords[c.SlaveIPID[agentid]], c.FrameworkRecords[c.SlaveIPID[agentid]], c.TaskRecords[c.SlaveIPID[agentid]]} {
//for _, service := range services {
/*
	service := c.MesosRecords[c.SlaveIPID[agentid]]
	log.Println("Current", service.Current)
	log.Println("Previous", service.Previous)
	c.MesosRecords[c.SlaveIPID[agentid]].Previous = c.MesosRecords[c.SlaveIPID[agentid]].Current
	service.Current = []*consul.AgentServiceRegistration{}
	log.Println("Current", service.Current)
	log.Println("Previous", service.Previous)
*/

//	c.FrameworkRecords[c.SlaveIPID[agentid]].Previous = c.FrameworkRecords[c.SlaveIPID[agentid]].Current
//	c.TaskRecords[c.SlaveIPID[agentid]].Previous = c.TaskRecords[c.SlaveIPID[agentid]].Current
/*
	prevcheck := false
	// Check to see if we registered the same service previously
	prevservices := [][]*consul.AgentServiceRegistration{c.PrevStateMesosRecords[c.SlaveIPID[agentid]], c.PrevStateFrameworkRecords[c.SlaveIPID[agentid]], c.PrevStateTaskRecords[c.SlaveIPID[agentid]]}
	for _, previousservices := range prevservices {
		for _, pservice := range previousservices {
			if compareService(service, pservice) {
				prevcheck = true
				break
			}
		}
		if prevcheck {
			break
		}
	}
	// Register the service

	if !prevcheck {
		if len(prevservices) > 0 {
			log.Println("New service found", service.ID)
		}
		err := c.Agents[agentid].ServiceRegister(service)
		if err != nil {
			log.Println("Failed to register service", err)
			continue
		}
	}

	// Register any healthchecks for the service
	for _, hc := range c.StateHealthChecks[c.SlaveIPID[agentid]] {
		if hc.TaskID != service.ID {
			continue
		}

		for _, hcreg := range hc.Regs {
			for _, phc := range c.PrevStateHealthChecks[c.SlaveIPID[agentid]] {
				prevcheck = false
				for _, phcreg := range phc.Regs {
					if compareCheck(hcreg, phcreg) {
						prevcheck = true
						break
					}
				}

				if prevcheck {
					break
				}
			}

			// Skip consul update since our checks havent changed
			if prevcheck {
				continue
			}

			log.Println("New healthcheck found", hcreg.Name, "for service", service.ID)
			err := c.Agents[agentid].CheckRegister(hcreg)
			if err != nil {
				log.Println("Failed to register healthcheck for service", service.ID, err)
				continue
			}
		}

	}
*/
//}
//}

/*
			wg.Done()
		}()
		wg.Wait()
		// Save off current state records
		//	c.MesosRecords[c.SlaveIPID[agentid]].Previous = c.MesosRecords[c.SlaveIPID[agentid]].Current
		//	c.FrameworkRecords[c.SlaveIPID[agentid]].Previous = c.FrameworkRecords[c.SlaveIPID[agentid]].Current
		//	c.TaskRecords[c.SlaveIPID[agentid]].Previous = c.TaskRecords[c.SlaveIPID[agentid]].Current
	}
}
*/

/*
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
*/

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
	asr := &consul.AgentServiceRegistration{
		ID:      id,
		Name:    name,
		Address: address,
		Tags:    tags,
	}
	if port > 0 {
		asr.Port = port
	}

	return asr
}

func compareService(newservice *consul.AgentServiceRegistration, oldservice *consul.AgentServiceRegistration) bool {
	// Do this explicitly cause #grumpyface
	if newservice.ID != oldservice.ID {
		return false
	}
	if newservice.Name != oldservice.Name {
		return false
	}
	if newservice.Address != oldservice.Address {
		return false
	}
	if newservice.Port != oldservice.Port {
		return false
	}
	if len(newservice.Tags) != len(oldservice.Tags) {
		return false
	}

	for _, otag := range oldservice.Tags {
		found := false
		for _, ntag := range newservice.Tags {
			if otag == ntag {
				found = true
			}
		}

		if !found {
			return false
		}
	}
	return true
}

func compareCheck(newcheck *consul.AgentCheckRegistration, oldcheck *consul.AgentCheckRegistration) bool {
	if newcheck.ID != oldcheck.ID {
		return false
	}
	if newcheck.Name != oldcheck.Name {
		return false
	}
	if newcheck.ServiceID != oldcheck.ServiceID {
		return false
	}
	if newcheck.AgentServiceCheck != oldcheck.AgentServiceCheck {
		return false
	}
	return true
}

func getDeltaServices(oldservices []*consul.AgentServiceRegistration, newservices []*consul.AgentServiceRegistration) []*consul.AgentServiceRegistration {
	delta := []*consul.AgentServiceRegistration{}
	// Need to compare current vs old
	for _, service := range newservices {
		found := false
		for _, existing := range oldservices {
			if compareService(service, existing) {
				found = true
				break
			}

		}
		if !found {
			delta = append(delta, service)
		}
	}
	return delta
}
