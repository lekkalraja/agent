// Package blackbox_exporter embeds https://github.com/prometheus/blackbox_exporter
package blackbox_exporter //nolint:golint

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/agent/pkg/integrations/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
)

// Integration is the node_exporter integration. The integration scrapes metrics
type Integration struct {
	c                       *Config
	logger                  log.Logger
	exporterMetricsRegistry *prometheus.Registry
}

// New creates a new node_exporter integration.
func New(log log.Logger, c *Config) (*Integration, error) {
	/*promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("blackbox_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)*/

	level.Info(log).Log("msg", "Starting blackbox_exporter", "version", version.Info())
	level.Info(log).Log("build_context", version.BuildContext())

	hup := make(chan os.Signal, 1)
	reloadCh := make(chan chan error)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				level.Info(log).Log("msg", "Reloaded config file")
			case rc := <-reloadCh:
				level.Info(log).Log("msg", "Reloaded config file")
				rc <- nil
			}
		}
	}()
	return &Integration{
		c:                       c,
		logger:                  log,
		exporterMetricsRegistry: prometheus.NewRegistry(),
	}, nil
}

// MetricsHandler implements Integration.
func (i *Integration) MetricsHandler() (http.Handler, error) {
	probeSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "Displays whether or not the probe was a success",
	})
	probeDurationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "Returns how long the probe took to complete in seconds",
	})

	registry := prometheus.NewRegistry()
	registry.MustRegister(probeSuccessGauge)
	registry.MustRegister(probeDurationGauge)

	handler := promhttp.HandlerFor(
		prometheus.Gatherers{i.exporterMetricsRegistry, registry},
		promhttp.HandlerOpts{
			ErrorHandling:       promhttp.ContinueOnError,
			MaxRequestsInFlight: 0,
			Registry:            i.exporterMetricsRegistry,
		},
	)

	// Register blackbox_exporter_build_info metrics, generally useful for
	// dashboards that depend on them for discovering targets.
	if err := registry.Register(version.NewCollector(i.c.Name())); err != nil {
		return nil, fmt.Errorf("couldn't register %s: %w", i.c.Name(), err)
	}

	return handler, nil
}

// ScrapeConfigs satisfies Integration.ScrapeConfigs.
func (i *Integration) ScrapeConfigs() []config.ScrapeConfig {
	return []config.ScrapeConfig{{
		JobName:     i.c.Name(),
		MetricsPath: "/metrics",
	}}
}

// Run satisfies Integration.Run.
func (i *Integration) Run(ctx context.Context) error {
	// We don't need to do anything here, so we can just wait for the context to
	// finish.
	<-ctx.Done()
	return ctx.Err()
}
