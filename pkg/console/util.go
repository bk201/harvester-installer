package console

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/jroimartin/gocui"
	"github.com/pkg/errors"
	cfg "github.com/rancher/harvester-installer/pkg/config"
	"github.com/rancher/k3os/pkg/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

const (
	defaultHTTPTimeout = 15 * time.Second
	automaticCmdline   = "harvester.automatic"
)

func getURL(url string, timeout time.Duration) ([]byte, error) {
	client := http.Client{
		Timeout: timeout,
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("got %d status code from %s, body: %s", resp.StatusCode, url, string(body))
	}

	return body, nil
}

func getRemoteSSHKeys(url string) ([]string, error) {
	b, err := getURL(url, defaultHTTPTimeout)
	if err != nil {
		return nil, err
	}

	var keys []string
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, errors.Errorf("fail to parse on line %d: %s", i+1, line)
		}
		keys = append(keys, line)
	}
	if len(keys) == 0 {
		return nil, errors.Errorf(("no key found"))
	}
	return keys, nil
}

func getFormattedServerURL(addr string) string {
	if !strings.HasPrefix(addr, "https://") {
		addr = "https://" + addr
	}
	if !strings.HasSuffix(addr, ":6443") {
		addr = addr + ":6443"
	}
	return addr
}

func getServerURLFromEnvData(data []byte) (string, error) {
	regexp, err := regexp.Compile("K3S_URL=(.*)\\b")
	if err != nil {
		return "", err
	}
	matches := regexp.FindSubmatch(data)
	if len(matches) == 2 {
		serverURL := string(matches[1])
		i := strings.LastIndex(serverURL, ":")
		if i >= 0 {
			return serverURL[:i] + ":8443", nil
		}
	}
	return "", nil
}

func showNext(c *Console, names ...string) error {
	for _, name := range names {
		v, err := c.GetElement(name)
		if err != nil {
			return err
		}
		if err := v.Show(); err != nil {
			return err
		}
	}

	validatorV, err := c.GetElement(validatorPanel)
	if err != nil {
		return err
	}
	if err := validatorV.Close(); err != nil {
		return err
	}
	return nil
}

func toCloudConfig(cfg *cfg.HarvesterConfig) *config.CloudConfig {
	cloudConfig := &config.CloudConfig{
		K3OS: config.K3OS{
			Install: &config.Install{},
		},
	}

	// cfg
	cloudConfig.K3OS.ServerURL = cfg.ServerURL
	cloudConfig.K3OS.Token = cfg.Token

	// cfg.OS
	cloudConfig.SSHAuthorizedKeys = copyStringSlice(cfg.OS.SSHAuthorizedKeys)
	cloudConfig.Hostname = cfg.OS.Hostname
	cloudConfig.K3OS.Modules = copyStringSlice(cfg.OS.Modules)
	cloudConfig.K3OS.Sysctls = copyMap(cfg.OS.Sysctls)
	cloudConfig.K3OS.NTPServers = copyStringSlice(cfg.OS.NTPServers)
	cloudConfig.K3OS.DNSNameservers = copyStringSlice(cfg.OS.DNSNameservers)
	if cfg.OS.Wifi != nil {
		cloudConfig.K3OS.Wifi = make([]config.Wifi, len(cfg.Wifi))
		for i, w := range cfg.Wifi {
			cloudConfig.K3OS.Wifi[i].Name = w.Name
			cloudConfig.K3OS.Wifi[i].Passphrase = w.Passphrase
		}
	}
	cloudConfig.K3OS.Password = cfg.OS.Password
	cloudConfig.K3OS.Environment = copyMap(cfg.OS.Environment)

	// cfg.OS.Install
	cloudConfig.K3OS.Install.ForceEFI = cfg.Install.ForceEFI
	cloudConfig.K3OS.Install.Device = cfg.Install.Device
	cloudConfig.K3OS.Install.Silent = cfg.Install.Silent
	cloudConfig.K3OS.Install.ISOURL = cfg.Install.ISOURL
	cloudConfig.K3OS.Install.PowerOff = cfg.Install.PowerOff
	cloudConfig.K3OS.Install.NoFormat = cfg.Install.NoFormat
	cloudConfig.K3OS.Install.Debug = cfg.Install.Debug
	cloudConfig.K3OS.Install.TTY = cfg.Install.TTY

	// k3os & k3s
	cloudConfig.K3OS.Labels = map[string]string{
		"harvester.cattle.io/managed": "true",
	}

	var extraK3sArgs []string
	if cfg.Install.MgmtInterface != "" {
		extraK3sArgs = []string{"--flannel-iface", cfg.Install.MgmtInterface}
	}

	if cfg.Install.Mode == modeJoin {
		cloudConfig.K3OS.K3sArgs = append([]string{"agent"}, extraK3sArgs...)
		return cloudConfig
	}

	var harvesterChartValues = map[string]string{
		"minio.persistence.storageClass":                "longhorn",
		"containers.apiserver.image.imagePullPolicy":    "IfNotPresent",
		"harvester-network-controller.image.pullPolicy": "IfNotPresent",
		"service.harvester.type":                        "LoadBalancer",
		"containers.apiserver.authMode":                 "localUser",
		"multus.enabled":                                "true",
		"longhorn.enabled":                              "true",
	}

	cloudConfig.WriteFiles = []config.File{
		{
			Owner:              "root",
			Path:               "/var/lib/rancher/k3s/server/manifests/harvester.yaml",
			RawFilePermissions: "0600",
			Content:            getHarvesterManifestContent(harvesterChartValues),
		},
	}
	cloudConfig.K3OS.Labels["svccontroller.k3s.cattle.io/enablelb"] = "true"
	cloudConfig.K3OS.K3sArgs = append([]string{
		"server",
		"--cluster-init",
		"--disable",
		"local-storage",
	}, extraK3sArgs...)

	return cloudConfig
}

