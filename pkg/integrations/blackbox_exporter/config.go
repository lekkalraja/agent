package blackbox_exporter //nolint:golint

import (
	"github.com/go-kit/kit/log"
	"github.com/grafana/agent/pkg/integrations"
	"github.com/grafana/agent/pkg/integrations/config"
	bbc "github.com/prometheus/blackbox_exporter/config"
)

// Config controls the blackbox_exporter integration.
type Config struct {
	Common  config.Common         `yaml:",inline"`
	Modules map[string]bbc.Module `yaml:"modules"`
	Targets []Target              `yaml:"targets"`
}

type Target struct {
	Target string  `yaml:"target"`
	Module string  `yaml:"module"`
	Labels []Label `yaml:"labels`
}

type Label struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

// UnmarshalYAML implements yaml.Unmarshaler for Config.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	sc := &bbc.SafeConfig{
		C: &bbc.Config{},
	}

	sc.ReloadConfig("default_data/blackbox.yml", nil)

	*c = Config{
		Modules: sc.C.Modules,
	}

	type plain Config
	return unmarshal((*plain)(c))
}

// Name returns the name of the integration that this config is for.
func (c *Config) Name() string {
	return "blackbox_exporter"
}

// CommonConfig returns the set of common settings shared across all integrations.
func (c *Config) CommonConfig() config.Common {
	return c.Common
}

// NewIntegration converts this config into an instance of an integration.
func (c *Config) NewIntegration(l log.Logger) (integrations.Integration, error) {
	return New(l, c)
}

func init() {
	integrations.RegisterIntegration(&Config{})
}
