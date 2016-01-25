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
	"github.com/miekg/dns"
)

// Config holds mesos dns configuration
type Config struct {
	// Enable serving DNS requests
	DNSOn bool `json:"DnsOn"`
	//  Domain: name of the domain used (default "mesos", ie .mesos domain)
	Domain string
	// Enable replies for external requests
	ExternalOn bool
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
	// Resolver port: port used to listen for slave requests (default 53)
	Port int
	// Value of RecursionAvailable for responses in Mesos domain
	RecurseOn bool
	// Refresh frequency: the frequency in seconds of regenerating records (default 60)
	RefreshSeconds int
	// Remote DNS servers: IP address of the DNS server for forwarded accesses
	RemoteServers []string
	// Which backends to load (builtin by default)
	Resolvers []string
	// Timeout in seconds waiting for the master to return data from StateJson
	StateTimeoutSeconds int
	// SOA record fields (see http://tools.ietf.org/html/rfc1035#page-18)
	SOAExpire  uint32 // expiration time
	SOAMinttl  uint32 // minimum TTL
	SOAMname   string // primary name server
	SOARefresh uint32 // refresh interval
	SOARetry   uint32 // retry interval
	SOARname   string // email of admin esponsible
	SOASerial  uint32 // initial version number (incremented on refresh)
	// Timeout is the default connect/read/write timeout for outbound
	// queries
	Timeout int
	// TTL: the TTL value used for SRV and A records (default 60)
	TTL int32
	// Zookeeper: a single Zk url
	Zk string
	// Zookeeper Detection Timeout: how long in seconds to wait for Zookeeper to
	// be initially responsive. Default is 30 and 0 means no timeout.
	ZkDetectionTimeout int
}

// NewConfig return the default config of the resolver
func NewConfig() Config {
	return Config{
		DNSOn:               true,
		Domain:              "mesos",
		ExternalOn:          true,
		HTTPOn:              true,
		HTTPPort:            8123,
		IPSources:           []string{"netinfo", "mesos", "host"},
		Listener:            "0.0.0.0",
		Port:                53,
		RecurseOn:           true,
		RefreshSeconds:      60,
		RemoteServers:       []string{"8.8.8.8"},
		Resolvers:           []string{"builtin"},
		SOAExpire:           86400,
		SOAMinttl:           60,
		SOAMname:            "ns1.mesos",
		SOARefresh:          60,
		SOARetry:            600,
		SOARname:            "root.ns1.mesos",
		StateTimeoutSeconds: 300,
		Timeout:             5,
		TTL:                 60,
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

	if c.ExternalOn {
		if len(c.RemoteServers) == 0 {
			c.RemoteServers = GetLocalDNS()
		}
		if err = validateRemoteServers(c.RemoteServers); err != nil {
			logging.Error.Fatalf("Remote servers validation failed: %v", err)
		}
	}

	if err = validateIPSources(c.IPSources); err != nil {
		logging.Error.Fatalf("IPSources validation failed: %v", err)
	}

	c.Domain = strings.ToLower(c.Domain)

	// SOA record fields
	c.SOARname = strings.TrimRight(strings.Replace(c.SOARname, "@", ".", -1), ".") + "."
	c.SOAMname = strings.TrimRight(c.SOAMname, ".") + "."
	c.SOASerial = uint32(time.Now().Unix())

	// print configuration file
	logging.Verbose.Println("Mesos-DNS configuration:")
	logging.Verbose.Println("   - ConfigFile: ", c.File)
	logging.Verbose.Println("   - DnsOn: ", c.DNSOn)
	logging.Verbose.Println("   - Domain: " + c.Domain)
	logging.Verbose.Println("   - EnforceRFC952: ", c.EnforceRFC952)
	logging.Verbose.Println("   - ExternalOn: ", c.ExternalOn)
	logging.Verbose.Println("   - HttpPort: ", c.HTTPPort)
	logging.Verbose.Println("   - HttpOn: ", c.HTTPOn)
	logging.Verbose.Println("   - IPSources: ", c.IPSources)
	logging.Verbose.Println("   - Listener: " + c.Listener)
	logging.Verbose.Println("   - Masters: " + strings.Join(c.Masters, ", "))
	logging.Verbose.Println("   - Port: ", c.Port)
	logging.Verbose.Println("   - RecurseOn: ", c.RecurseOn)
	logging.Verbose.Println("   - RefreshSeconds: ", c.RefreshSeconds)
	logging.Verbose.Println("   - RemoteServers: " + strings.Join(c.RemoteServers, ", "))
	logging.Verbose.Println("   - Resolvers: " + strings.Join(c.Resolvers, ", "))
	logging.Verbose.Println("   - SOAExpire: ", c.SOAExpire)
	logging.Verbose.Println("   - SOAMinttl: ", c.SOAMinttl)
	logging.Verbose.Println("   - SOAMname: " + c.SOAMname)
	logging.Verbose.Println("   - SOARefresh: ", c.SOARefresh)
	logging.Verbose.Println("   - SOARetry: ", c.SOARetry)
	logging.Verbose.Println("   - SOARname: " + c.SOARname)
	logging.Verbose.Println("   - SOASerial: ", c.SOASerial)
	logging.Verbose.Println("   - StateTimeoutSeconds: ", c.StateTimeoutSeconds)
	logging.Verbose.Println("   - Timeout: ", c.Timeout)
	logging.Verbose.Println("   - TTL: ", c.TTL)
	logging.Verbose.Println("   - Zookeeper: ", c.Zk)
	logging.Verbose.Println("   - ZookeeperDetectionTimeout: ", c.ZkDetectionTimeout)

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
