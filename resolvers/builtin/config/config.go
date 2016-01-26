package config

type Config struct {
	// Enable serving DNS requests
	DNSOn bool `json:"DnsOn"`
	// Enable replies for external requests
	ExternalOn bool
	// Resolver port: port used to listen for slave requests (default 53)
	Port int
	// Value of RecursionAvailable for responses in Mesos domain
	RecurseOn bool
	// Remote DNS servers: IP address of the DNS server for forwarded accesses
	RemoteServers []string
	// SOA record fields (see http://tools.ietf.org/html/rfc1035#page-18)
	SOAExpire  uint32 // expiration time
	SOAMinttl  uint32 // minimum TTL
	SOAMname   string // primary name server
	SOARefresh uint32 // refresh interval
	SOARetry   uint32 // retry interval
	SOARname   string // email of admin esponsible
	SOASerial  uint32 // initial version number (incremented on refresh)
	// TTL: the TTL value used for SRV and A records (default 60)
	TTL int32
}

// NewConfig return the default config of the resolver
func NewConfig() Config {
	return Config{
		DNSOn:         true,
		ExternalOn:    true,
		Port:          53,
		RecurseOn:     true,
		RemoteServers: []string{"8.8.8.8"},
		SOAExpire:     86400,
		SOAMinttl:     60,
		SOAMname:      "ns1.mesos",
		SOARefresh:    60,
		SOARetry:      600,
		SOARname:      "root.ns1.mesos",
		TTL:           60,
	}
}