func doInstall(g *gocui.Gui, cloudConfig *config.CloudConfig) error {
	var (
		err      error
		tempFile *os.File
	)

	// if cfg.Config.K3OS.Install.ConfigURL != "" {
	// 	remoteConfig, err := getRemoteCloudConfig(cfg.Config.K3OS.Install.ConfigURL)
	// 	if err != nil {
	// 		printToInstallPanel(g, err.Error())
	// 	} else if err := mergo.Merge(&cfg.Config.CloudConfig, remoteConfig, mergo.WithAppendSlice); err != nil {
	// 		printToInstallPanel(g, err.Error())
	// 	}
	// }

	tempFile, err = ioutil.TempFile("/tmp", "k3os.XXXXXXXX")
	if err != nil {
		return err
	}
	defer tempFile.Close()

	// no good, modify cloudConfig pointer
	cloudConfig.K3OS.Install.ConfigURL = tempFile.Name()

	// cfg.Config.K3OS.Install.ConfigURL = tempFile.Name()

	ev, err := config.ToEnv(*cloudConfig)
	if err != nil {
		return err
	}
	if tempFile != nil {
		cloudConfig.K3OS.Install = nil
		bytes, err := yaml.Marshal(cloudConfig)
		if err != nil {
			return err
		}
		if _, err := tempFile.Write(bytes); err != nil {
			return err
		}
		if err := tempFile.Close(); err != nil {
			return err
		}
		// defer os.Remove(tempFile.Name())
	}
	// cmd := exec.Command("/usr/libexec/k3os/install")
	cmd := exec.Command("/root/install")
	cmd.Env = append(os.Environ(), ev...)
	logrus.Infof("env: %v", cmd.Env)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		printToInstallPanel(g, scanner.Text())
	}
	scanner = bufio.NewScanner(stderr)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		printToInstallPanel(g, scanner.Text())
	}
	return nil
}

func printToInstallPanel(g *gocui.Gui, message string) {
	g.Update(func(g *gocui.Gui) error {
		v, err := g.View(installPanel)
		if err != nil {
			return err
		}
		fmt.Fprintln(v, message)

		lines := len(v.BufferLines())
		_, sy := v.Size()
		if lines > sy {
			ox, oy := v.Origin()
			v.SetOrigin(ox, oy+1)
		}
		return nil
	})
}

func getRemoteConfig(configURL string) (*cfg.HarvesterConfig, error) {
	b, err := getURL(configURL, defaultHTTPTimeout)
	if err != nil {
		return nil, err
	}
	harvestCfg, err := cfg.ToHarvesterConfig(b)
	if err != nil {
		return nil, err
	}
	return harvestCfg, nil
}

func getHarvesterManifestContent(values map[string]string) string {
	base := `apiVersion: v1
kind: Namespace
metadata:
  name: harvester-system
---
apiVersion: helm.cattle.io/v1
kind: HelmChart
metadata:
  name: harvester
  namespace: kube-system
spec:
  chart: https://%{KUBERNETES_API}%/static/charts/harvester-0.1.0.tgz
  targetNamespace: harvester-system
  set:
`
	var buffer = bytes.Buffer{}
	buffer.WriteString(base)
	for k, v := range values {
		buffer.WriteString(fmt.Sprintf("    %s: %q\n", k, v))
	}
	return buffer.String()
}

// func validateAutomaticInstall(config *cfg.HarvesterConfig) error {
func validateConfig(config *cfg.HarvesterConfig) error {
	logrus.Infof("Validating config: %+v, Install: %+v", config, config.Install)

	// modes
	switch mode := config.Install.Mode; mode {
	case modeCreate:
		if config.ServerURL != "" {
			return errors.Errorf("ServerURL need to be empty in %q mode", mode)
		}
	case modeJoin:
		if config.ServerURL == "" {
			return errors.Errorf("ServerURL can't be empty in %q mode", mode)
		}
		if config.Token == "" {
			return errors.Errorf("Token can't be empty in %q mode", mode)
		}
	default:
		return errors.Errorf("Install.Mode must be %q or %q", modeCreate, modeJoin)
	}

	// validate either ssh key or password  must set
	if len(config.SSHAuthorizedKeys) == 0 && config.Password == "" {
		return errors.Errorf("No SSH keys or password are set")
	}

	if err := validateMgmtInterface(config.Install.MgmtInterface); err != nil {
		return err
	}

	if err := validateDevice(config.Install.Device); err != nil {
		return err
	}
	return nil
}

func validateMgmtInterface(name string) error {
	if name == "" {
		return errors.New("no management interface specified")
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	for _, i := range ifaces {
		if i.Name == name {
			if i.Flags&net.FlagLoopback != 0 {
				return errors.Errorf("interface %q is a loopback interface", name)
			}
			return nil
		}
	}
	return errors.Errorf("interface %q is not found", name)
}

func validateDevice(device string) error {
	if device == "" {
		return errors.New("no device specified")
	}
	options, err := getDiskOptions()
	if err != nil {
		return err
	}
	for _, option := range options {
		if device == option.Value {
			return nil
		}
	}
	return errors.Errorf("device %q not found", device)
}

func copyStringSlice(src []string) []string {
	if src == nil {
		return nil
	}
	s := make([]string, len(src))
	copy(s, src)
	return s
}

func copyMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	m := make(map[string]string)
	for k, v := range src {
		m[k] = v
	}
	return m
}
