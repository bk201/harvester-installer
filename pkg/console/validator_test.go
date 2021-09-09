package console

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/harvester/harvester-installer/pkg/config"
	"github.com/harvester/harvester-installer/pkg/util"
)

type FakeValidator struct {
	hasInterfaces []string
	hasDevices    []string
}

func (v FakeValidator) CheckInterface(name string) error {
	for _, i := range v.hasInterfaces {
		if i == name {
			return nil
		}
	}
	return prettyError(ErrMsgInterfaceNotFound, name)
}

func (v FakeValidator) CheckDevice(device string) error {
	for _, d := range v.hasDevices {
		if d == device {
			return nil
		}
	}
	return prettyError(ErrMsgDeviceNotFound, device)
}

func createDefaultFakeValidator() FakeValidator {
	return FakeValidator{
		hasInterfaces: []string{"eth0"},
		hasDevices:    []string{"/dev/vda"},
	}
}

func loadConfig(t *testing.T, name string) *config.HarvesterConfig {
	c, err := config.LoadHarvesterConfig(util.LoadFixture(t, name))
	if err != nil {
		t.Fatal("fail to load config: ", err)
	}
	return c
}

func TestValidateConfig(t *testing.T) {
	createCreateConfig := func() *config.HarvesterConfig {
		return &config.HarvesterConfig{
			Token: "token",
			OS: config.OS{
				SSHAuthorizedKeys: []string{"github: someuser"},
				Password:          "password",
			},
			Install: config.Install{
				Mode:          config.ModeCreate,
				MgmtInterface: "eth0",
				Device:        "/dev/vda",
				Networks: []config.Network{
					{
						Interface: "eth0",
						Method:    config.NetworkMethodDHCP,
					},
				},
				Vip:       "192.168.0.100",
				VipMode:   config.NetworkMethodDHCP,
				VipHwAddr: "52:54:00:de:ad:aa",
			},
		}
	}

	createJoinConfig := func() *config.HarvesterConfig {
		c := createCreateConfig()
		c.ServerURL = "https://somewhere"
		c.Mode = config.ModeJoin
		return c
	}

	testCases := []struct {
		name     string
		cfg      *config.HarvesterConfig
		preApply func(c *config.HarvesterConfig)
		errMsg   string
	}{
		{
			name: "valid create config",
			cfg:  createCreateConfig(),
		},
		{
			name: "invalid create config: contains server URL",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.ServerURL = "https://somewhere"
			},
			errMsg: ErrMsgModeCreateContainsServerURL,
		},
		{
			name: "invalid config: unknown mode",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.Mode = "asdf"
			},
			errMsg: ErrMsgModeUnknown,
		},
		{
			name: "valid join config",
			cfg:  createJoinConfig(),
		},
		{
			name: "invalid join config: no server URL",
			cfg:  createJoinConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.ServerURL = ""
			},
			errMsg: ErrMsgModeJoinServerURLNotSpecified,
		},
		{
			name: "invalid create config: contains no credential",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.SSHAuthorizedKeys = nil
				c.Password = ""
			},
			errMsg: ErrMsgNoCredentials,
		},
		{
			name: "invalid create config: device not found",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.Device = "/dev/vdb"
			},
			errMsg: ErrMsgDeviceNotFound,
		},
		{
			name: "invalid create config: mgmt interface not found",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.MgmtInterface = "eth1"
			},
			errMsg: ErrMsgInterfaceNotFound,
		},
		{
			name: "invalid create config: network interface not found",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.Networks[0].Interface = "eth1"
			},
			errMsg: ErrMsgInterfaceNotFound,
		},
		{
			name: "invalid create config: bad VIP",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.Install.Vip = "x"
			},
			errMsg: ErrMsgVIPInvalidVIPAddr,
		},
		{
			name: "invalid create config: VIP DHCP mode without hardware address",
			cfg:  createCreateConfig(),
			preApply: func(c *config.HarvesterConfig) {
				c.Install.VipHwAddr = ""
			},
			errMsg: ErrMsgVIPInvalidHWAddr,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.preApply != nil {
				testCase.preApply(testCase.cfg)
			}
			err := validateConfig(createDefaultFakeValidator(), testCase.cfg)
			if testCase.errMsg == "" {
				assert.Nil(t, err)
			} else {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), testCase.errMsg)
			}
		})
	}
}
