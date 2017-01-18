package consul

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records/state"
)

// CreateService will create an appropriately formatted AgentServiceRegistration record.
// This includes the removal of underscores ("_") and translation of slashes ("/") to dashes ("-")
func CreateService(id string, name string, address string, stateport string, tags []string) *capi.AgentServiceRegistration {
	// Format the name appropriately
	reg, err := regexp.Compile("[^\\w-]")
	if err != nil {
		logging.Error.Println(err)
		return &capi.AgentServiceRegistration{}
	}

	s := reg.ReplaceAllString(name, "-")

	asr := &capi.AgentServiceRegistration{
		ID:      id,
		Name:    strings.ToLower(strings.Replace(s, "_", "", -1)),
		Address: address,
		Tags:    tags,
	}

	// Discover port
	// If we have an invalid or empty string from stateport,
	// we'll skip assigning a port to the service registration
	port, err := strconv.Atoi(stateport)
	if err == nil {
		asr.Port = port
	}

	return asr
}

func createHealthChecks(kvs capi.KVPairs, endpoint string, id string, address string, port string) (*capi.AgentCheckRegistration, error) {
	check := &capi.AgentCheckRegistration{}
	var err error

	// Find the KV pair we need
	for _, kv := range kvs {
		if kv.Key != "healthchecks/"+endpoint {
			continue
		}

		err = json.Unmarshal(kv.Value, check)
		if err != nil {
			break
		}

		check.ID = strings.Join([]string{check.ID, id}, ":")
		check.ServiceID = id

		// Now to do some variable expansion
		// We're going to reserve `{IP}` and `{PORT}`
		// We'll apply this to http, script, tcp
		if check.HTTP != "" {
			check.HTTP, err = evalVars(check.HTTP, address, port)
		}
		if check.TCP != "" {
			check.TCP, err = evalVars(check.TCP, address, port)
		}
		if check.Script != "" {
			check.Script, err = evalVars(check.Script, address, port)
		}

		// We've found the appropriate KV pairs and have
		// generated our agent check registration
		break
	}

	return check, err
}

// compareService is a deep comparison of two AgentServiceRegistrations to determine if they are the same
func compareService(newservice *capi.AgentServiceRegistration, oldservice *capi.AgentServiceRegistration) bool {
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

// compareCheck is a deep comparison of two AgentCheckRegistrations to determine if they are the same
func compareCheck(newcheck *capi.AgentCheckRegistration, oldcheck *capi.AgentCheckRegistration) bool {
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

func getDeltaRecords(oldRecords []Record, newRecords []Record, action string) []Record {
	oldchecks := []Record{}
	oldservices := []Record{}
	newchecks := []Record{}
	newservices := []Record{}

	for _, rec := range oldRecords {
		if rec.Service != nil {
			oldservices = append(oldservices, rec)
		}
		if rec.Check != nil {
			oldchecks = append(oldchecks, rec)
		}
	}

	for _, rec := range newRecords {
		if rec.Service != nil {
			newservices = append(newservices, rec)
		}
		if rec.Check != nil {
			newchecks = append(newchecks, rec)
		}
	}

	delta := []Record{}
	delta = append(delta, getDeltaServices(oldservices, newservices, action)...)
	delta = append(delta, getDeltaChecks(oldchecks, newchecks, action)...)
	return delta

}

// getDeltaServices compares two slices (A and B) of AgentServiceRegistration and returns a slice of the differences found in B
func getDeltaServices(oldservices []Record, newservices []Record, action string) []Record {
	delta := []Record{}
	// Need to compare current vs old
	for _, service := range newservices {
		found := false
		for _, existing := range oldservices {
			if compareService(service.Service, existing.Service) {
				found = true
				break
			}
		}
		if !found {
			service.Action = action
			delta = append(delta, service)
		}
	}
	return delta
}

// getDeltaChecks compares two slices (A and B) of AgentCheckRegistration and returns a slice of the differences found in B
func getDeltaChecks(oldchecks []Record, newchecks []Record, action string) []Record {
	delta := []Record{}
	for _, newhc := range newchecks {
		found := false
		for _, oldhc := range oldchecks {
			if compareCheck(newhc.Check, oldhc.Check) {
				found = true
				break
			}
		}
		if !found {
			newhc.Action = action
			delta = append(delta, newhc)
		}
	}
	return delta
}

// evalVars does the translation of {PORT} and {IP} variables in a defined consul healthcheck
// with their appropriate values
func evalVars(check string, address string, port string) (string, error) {
	if strings.Contains(check, "{PORT}") && port == "0" {
		return check, errors.New("Invalid port for substitution in healthcheck " + check + port)
	}

	// Replace both {PORT} and {IP}
	return strings.Replace(strings.Replace(check, "{PORT}", port, -1), "{IP}", address, -1), nil
}

// getAddress attempts to discover a tasks address based on a list of ip sources
// you can think of it as similar functionality to nsswitch
func getAddress(task state.Task, ipsources []string, slaveip string) string {

	var address string
	for _, lookup := range ipsources {
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
				logging.Error.Fatal("Lookup order label is not in proper format `label:labelname`")
				continue
			}

			addresses := state.StatusIPs(task.Statuses, state.Labels(lookupkey[1]))
			if len(addresses) > 0 {
				address = addresses[0]
			}

			// CUSTOM
			// Since we add the calicodocker label after the container has started up, we'll need to do
			// an additional check to see if we need to wait for the calico label
			if address == "" && lookupkey[1] == "CalicoDocker.NetworkSettings.IPAddress" {
				for _, taskLabel := range task.Labels {
					if taskLabel.Key == "CALICO_IP" {
						return ""
					}
				}
			}
		case "fallback":
			address = slaveip
		}

		if address != "" {
			break
		}
	}

	return address
}
