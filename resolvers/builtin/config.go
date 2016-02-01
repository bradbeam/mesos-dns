package builtin

import (
	"net"

	"github.com/mesosphere/mesos-dns/logging"
	"github.com/miekg/dns"
)

type Config struct {
	// Enable serving DNS requests
	DNSOn bool `json:"DnsOn"`
	// Enable replies for external requests
	ExternalOn bool
	// Enable serving HTTP requests
	HTTPOn bool `json:"HttpOn"`
	// NOTE(tsenart): HTTPPort, DNSOn and HTTPOn have defined JSON keys for
	// backwards compatibility with external API clients.
	HTTPPort int `json:"HttpPort"`
	// ListenAddr is the server listener address
	Listener string
	// Resolver port: port used to listen for slave requests (default 53)
	Port int
	// Value of RecursionAvailable for responses in Mesos domain
	RecurseOn bool
	// Remote DNS servers: IP address of the DNS server for forwarded accesses
	RemoteDNS []string
	// Timeout is the default connect/read/write timeout for outbound
	// queries
	Timeout int
	// TTL: the TTL value used for SRV and A records (default 60)
	TTL int32
}

// NewConfig return the default config of the resolver
func NewConfig() *Config {
	return &Config{
		DNSOn:      true,
		ExternalOn: true,
		HTTPOn:     true,
		HTTPPort:   8123,
		Listener:   "0.0.0.0",
		Port:       53,
		RecurseOn:  true,
		RemoteDNS:  []string{"8.8.8.8"},
		Timeout:    5,
		TTL:        60,
	}
}

func (c *Config) SetConfig(conf map[string]interface{}) {
	var err error

	// Builtin resolver validation
	if c.ExternalOn {
		if len(c.RemoteDNS) == 0 {
			c.RemoteDNS = GetLocalDNS()
		}
		if err = validateRemoteDNS(c.RemoteDNS); err != nil {
			logging.Error.Fatalf("Remote servers validation failed: %v", err)
		}
	}

	err = validateEnabledServices(c)
	if err != nil {
		logging.Error.Fatalf("service validation failed: %v", err)
	}
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
