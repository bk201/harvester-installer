package console

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/jroimartin/gocui"
	"github.com/pkg/errors"
	"github.com/rancher/harvester-installer/pkg/config"
	"github.com/rancher/harvester-installer/pkg/util"
	k3os "github.com/rancher/k3os/pkg/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

const (
	defaultHTTPTimeout = 15 * time.Second
	harvesterNodePort  = "30443"
	automaticCmdline   = "harvester.automatic"
)

func getURL(client http.Client, url string) ([]byte, error) {
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

func validateInsecureURL(url string) error {
	client := http.Client{
		Timeout: defaultHTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	_, err := getURL(client, url)
	return err
}

func getRemoteSSHKeys(url string) ([]string, error) {
	client := http.Client{
		Timeout: defaultHTTPTimeout,
	}
	b, err := getURL(client, url)
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

func toCloudConfig(cfg *config.HarvesterConfig) *k3os.CloudConfig {
	cloudConfig := &k3os.CloudConfig{
		K3OS: k3os.K3OS{
			Install: &k3os.Install{},
		},
	}

	// cfg
	cloudConfig.K3OS.ServerURL = cfg.ServerURL
	cloudConfig.K3OS.Token = cfg.Token

	// cfg.OS
	cloudConfig.SSHAuthorizedKeys = util.DupStrings(cfg.OS.SSHAuthorizedKeys)
	cloudConfig.Hostname = cfg.OS.Hostname
	cloudConfig.K3OS.Modules = util.DupStrings(cfg.OS.Modules)
	cloudConfig.K3OS.Sysctls = util.DupStringMap(cfg.OS.Sysctls)
	cloudConfig.K3OS.NTPServers = util.DupStrings(cfg.OS.NTPServers)
	cloudConfig.K3OS.DNSNameservers = util.DupStrings(cfg.OS.DNSNameservers)
	if cfg.OS.Wifi != nil {
		cloudConfig.K3OS.Wifi = make([]k3os.Wifi, len(cfg.Wifi))
		for i, w := range cfg.Wifi {
			cloudConfig.K3OS.Wifi[i].Name = w.Name
			cloudConfig.K3OS.Wifi[i].Passphrase = w.Passphrase
		}
	}
	cloudConfig.K3OS.Password = cfg.OS.Password
	cloudConfig.K3OS.Environment = util.DupStringMap(cfg.OS.Environment)

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
		"multus.enabled":                                "true",
		"longhorn.enabled":                              "true",
		"minio.persistence.storageClass":                "longhorn",
		"harvester-network-controller.image.pullPolicy": "IfNotPresent",
		"containers.apiserver.image.imagePullPolicy":    "IfNotPresent",
		"containers.apiserver.authMode":                 "localUser",
		"service.harvester.type":                        "NodePort",
		"service.harvester.httpsNodePort":               harvesterNodePort,
	}

	cloudConfig.WriteFiles = []k3os.File{
		{
			Owner:              "root",
			Path:               "/var/lib/rancher/k3s/server/manifests/harvester.yaml",
			RawFilePermissions: "0600",
			Content:            getHarvesterManifestContent(harvesterChartValues),
		},
	}
	cloudConfig.K3OS.K3sArgs = append([]string{
		"server",
		"--cluster-init",
		"--disable",
		"local-storage",
		"--disable",
		"servicelb",
		"--disable",
		"traefik",
	}, extraK3sArgs...)

	return cloudConfig
}

func execute(g *gocui.Gui, env []string, cmdName string) error {
	cmd := exec.Command(cmdName)
	cmd.Env = env
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

func doInstall(g *gocui.Gui, cloudConfig *k3os.CloudConfig, webhooks RendererWebhooks) error {
	webhooks.Handle(EventInstallStarted)

	var (
		err      error
		tempFile *os.File
	)

	tempFile, err = ioutil.TempFile("/tmp", "k3os.XXXXXXXX")
	if err != nil {
		return err
	}
	defer tempFile.Close()

	cloudConfig.K3OS.Install.ConfigURL = tempFile.Name()

	ev, err := k3os.ToEnv(*cloudConfig)
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
		defer os.Remove(tempFile.Name())
	}

	env := append(os.Environ(), ev...)
	if err := execute(g, env, "/usr/libexec/k3os/install"); err != nil {
		webhooks.Handle(EventInstallFailed)
		return err
	}
	webhooks.Handle(EventInstallSuceeded)
	if err := execute(g, env, "/usr/libexec/k3os/shutdown"); err != nil {
		return err
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

func getRemoteConfig(configURL string) (*config.HarvesterConfig, error) {
	client := http.Client{
		Timeout: defaultHTTPTimeout,
	}
	b, err := getURL(client, configURL)
	if err != nil {
		return nil, err
	}
	harvestCfg, err := config.LoadHarvesterConfig(b)
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
  chart: https://%{KUBERNETES_API}%/static/charts/harvester-dev.tgz
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
