package records

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mesosphere/mesos-dns/logging"
	bind "github.com/mesosphere/mesos-dns/resolvers/bind/config"
	builtin "github.com/mesosphere/mesos-dns/resolvers/builtin/config"
	consul "github.com/mesosphere/mesos-dns/resolvers/consul/config"
	"github.com/miekg/dns"
)

// Config holds mesos dns configuration
type Config struct {
	//  Domain: name of the domain used (default "mesos", ie .mesos domain)
	Domain string
	// EnforceRFC952 will enforce an older, more strict set of rules for DNS labels
	EnforceRFC952 bool
	// File is the location of the config.json file
	File string
	// Enable serving HTTP requests
	HTTPOn bool `json:"HttpOn"`
	// NOTE(tsenart): HTTPPort, DNSOn and HTTPOn have defined JSON keys for
	// backwards compatibility with external API clients.
	HTTPPort int `json:"HttpPort"`
	// IPSources is the prioritized list of task IP sources
	IPSources []string // e.g. ["host", "docker", "mesos", "rkt"]
	// ListenAddr is the server listener address
	Listener string
	// Mesos master(s): a list of IP:port pairs for one or more Mesos masters
	Masters []string
	// Refresh frequency: the frequency in seconds of regenerating records (default 60)
	RefreshSeconds int
	// Which backends to load (builtin by default)
	Resolvers []string
	// Timeout in seconds waiting for the master to return data from StateJson
	StateTimeoutSeconds int
	// Timeout is the default connect/read/write timeout for outbound
	// queries
	Timeout int
	// Zookeeper: a single Zk url
	Zk string
	// Zookeeper Detection Timeout: how long in seconds to wait for Zookeeper to
	// be initially responsive. Default is 30 and 0 means no timeout.
	ZkDetectionTimeout int
	// Configuration specific to the builtin resolver
	Builtin *builtin.Config
	// Configuration specific to the consul resolver
	Consul *consul.Config
	// Configuration specific to the file resolver
	Bind *bind.Config
}

// NewConfig return the default config of the resolver
func NewConfig() Config {
	return Config{
		Domain:              "mesos",
		HTTPOn:              true,
		HTTPPort:            8123,
		IPSources:           []string{"netinfo", "mesos", "host"},
		Listener:            "0.0.0.0",
		RefreshSeconds:      60,
		Resolvers:           []string{"builtin"},
		StateTimeoutSeconds: 300,
		Timeout:             5,
		ZkDetectionTimeout:  30,
	}
}

// SetConfig instantiates a Config struct read in from config.json
func SetConfig(cjson string) Config {
	c, err := readConfig(cjson)
	if err != nil {
		logging.Error.Fatal(err)
	}
	logging.Verbose.Printf("config loaded from %q", c.File)
	// validate and complete configuration file
	err = validateEnabledServices(c)
	if err != nil {
		logging.Error.Fatalf("service validation failed: %v", err)
	}
	if err = validateMasters(c.Masters); err != nil {
		logging.Error.Fatalf("Masters validation failed: %v", err)
	}

	if err = validateIPSources(c.IPSources); err != nil {
		logging.Error.Fatalf("IPSources validation failed: %v", err)
	}

	c.Domain = strings.ToLower(c.Domain)

	// Builtin resolver validation
	if c.Builtin.ExternalOn {
		if len(c.Builtin.RemoteServers) == 0 {
			c.Builtin.RemoteServers = GetLocalDNS()
		}
		if err = validateRemoteServers(c.Builtin.RemoteServers); err != nil {
			logging.Error.Fatalf("Remote servers validation failed: %v", err)
		}
	}

	// SOA record fields
	c.Builtin.SOARname = strings.TrimRight(strings.Replace(c.Builtin.SOARname, "@", ".", -1), ".") + "."
	c.Builtin.SOAMname = strings.TrimRight(c.Builtin.SOAMname, ".") + "."
	c.Builtin.SOASerial = uint32(time.Now().Unix())

	// print configuration file
	logging.Verbose.Println("Mesos-DNS configuration:")
	logging.Verbose.Println("   - ConfigFile: ", c.File)
	logging.Verbose.Println("   - Domain: " + c.Domain)
	logging.Verbose.Println("   - EnforceRFC952: ", c.EnforceRFC952)
	logging.Verbose.Println("   - HttpPort: ", c.HTTPPort)
	logging.Verbose.Println("   - HttpOn: ", c.HTTPOn)
	logging.Verbose.Println("   - IPSources: ", c.IPSources)
	logging.Verbose.Println("   - Listener: " + c.Listener)
	logging.Verbose.Println("   - Masters: " + strings.Join(c.Masters, ", "))
	logging.Verbose.Println("   - RefreshSeconds: ", c.RefreshSeconds)
	logging.Verbose.Println("   - Resolvers: " + strings.Join(c.Resolvers, ", "))
	logging.Verbose.Println("   - StateTimeoutSeconds: ", c.StateTimeoutSeconds)
	logging.Verbose.Println("   - Timeout: ", c.Timeout)
	logging.Verbose.Println("   - Zookeeper: ", c.Zk)
	logging.Verbose.Println("   - ZookeeperDetectionTimeout: ", c.ZkDetectionTimeout)
	// print out specific backend configurations
	for _, backend := range c.Resolvers {
		logging.Verbose.Println("  ", backend, "Configuration:")
	}

	return *c
}

func readConfig(file string) (*Config, error) {
	c := NewConfig()

	workingDir := "."
	for _, name := range []string{"HOME", "USERPROFILE"} { // *nix, windows
		if dir := os.Getenv(name); dir != "" {
			workingDir = dir
		}
	}

	var err error
	c.File, err = filepath.Abs(strings.Replace(file, "~/", workingDir+"/", 1))
	if err != nil {
		return nil, fmt.Errorf("cannot find configuration file")
	} else if bs, err := ioutil.ReadFile(c.File); err != nil {
		return nil, fmt.Errorf("missing configuration file: %q", c.File)
	} else if err = json.Unmarshal(bs, &c); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file %q: %v", c.File, err)
	}

	return &c, nil
}

func unique(ss []string) []string {
	set := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := set[s]; !ok {
			set[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

// GetLocalDNS returns the first nameserver in /etc/resolv.conf
// Used for non-Mesos queries.
func GetLocalDNS() []string {
	conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		logging.Error.Fatalf("%v", err)
	}

	return nonLocalAddies(conf.Servers)
}

// Returns non-local nameserver entries
func nonLocalAddies(cservers []string) []string {
	bad := localAddies()

	good := []string{}

	for i := 0; i < len(cservers); i++ {
		local := false
		for x := 0; x < len(bad); x++ {
			if cservers[i] == bad[x] {
				local = true
			}
		}

		if !local {
			good = append(good, cservers[i])
		}
	}

	return good
}

// Returns an array of local ipv4 addresses
func localAddies() []string {
	addies, err := net.InterfaceAddrs()
	if err != nil {
		logging.Error.Println(err)
	}

	bad := []string{}

	for i := 0; i < len(addies); i++ {
		ip, _, err := net.ParseCIDR(addies[i].String())
		if err != nil {
			logging.Error.Println(err)
		}
		t4 := ip.To4()
		if t4 != nil {
			bad = append(bad, t4.String())
		}
	}

	return bad
}
