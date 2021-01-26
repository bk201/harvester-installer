package config

import (
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	"github.com/rancher/k3os/pkg/config"
	"github.com/rancher/mapper/convert"
	"github.com/rancher/mapper/values"
)

// ReadConfig reads Harvester config from various sources
func ReadConfig() (InstallConfig, error) {
	result := InstallConfig{}
	result.CloudConfig.K3OS = config.K3OS{Install: &config.Install{}}
	data, err := readCmdline()
	if err != nil {
		return result, err
	}
	schema.Mapper.ToInternal(data)
	return result, convert.ToObj(data, &result)
}

func readCmdline() (map[string]interface{}, error) {
	//supporting regex https://regexr.com/4mq0s
	parser, err := regexp.Compile(`(\"[^\"]+\")|([^\s]+=(\"[^\"]+\")|([^\s]+))`)
	if err != nil {
		return nil, nil
	}

	bytes, err := ioutil.ReadFile("/proc/cmdline")
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	data := map[string]interface{}{}
	for _, item := range parser.FindAllString(string(bytes), -1) {
		parts := strings.SplitN(item, "=", 2)
		value := "true"
		if len(parts) > 1 {
			value = strings.Trim(parts[1], `"`)
		}
		keys := strings.Split(strings.Trim(parts[0], `"`), ".")
		existing, ok := values.GetValue(data, keys...)
		if ok {
			switch v := existing.(type) {
			case string:
				values.PutValue(data, []string{v, value}, keys...)
			case []string:
				values.PutValue(data, append(v, value), keys...)
			}
		} else {
			values.PutValue(data, value, keys...)
		}
	}

	return data, nil
}
