package consul

import (
	"strings"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
)

func generateMesosRecords(ch chan Record, rg *records.RecordGenerator, prefix string, frameworkInfo chan map[string]string, taskInfo chan map[string]string) {
	// Create a bogus Slave struct for the leader
	// master@10.10.10.8:5050
	lead := state.Slave{
		PID: state.PID{
			&upid.UPID{
				Host: strings.Split(strings.Split(rg.State.Leader, "@")[1], ":")[0],
				Port: strings.Split(strings.Split(rg.State.Leader, "@")[1], ":")[1],
			},
		},
	}

	//frameworkSlaves := make(map[string]string)
	taskSlaves := make(map[string]string)

	for _, slave := range rg.State.Slaves {
		record := Record{
			Action:  "add",
			Address: "",
			SlaveID: slave.ID,
		}

		// Pull out only the hostname, not the FQDN
		slaveHostname := strings.Split(slave.Hostname, ".")[0]
		taskSlaves[slave.ID] = slaveHostname

		tags := []string{"slave", slaveHostname}

		if slave.Attrs.Master == "true" {
			tags = append(tags, "master")
		}

		if slave.PID.Host == lead.PID.Host {
			tags = append(tags, "leader")
		}

		record.Service = createService(strings.Join([]string{prefix, slave.ID}, ":"), "mesos", slave.PID.Host, slave.PID.Port, tags)

		ch <- record
	}
	close(ch)

	taskInfo <- taskSlaves
	close(taskInfo)
}

func generateFrameworkRecords(ch chan Record, rg *records.RecordGenerator, prefix string, mesosInfo chan map[string]string) {
	for _, framework := range rg.State.Frameworks {
		// Skip inactive frameworks
		if !framework.Active {
			continue
		}

		record := Record{
			Action:  "add",
			Address: "",
			SlaveID: "",
		}

		// Pull sanitized framework host + port values
		frameworkHost, frameworkPort := framework.HostPort()

		// :(  records.hostToIP4 would be super
		if frameworkHost == "" {
			continue
		}

		record.Service = createService(strings.Join([]string{prefix, framework.Name}, ":"), framework.Name, frameworkHost, frameworkPort, []string{})

		ch <- record
	}
	close(ch)
}

func generateTaskRecords(ch chan Record, rg *records.RecordGenerator, prefix string, mesosInfo chan map[string]string, consulKV chan capi.KVPairs) {
	ipsources := rg.Config.IPSources
	ipsources = append(ipsources, "fallback")
	slaveip := "127.0.0.1"

	var slaveInfo map[string]string
	for data := range mesosInfo {
		slaveInfo = data
	}

	kvPairs := <-consulKV

	for _, framework := range rg.State.Frameworks {
		if !framework.Active {
			continue
		}

		for _, task := range framework.Tasks {
			if task.State != "TASK_RUNNING" {
				continue
			}

			// Discover task IP
			address := getAddress(task, ipsources, slaveip)

			// Determine if we need to ignore the task because there is no appropriate IP
			if address == "" {
				continue
			}

			ports := task.Ports()
			if len(ports) == 0 {
				ports = append(ports, "0")
			}

			// Create a service registration for every port
			for _, port := range ports {
				record := Record{
					Action:  "add",
					Address: address,
					SlaveID: task.SlaveID,
				}
				var tags []string
				if slave, ok := slaveInfo[task.SlaveID]; ok {
					tags = append(tags, slave)
				}
				id := strings.Join([]string{prefix, task.SlaveID, task.ID, port}, ":")
				// Need to get slave hostname to add as tag
				record.Service = createService(id, task.Name, address, port, tags)
				ch <- record

				// Look up any defined HC's in consul based on task labels
				for _, label := range task.Labels {
					if label.Key != "ConsulHealthCheckKeys" {
						continue
					}

					record := Record{
						Action:  "add",
						Address: address,
						SlaveID: task.SlaveID,
					}

					for _, endpoint := range strings.Split(label.Value, ",") {
						hc, err := createHealthChecks(kvPairs, endpoint, id, address, port)
						if err != nil {
							// TODO something here
							continue

						}
						record.Check = hc
						ch <- record
					}
				}
			}
		}
	}

	close(ch)
}
