package config

import (
	"github.com/rancher/k3os/pkg/config"
)

var (
	Config = InstallConfig{}
)

type Harvester struct {
	Automatic     bool   `json:"automatic,omitempty"`
	InstallMode   string `json:"installMode,omitempty"`
	MgmtInterface string `json:"mgmtInterface,omitempty"`
}

type InstallConfig struct {
	config.CloudConfig

	Harvester `json:"harvester,omitempty"`
}
