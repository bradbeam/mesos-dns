package consul

type Config struct {
	Address       string
	CacheOnly     bool
	CacheRefresh  int
	Datacenter    string
	LookupOrder   []string
	Scheme        string
	ServicePrefix string
	Token         string
}

func NewConfig() *Config {
	return &Config{
		Address:       "127.0.0.1:8500",
		CacheOnly:     false,
		CacheRefresh:  3,
		LookupOrder:   []string{"docker", "netinfo", "host"},
		Scheme:        "http",
		ServicePrefix: "mesos-dns",
	}
}
