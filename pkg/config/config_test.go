package config

import (
	"flag"
	"os"
	"testing"
	"time"

	"github.com/grafana/agent/pkg/integrations"
	"github.com/grafana/agent/pkg/metrics"
	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/grafana/agent/pkg/util"
	"github.com/prometheus/common/model"
	promCfg "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

// TestConfig_FlagDefaults makes sure that default values of flags are kept
// when parsing the config.
func TestConfig_FlagDefaults(t *testing.T) {
	cfg := `
prometheus:
  wal_directory: /tmp/wal
  global:
    scrape_timeout: 33s`

	fs := flag.NewFlagSet("test", flag.ExitOnError)
	c, err := load(fs, []string{"-config.file", "test"}, func(_ string, _ bool, c *Config) error {
		return LoadBytes([]byte(cfg), false, c)
	})
	require.NoError(t, err)
	require.NotEmpty(t, c.Prometheus.ServiceConfig.Lifecycler.InfNames)
	require.NotZero(t, c.Prometheus.ServiceConfig.Lifecycler.NumTokens)
	require.NotZero(t, c.Prometheus.ServiceConfig.Lifecycler.HeartbeatPeriod)
	require.True(t, c.Server.RegisterInstrumentation)
}

func TestConfig_OverrideDefaultsOnLoad(t *testing.T) {
	cfg := `
prometheus:
  wal_directory: /tmp/wal
  global:
    scrape_timeout: 33s`
	expect := instance.GlobalConfig{
		Prometheus: promCfg.GlobalConfig{
			ScrapeInterval:     model.Duration(1 * time.Minute),
			ScrapeTimeout:      model.Duration(33 * time.Second),
			EvaluationInterval: model.Duration(1 * time.Minute),
		},
	}

	fs := flag.NewFlagSet("test", flag.ExitOnError)
	c, err := load(fs, []string{"-config.file", "test"}, func(_ string, _ bool, c *Config) error {
		return LoadBytes([]byte(cfg), false, c)
	})
	require.NoError(t, err)
	require.Equal(t, expect, c.Prometheus.Global)
}

func TestConfig_OverrideByEnvironmentOnLoad(t *testing.T) {
	cfg := `
prometheus:
  wal_directory: /tmp/wal
  global:
    scrape_timeout: ${SCRAPE_TIMEOUT}`
	expect := instance.GlobalConfig{
		Prometheus: promCfg.GlobalConfig{
			ScrapeInterval:     model.Duration(1 * time.Minute),
			ScrapeTimeout:      model.Duration(33 * time.Second),
			EvaluationInterval: model.Duration(1 * time.Minute),
		},
	}
	_ = os.Setenv("SCRAPE_TIMEOUT", "33s")

	fs := flag.NewFlagSet("test", flag.ExitOnError)
	c, err := load(fs, []string{"-config.file", "test"}, func(_ string, _ bool, c *Config) error {
		return LoadBytes([]byte(cfg), true, c)
	})
	require.NoError(t, err)
	require.Equal(t, expect, c.Prometheus.Global)
}

func TestConfig_OverrideByEnvironmentOnLoad_NoDigits(t *testing.T) {
	cfg := `
prometheus:
  wal_directory: /tmp/wal
  global:
    external_labels:
      foo: ${1}`
	expect := labels.Labels{{Name: "foo", Value: "${1}"}}

	fs := flag.NewFlagSet("test", flag.ExitOnError)
	c, err := load(fs, []string{"-config.file", "test"}, func(_ string, _ bool, c *Config) error {
		return LoadBytes([]byte(cfg), true, c)
	})
	require.NoError(t, err)
	require.Equal(t, expect, c.Prometheus.Global.Prometheus.ExternalLabels)
}

func TestConfig_FlagsAreAccepted(t *testing.T) {
	cfg := `
prometheus:
  global:
    scrape_timeout: 33s`

	fs := flag.NewFlagSet("test", flag.ExitOnError)
	args := []string{
		"-config.file", "test",
		"-prometheus.wal-directory", "/tmp/wal",
		"-config.expand-env",
	}

	c, err := load(fs, args, func(_ string, _ bool, c *Config) error {
		return LoadBytes([]byte(cfg), false, c)
	})
	require.NoError(t, err)
	require.Equal(t, "/tmp/wal", c.Prometheus.WALDir)
}

func TestConfig_StrictYamlParsing(t *testing.T) {
	t.Run("duplicate key", func(t *testing.T) {
		cfg := `
prometheus:
  wal_directory: /tmp/wal
  global:
    scrape_timeout: 10s
    scrape_timeout: 15s`
		var c Config
		err := LoadBytes([]byte(cfg), false, &c)
		require.Error(t, err)
	})

	t.Run("non existing key", func(t *testing.T) {
		cfg := `
prometheus:
  wal_directory: /tmp/wal
  global:
  scrape_timeout: 10s`
		var c Config
		err := LoadBytes([]byte(cfg), false, &c)
		require.Error(t, err)
	})
}

func TestConfig_Defaults(t *testing.T) {
	var c Config
	err := LoadBytes([]byte(`{}`), false, &c)
	require.NoError(t, err)

	require.Equal(t, metrics.DefaultConfig, c.Prometheus)
	require.Equal(t, integrations.DefaultManagerConfig, c.Integrations)
}

func TestConfig_TempoLokiValidates(t *testing.T) {
	tests := []struct {
		cfg string
	}{
		{
			cfg: `
loki:
  configs:
  - name: default
    positions:
      filename: /tmp/positions.yaml
    clients:
    - url: http://loki:3100/loki/api/v1/push
tempo:
  configs:
  - name: default
    automatic_logging:
      backend: loki
      loki_name: default
      spans: true`,
		},
		{
			cfg: `
loki:
  configs:
  - name: default
    positions:
      filename: /tmp/positions.yaml
    clients:
    - url: http://loki:3100/loki/api/v1/push
tempo:
  configs:
  - name: default
    automatic_logging:
      backend: stdout
      loki_name: doesnt_exist
      spans: true`,
		},
	}

	for _, tc := range tests {
		fs := flag.NewFlagSet("test", flag.ExitOnError)
		_, err := load(fs, []string{"-config.file", "test"}, func(_ string, _ bool, c *Config) error {
			return LoadBytes([]byte(tc.cfg), false, c)
		})

		require.NoError(t, err)
	}
}

func TestConfig_LokiNameMigration(t *testing.T) {
	input := util.Untab(`
loki:
  configs:
  - name: foo
    positions:
      filename: /tmp/positions.yaml
    clients:
    - url: http://loki:3100/loki/api/v1/push
`)
	var cfg Config
	require.NoError(t, LoadBytes([]byte(input), false, &cfg))
	require.NoError(t, cfg.ApplyDefaults())

	require.Nil(t, cfg.Loki)
	require.NotNil(t, cfg.Logs)
	require.Equal(t, "foo", cfg.Logs.Configs[0].Name)
}

func TestConfig_TempoLokiFailsValidation(t *testing.T) {
	tests := []struct {
		cfg           string
		expectedError string
	}{
		{
			cfg: `
loki:
  configs:
  - name: foo
    positions:
      filename: /tmp/positions.yaml
    clients:
    - url: http://loki:3100/loki/api/v1/push
tempo:
  configs:
  - name: default
    automatic_logging:
      backend: logs_instance
      logs_instance_name: default
      spans: true`,
			expectedError: "error in config file: failed to validate automatic_logging for tempo config default: specified logs config default not found in agent config",
		},
	}

	for _, tc := range tests {
		fs := flag.NewFlagSet("test", flag.ExitOnError)
		_, err := load(fs, []string{"-config.file", "test"}, func(_ string, _ bool, c *Config) error {
			return LoadBytes([]byte(tc.cfg), false, c)
		})

		require.EqualError(t, err, tc.expectedError)
	}
}
