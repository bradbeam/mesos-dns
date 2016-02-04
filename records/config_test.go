package records

import (
	"testing"
)

func TestNewConfigValidates(t *testing.T) {
	c := NewConfig()
	err := validateIPSources(c.IPSources)
	if err != nil {
		t.Error(err)
	}
	err = validateMasters(c.Masters)
	if err != nil {
		t.Error(err)
	}
	err = validateEnabledServices(&c)
	if err == nil {
		t.Error("expected error because no masters and no zk servers are configured by default")
	}
	c.Zk = "foo"
	err = validateEnabledServices(&c)
	if err != nil {
		t.Error(err)
	}
}
