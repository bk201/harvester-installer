package config

import (
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/rancher/k3os/pkg/config"
	"github.com/rancher/mapper"
	"github.com/rancher/mapper/convert"
)

var (
	schemas = mapper.NewSchemas().Init(func(s *mapper.Schemas) *mapper.Schemas {
		s.DefaultMappers = func() []mapper.Mapper {
			return []mapper.Mapper{
				config.NewToMap(),
				config.NewToSlice(),
				config.NewToBool(),
				&config.FuzzyNames{},
			}
		}
		return s
	}).MustImport(InstallConfig{})
	schema = schemas.Schema("installConfig")
)

func ToInstallConfig(yamlBytes []byte) (*InstallConfig, error) {
	result := &InstallConfig{}
	result.CloudConfig.K3OS = config.K3OS{Install: &config.Install{}}
	data := map[string]interface{}{}
	if err := yaml.Unmarshal(yamlBytes, &data); err != nil {
		return result, fmt.Errorf("failed to unmarshal yaml: %v", err)
	}
	schema.Mapper.ToInternal(data)
	return result, convert.ToObj(data, result)
}
