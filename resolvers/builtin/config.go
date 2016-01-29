package builtin

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
	// Resolver port: port used to listen for slave requests (default 53)
	Port int
	// Value of RecursionAvailable for responses in Mesos domain
	RecurseOn bool
	// Remote DNS servers: IP address of the DNS server for forwarded accesses
	RemoteDNS []string
}

// NewConfig return the default config of the resolver
func NewConfig(c) Config {

    return Config{
		DNSOn:         true,
		ExternalOn:    true,
		HTTPOn:        true,
		HTTPPort:      8123,
		Port:          53,
		RecurseOn:     true,
		RemoteDNS:     []string{"8.8.8.8"},
	}
}

func SetConfig(cjson string) Config {
	// Builtin resolver validation
	if c.Builtin.ExternalOn {
		if len(c.Builtin.RemoteDNS) == 0 {
			c.Builtin.RemoteDNS = GetLocalDNS()
		}
		if err = validateRemoteDNS(c.Builtin.RemoteDNS); err != nil {
			logging.Error.Fatalf("Remote servers validation failed: %v", err)
		}
	}

	// SOA record fields
	c.SOARname = strings.TrimRight(strings.Replace(c.Builtin.SOARname, "@", ".", -1), ".") + "."
	c.SOAMname = strings.TrimRight(c.Builtin.SOAMname, ".") + "."
	c.SOASerial = uint32(time.Now().Unix())

}
