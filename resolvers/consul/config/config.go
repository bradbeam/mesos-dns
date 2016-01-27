package config

import consul "github.com/hashicorp/consul/api"

type Config consul.Config

func NewConfig() *consul.Config {
	// Probably need to update this with the merging of defined config options
	return consul.DefaultConfig()
}

/*
type RecordGenerator struct {
	Masters    []masterInfo
	Slaves     []slaveInfo
	Frameworks []frameworkInfo
	Tasks      []taskInfo
	State      state.State
}

type masterInfo struct {
	Index   int
	Leader  bool // if it's the leader
	Name    string
	Address string
	Port    int
}

type slaveInfo struct {
	Index   int
	Name    string
	Address string
	Port    int
}

type frameworkInfo struct {
	Name    string
	Address string
	Port    Int
}

type taskInfo struct {
	ID        string // Probably some combination of Name + something; probably similar to existing taskid + record/generator/hashstring()
	Name      string
	Address   string
	Slave     int    // slaveInfo.Index  //// should allow us to map up with locally running consul agent
	Framework string // frameworkInfo.Name
	TcpPorts  []int
	UdpPorts  []int
}
*/
