package consul

import (
	"strconv"
	"strings"

	"github.com/CiscoCloud/mesos-consul/state"
	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
)

func generateMesosRecords(ch chan Record, rg *records.RecordGenerator, prefix string) {
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

	for _, slave := range rg.State.Slaves {
		record := Record{
			Address: "",
			SlaveID: slave.ID,
		}

		// Pull out only the hostname, not the FQDN
		slaveHostname := strings.Split(slave.Hostname, ".")[0]

		tags := []string{"slave", slaveHostname}

		if slave.Attrs.Master == "true" {
			tags = append(tags, "master")
		}

		if slave.PID.Host == lead.PID.Host {
			tags = append(tags, "leader")
		}

		port, err := strconv.Atoi(slave.PID.Port)
		if err != nil {
			logging.Error.Println("Failed to get port for slave", slave.ID, ". Error:", err)
			continue
		}

		record.Service = createService(strings.Join([]string{prefix, slave.ID}, ":"), "mesos", slave.PID.Host, port, tags)

		ch <- record
	}
	close(ch)
}
