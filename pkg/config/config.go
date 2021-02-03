package config

type Install struct {
	Mode          string `json:"mode,omitempty"`
	MgmtInterface string `json:"mgmtInterface,omitempty"`

	ForceEFI bool   `json:"forceEfi,omitempty"`
	Device   string `json:"device,omitempty"`
	// ConfigURL string `json:"configUrl,omitempty"`
	Silent   bool   `json:"silent,omitempty"`
	ISOURL   string `json:"isoUrl,omitempty"`
	PowerOff bool   `json:"powerOff,omitempty"`
	NoFormat bool   `json:"noFormat,omitempty"`
	Debug    bool   `json:"debug,omitempty"`
	TTY      string `json:"tty,omitempty"`
}

type Wifi struct {
	Name       string `json:"name,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type OS struct {
	SSHAuthorizedKeys []string `json:"sshAuthorizedKeys,omitempty"`
	Hostname          string   `json:"hostname,omitempty"`

	// DataSources    []string          `json:"dataSources,omitempty"`
	Modules        []string          `json:"modules,omitempty"`
	Sysctls        map[string]string `json:"sysctls,omitempty"`
	NTPServers     []string          `json:"ntpServers,omitempty"`
	DNSNameservers []string          `json:"dnsNameservers,omitempty"`
	Wifi           []Wifi            `json:"wifi,omitempty"`
	Password       string            `json:"password,omitempty"`
	// ServerURL      string            `json:"serverUrl,omitempty"`
	// Token          string            `json:"token,omitempty"`
	// Labels         map[string]string `json:"labels,omitempty"`
	// K3sArgs        []string          `json:"k3sArgs,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	// Taints         []string          `json:"taints,omitempty"`
	Install *Install `json:"install,omitempty"`
}

type HarvesterConfig struct {
	ServerURL string `json:"serverUrl,omitempty"`
	Token     string `json:"token,omitempty"`

	OS `json:"os,omitempty"`
}

func NewHarvesterConfig() *HarvesterConfig {
	return &HarvesterConfig{
		OS: OS{
			Install: &Install{},
		},
	}
}
