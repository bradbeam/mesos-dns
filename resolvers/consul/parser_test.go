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
	taskCh := make(chan map[string]SlaveInfo)
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

	tests := []struct {
		SkipInactive    bool
		FrameworkActive bool
	}{
		{
			SkipInactive:    true,
			FrameworkActive: true,
		},
		{
			SkipInactive:    false,
			FrameworkActive: true,
		},
		{
			SkipInactive:    true,
			FrameworkActive: false,
		},
		{
			SkipInactive:    false,
			FrameworkActive: false,
		},
	}

	prefix := "mesos-dns"
	for _, test := range tests {
		fwCh := make(chan map[string]string)
		ch := make(chan Record)
		rg.State.Frameworks[0].Active = test.FrameworkActive

		t.Log("Test framework active", test.FrameworkActive, "skip inactive frameworks", test.SkipInactive)
		go generateFrameworkRecords(ch, rg, prefix, fwCh, test.SkipInactive)

		// We don't need to worry about the skip true/active false case here because
		// we'll close an empty chan
		for r := range ch {
			if r.Service.Name != "marathon" {
				t.Logf("%+v", r.Service)
				t.Error("Failed to get marathon framework registration")
			}
		}

	}
}

func TestGenerateTaskRecords(t *testing.T) {
	t.Log("Test generate task records")
	sj := loadState(t)
	rg := &records.RecordGenerator{
		State: sj,
	}
	rg.Config = records.NewConfig()
	rg.Config.IPSources = append(rg.Config.IPSources, "label:CalicoDocker.NetworkSettings.IPAddress")
	prefix := "mesos-dns"
	mesosInfo := make(map[string]SlaveInfo)
	mesosInfo["20160107-001256-134875658-5050-27524-S3"] = SlaveInfo{
		Address:  "127.0.0.1",
		Hostname: "slave01",
		ID:       "20160107-001256-134875658-5050-27524-S3",
	}

	tests := []struct {
		ExpectedChks    int
		ExpectedRecs    int
		ExpectedSvcs    int
		FrameworkActive bool
		SkipInactive    bool
	}{
		{
			ExpectedChks:    4,
			ExpectedRecs:    9,
			ExpectedSvcs:    5,
			FrameworkActive: true,
			SkipInactive:    true,
		},
		{
			ExpectedChks:    4,
			ExpectedRecs:    9,
			ExpectedSvcs:    5,
			FrameworkActive: true,
			SkipInactive:    false,
		},
		{
			ExpectedChks:    0,
			ExpectedRecs:    0,
			ExpectedSvcs:    0,
			FrameworkActive: false,
			SkipInactive:    true,
		},
		{
			ExpectedChks:    4,
			ExpectedRecs:    9,
			ExpectedSvcs:    5,
			FrameworkActive: false,
			SkipInactive:    false,
		},
	}

	for _, test := range tests {
		t.Log("Test framework active", test.FrameworkActive, "skip inactive frameworks", test.SkipInactive)

		taskCh := make(chan map[string]SlaveInfo)
		go func(info map[string]SlaveInfo, ch chan map[string]SlaveInfo) {
			ch <- info
			close(ch)
		}(mesosInfo, taskCh)

		kvCh := make(chan capi.KVPairs)
		go func(ch chan capi.KVPairs) {
			ch <- capi.KVPairs{}
			close(ch)
		}(kvCh)

		for idx, _ := range rg.State.Frameworks {
			rg.State.Frameworks[idx].Active = test.FrameworkActive
		}
		ch := make(chan Record)
		go generateTaskRecords(ch, rg, prefix, taskCh, kvCh, test.SkipInactive)

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

		// 4x -S3 slave, 4x checks, 1x -S66 slave (netinfo ip)
		// the rest fail to assign an IP because we don't have slaveinfo populated
		// for the other slaves
		if len(recs) != test.ExpectedRecs {
			t.Errorf("Did not generate total expected number of records, %d, got %d", test.ExpectedRecs, len(recs))
			for _, rec := range recs {
				t.Logf("%+v", rec)
			}
		}

		// 4x -S3 slave, 1x -S66 slave (netinfo ip)
		if len(svcs) != test.ExpectedSvcs {
			t.Errorf("Did not generate expected number of service records, %d, got %d", test.ExpectedSvcs, len(svcs))
			for _, rec := range svcs {
				t.Logf("%s %+v", rec.Service.ID, rec)
			}
		}

		// 4x checks
		if len(chks) != test.ExpectedChks {
			t.Errorf("Did not generate expected number of check records, %d, got %d", test.ExpectedChks, len(chks))
			for _, rec := range chks {
				t.Logf("%s %+v", rec.Check.ID, rec)
			}
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
