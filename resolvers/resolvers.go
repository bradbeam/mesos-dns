package resolvers

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/mesosphere/mesos-dns/logging"
	"github.com/mesosphere/mesos-dns/records"
	"github.com/mesosphere/mesos-dns/resolvers/bind"
	"github.com/mesosphere/mesos-dns/resolvers/builtin"
	"github.com/mesosphere/mesos-dns/resolvers/consul"
)

type Resolver interface {
	Reload(rg *records.RecordGenerator)
}

func New(config records.Config, errch chan error, version string) []Resolver {
	var resolvers []Resolver

	for _, rType := range config.Resolvers {
		// Each backend receives their config as type interface{} or nil.
		switch strings.ToLower(rType) {
		case "consul":
			conf := consul.NewConfig()
			Merge(config.ResolversConf["consul"].(map[string]interface{}), &conf)
			resolvers = append(resolvers, consul.New(*conf, errch, version))
		case "builtin":
			conf := builtin.NewConfig()
			Merge(config.ResolversConf["builtin"].(map[string]interface{}), &conf)
			resolvers = append(resolvers, builtin.New(*conf, errch, version))
		case "bind":
			conf := bind.NewConfig()
			Merge(config.ResolversConf["bind"].(map[string]interface{}), &conf)
			resolvers = append(resolvers, bind.New(*conf, errch, version))
		}
	}

	return resolvers
}

func Merge(data map[string]interface{}, config interface{}) {
	for k, v := range data {
		err := SetField(config, k, v)
		if err != nil {
			logging.Verbose.Println("Error merging config key %s: %v", k, err)
		}
	}
}

// Helper function for merging provided key/value with a struct
func SetField(obj interface{}, name string, value interface{}) error {
	structValue := reflect.ValueOf(obj)
	structFieldValue := reflect.Indirect(structValue).FieldByName(name)

	// Check if field exists
	if !structFieldValue.IsValid() {
		return fmt.Errorf("No such field: %s in obj", name)
	}

	// Check if field is settable
	if !structFieldValue.CanSet() {
		return fmt.Errorf("Cannot set %s field value", name)
	}

	structFieldType := structFieldValue.Type()
	val := reflect.ValueOf(value)
	if structFieldType != val.Type() {
		return errors.New("Provided value type did not match struct field type")
	}

	structFieldValue.Set(val)
	return nil
}
