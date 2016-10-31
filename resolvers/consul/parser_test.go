package consul

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	"github.com/mesos/mesos-go/upid"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/records/state"
)

func TestGenerateMesosRecords(t *testing.T) {
	t.Log("Test generate mesos records")
	ch := make(chan Record)
	rg := &records.RecordGenerator{
		State: state.State{
			Leader: "master@127.0.0.1:5050",
			Slaves: []state.Slave{
				state.Slave{
					ID:       "slave1",
					Hostname: "slave.local",
					PID: state.PID{
						&upid.UPID{
							Host: "127.0.0.1",
							Port: "5050",
						},
					},
				},
			},
		},
	}
	prefix := "mesos-dns"
	expected := Record{
		Address: "127.0.0.1",
		SlaveID: "slave1",
	}

	go generateMesosRecords(ch, rg, prefix)

	for r := range ch {
		if r.SlaveID != expected.SlaveID {
			t.Error("Failed to get slaveID from generated record")
		}
	}
}

func TestGenerateFrameworkRecords(t *testing.T) {
	t.Log("Test generate framework records")
	ch := make(chan Record)
	rg := &records.RecordGenerator{
		State: state.State{
			Frameworks: []state.Framework{
				state.Framework{
					Active:   true,
					Hostname: "slave.local",
					Name:     "marathon",
					PID: state.PID{
						&upid.UPID{
							Host: "127.0.0.1",
							Port: "8080",
						},
					},
					Tasks: []state.Task{},
				},
			},
		},
	}
	prefix := "mesos-dns"

	go generateFrameworkRecords(ch, rg, prefix)

	for r := range ch {
		if r.Service.Name != "marathon" {
			t.Logf("%+v", r.Service)
			t.Error("Failed to get marathon framework registration")
		}
	}

}

func loadState(t *testing.T) state.State {
	var sj state.State
	b, err := ioutil.ReadFile("test/state.json")
	if err != nil {
		t.Fatal(err)
	} else if err = json.Unmarshal(b, &sj); err != nil {
		t.Fatal(err)
	}

	return sj
}
