package config

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"unicode"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/server"

	"github.com/drone/envsubst"
	"github.com/grafana/agent/pkg/integrations"
	"github.com/grafana/agent/pkg/logs"
	"github.com/grafana/agent/pkg/metrics"
	"github.com/grafana/agent/pkg/tempo"
	"github.com/grafana/agent/pkg/util"
	"github.com/pkg/errors"
	"github.com/prometheus/common/version"
	"gopkg.in/yaml.v2"
)

// DefaultConfig holds default settings for all the subsystems.
var DefaultConfig = Config{
	// All subsystems with a DefaultConfig should be listed here.
	Prometheus:   metrics.DefaultConfig,
	Integrations: integrations.DefaultManagerConfig,
}

// Config contains underlying configurations for the agent
type Config struct {
	Server       server.Config              `yaml:"server,omitempty"`
	Prometheus   metrics.Config             `yaml:"prometheus,omitempty"`
	Integrations integrations.ManagerConfig `yaml:"integrations,omitempty"`
	Tempo        tempo.Config               `yaml:"tempo,omitempty"`

	Logs               *logs.Config `yaml:"logs,omitempty"`
	Loki               *logs.Config `yaml:"loki,omitempty"` // Deprecated: use Logs instead
	UsedDeprecatedLoki bool         `yaml:"-"`

	// We support a secondary server just for the /-/reload endpoint, since
	// invoking /-/reload against the primary server can cause the server
	// to restart.
	ReloadAddress string `yaml:"-"`
	ReloadPort    int    `yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Apply defaults to the config from our struct and any defaults inherited
	// from flags.
	*c = DefaultConfig
	util.DefaultConfigFromFlags(c)

	type config Config
	return unmarshal((*config)(c))
}

// LogDeprecations will log use of any deprecated fields to l as warn-level
// messages.
func (c *Config) LogDeprecations(l log.Logger) {
	if c.UsedDeprecatedLoki {
		level.Warn(l).Log("msg", "DEPRECATION NOTICE: `loki` is deprecated in favor of `logs`")
	}
}

// ApplyDefaults sets default values in the config
func (c *Config) ApplyDefaults() error {
	if err := c.Prometheus.ApplyDefaults(); err != nil {
		return err
	}

	if c.Logs != nil && c.Loki != nil {
		return fmt.Errorf("at most one of loki and logs should be specified")
	}

	if c.Logs == nil && c.Loki != nil {
		c.Logs = c.Loki
		c.Loki = nil
		c.UsedDeprecatedLoki = true
	}

	if err := c.Integrations.ApplyDefaults(&c.Prometheus); err != nil {
		return err
	}

	c.Prometheus.ServiceConfig.Lifecycler.ListenPort = c.Server.GRPCListenPort
	c.Integrations.ListenPort = c.Server.HTTPListenPort
	c.Integrations.ListenHost = c.Server.HTTPListenAddress

	c.Integrations.ServerUsingTLS = c.Server.HTTPTLSConfig.TLSKeyPath != "" && c.Server.HTTPTLSConfig.TLSCertPath != ""

	if len(c.Integrations.PrometheusRemoteWrite) == 0 {
		c.Integrations.PrometheusRemoteWrite = c.Prometheus.Global.RemoteWrite
	}

	c.Integrations.PrometheusGlobalConfig = c.Prometheus.Global.Prometheus

	// since the Tempo config might rely on an existing Loki config
	// this check is made here to look for cross config issues before we attempt to load
	if err := c.Tempo.Validate(c.Logs); err != nil {
		return err
	}

	return nil
}

// RegisterFlags registers flags in underlying configs
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Server.MetricsNamespace = "agent"
	c.Server.RegisterInstrumentation = true
	c.Prometheus.RegisterFlags(f)
	c.Server.RegisterFlags(f)

	f.StringVar(&c.ReloadAddress, "reload-addr", "127.0.0.1", "address to expose a secondary server for /-/reload on.")
	f.IntVar(&c.ReloadPort, "reload-port", 0, "port to expose a secondary server for /-/reload on. 0 disables secondary server.")
}

// LoadFile reads a file and passes the contents to Load
func LoadFile(filename string, expandEnvVars bool, c *Config) error {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return errors.Wrap(err, "error reading config file")
	}
	return LoadBytes(buf, expandEnvVars, c)
}

// LoadBytes unmarshals a config from a buffer. Defaults are not
// applied to the file and must be done manually if LoadBytes
// is called directly.
func LoadBytes(buf []byte, expandEnvVars bool, c *Config) error {
	// (Optionally) expand with environment variables
	if expandEnvVars {
		s, err := envsubst.Eval(string(buf), getenv)
		if err != nil {
			return fmt.Errorf("unable to substitute config with environment variables: %w", err)
		}
		buf = []byte(s)
	}
	// Unmarshal yaml config
	return yaml.UnmarshalStrict(buf, c)
}

// getenv is a wrapper around os.Getenv that ignores patterns that are numeric
// regex capture groups (ie "${1}").
func getenv(name string) string {
	numericName := true

	for _, r := range name {
		if !unicode.IsDigit(r) {
			numericName = false
			break
		}
	}

	if numericName {
		// We need to add ${} back in since envsubst removes it.
		return fmt.Sprintf("${%s}", name)
	}
	return os.Getenv(name)
}

// Load loads a config file from a flagset. Flags will be registered
// to the flagset before parsing them with the values specified by
// args.
func Load(fs *flag.FlagSet, args []string) (*Config, error) {
	return load(fs, args, LoadFile)
}

// load allows for tests to inject a function for retrieving the config file that
// doesn't require having a literal file on disk.
func load(fs *flag.FlagSet, args []string, loader func(string, bool, *Config) error) (*Config, error) {
	var (
		cfg = DefaultConfig

		printVersion    bool
		file            string
		configExpandEnv bool
	)

	fs.StringVar(&file, "config.file", "", "configuration file to load")
	fs.BoolVar(&printVersion, "version", false, "Print this build's version information")
	fs.BoolVar(&configExpandEnv, "config.expand-env", false, "Expands ${var} in config according to the values of the environment variables.")
	cfg.RegisterFlags(fs)

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("error parsing flags: %w", err)
	}

	if printVersion {
		fmt.Println(version.Print("agent"))
		os.Exit(0)
	}

	if file == "" {
		return nil, fmt.Errorf("-config.file flag required")
	} else if err := loader(file, configExpandEnv, &cfg); err != nil {
		return nil, fmt.Errorf("error loading config file %s: %w", file, err)
	}

	// Parse the flags again to override any YAML values with command line flag
	// values
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("error parsing flags: %w", err)
	}

	// Finally, apply defaults to config that wasn't specified by file or flag
	if err := cfg.ApplyDefaults(); err != nil {
		return nil, fmt.Errorf("error in config file: %w", err)
	}

	return &cfg, nil
}
