package consul

import (
	"encoding/json"
	"io/ioutil"
	"testing"

	capi "github.com/hashicorp/consul/api"
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
	fwCh := make(chan map[string]string)
	taskCh := make(chan map[string]string)
	expected := Record{
		Address: "127.0.0.1",
		SlaveID: "slave1",
	}

	go generateMesosRecords(ch, rg, prefix, fwCh, taskCh)

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
	fwCh := make(chan map[string]string)

	go generateFrameworkRecords(ch, rg, prefix, fwCh)

	for r := range ch {
		if r.Service.Name != "marathon" {
			t.Logf("%+v", r.Service)
			t.Error("Failed to get marathon framework registration")
		}
	}

}

func TestGenerateTaskRecords(t *testing.T) {
	t.Log("Test generate task records")
	ch := make(chan Record)
	sj := loadState(t)
	rg := &records.RecordGenerator{
		State: sj,
	}
	rg.Config = records.NewConfig()
	rg.Config.IPSources = append(rg.Config.IPSources, "label:CalicoDocker.NetworkSettings.IPAddress")
	prefix := "mesos-dns"
	mesosInfo := make(map[string]string)
	mesosInfo["20160107-001256-134875658-5050-27524-S3"] = "slave01"
	taskCh := make(chan map[string]string)

	go func(info map[string]string, ch chan map[string]string) {
		ch <- info
		close(ch)
	}(mesosInfo, taskCh)

	kvCh := make(chan capi.KVPairs)
	go func(ch chan capi.KVPairs) {
		ch <- capi.KVPairs{}
		close(ch)
	}(kvCh)

	go generateTaskRecords(ch, rg, prefix, taskCh, kvCh)

	var recs []Record
	var svcs []Record
	var chks []Record
	for r := range ch {
		recs = append(recs, r)
		if r.Service != nil {
			svcs = append(svcs, r)
		}
		if r.Check != nil {
			chks = append(chks, r)
		}
	}

	if len(recs) != 18 {
		t.Error("Did not generate total expected number of records, 18, got", len(recs))
		t.Logf("%+v", recs)
	}
	if len(svcs) != 14 {
		t.Error("Did not generate expected number of service records, 14, got", len(svcs))
		t.Logf("%+v", svcs)
	}

	if len(chks) != 4 {
		t.Error("Did not generate expected number of check records, 4, got", len(chks))
		t.Logf("%+v", chks)
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
