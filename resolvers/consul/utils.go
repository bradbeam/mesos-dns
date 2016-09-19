package consul

import (
	"regexp"
	"strconv"
	"strings"

	capi "github.com/hashicorp/consul/api"
	"github.com/mesosphere/mesos-dns/logging"
)

// createService will create an appropriately formatted AgentServiceRegistration record.
// This includes the removal of underscores ("_") and translation of slashes ("/") to dashes ("-")
func createService(id string, name string, address string, port int, tags []string) *capi.AgentServiceRegistration {
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
	if port > 0 {
		asr.Port = port
	}

	return asr
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

// getDeltaChecks compares two slices (A and B) of AgentServiceRegistration and returns a slice of the differences found in B
func getDeltaServices(oldservices []*capi.AgentServiceRegistration, newservices []*capi.AgentServiceRegistration) []*capi.AgentServiceRegistration {
	delta := []*capi.AgentServiceRegistration{}
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

// getDeltaChecks compares two slices (A and B) of AgentCheckRegistration and returns a slice of the differences found in B
func getDeltaChecks(oldchecks []*capi.AgentCheckRegistration, newchecks []*capi.AgentCheckRegistration, context string) []*capi.AgentCheckRegistration {
	delta := []*capi.AgentCheckRegistration{}
	for _, newhc := range newchecks {
		found := false
		for _, oldhc := range oldchecks {
			if compareCheck(newhc, oldhc) {
				found = true
				break
			}
			// We add this context key in here to know how to react to IDs that are the same
			// In a registration/add case, we want to update the healthcheck if it is different
			// even if it uses the same ID
			// In a deregistration/purge case, we want to leave the healthcheck alone if the IDs are
			// the same but the content differs. This is so we don't remove a newly updated healthcheck
			// that uses the same ID as the old one
			if context == "purge" {
				if newhc.ID == oldhc.ID {
					found = true
					break
				}
			}
		}
		if !found {
			delta = append(delta, newhc)
		}
	}
	return delta
}

// evalVars does the translation of {PORT} and {IP} variables in a defined consul healthcheck
// with their appropriate values
func evalVars(check *string, address string, port int) bool {
	if strings.Contains(*check, "{PORT}") && port == 0 {
		logging.Error.Println("Invalid port for substitution in healthcheck", *check, port)
		return false
	}
	*check = strings.Replace(*check, "{IP}", address, -1)
	*check = strings.Replace(*check, "{PORT}", strconv.Itoa(port), -1)
	return true
}
