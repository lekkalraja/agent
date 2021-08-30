// Package blackbox_exporter embeds https://github.com/prometheus/blackbox_exporter
package blackbox_exporter //nolint:golint

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/agent/pkg/integrations/config"
	"github.com/prometheus/blackbox_exporter/prober"
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

var Probers = map[string]prober.ProbeFn{
	"http": prober.ProbeHTTP,
	"tcp":  prober.ProbeTCP,
	"icmp": prober.ProbeICMP,
	"dns":  prober.ProbeDNS,
}

// New creates a new node_exporter integration.
func New(log log.Logger, c *Config) (*Integration, error) {
	level.Info(log).Log("msg", "Starting blackbox_exporter", "version", version.Info())
	level.Info(log).Log("build_context", version.BuildContext())
	level.Info(log).Log("Cofig", c.Modules)

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
	level.Info(i.logger).Log("msg", "MetricsHandler.......................")
	probeSuccessGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "Displays whether or not the probe was a success",
	}, []string{"target"})
	probeDurationGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "Returns how long the probe took to complete in seconds",
	}, []string{"target"})
	gatherers := prometheus.Gatherers{i.exporterMetricsRegistry}
	for _, target := range i.c.Targets {
		registry := prometheus.NewRegistry()
		registry.MustRegister(probeSuccessGauge)
		registry.MustRegister(probeDurationGauge)
		module := i.c.Modules[target.Module]
		prober, ok := Probers[module.Prober]
		if !ok {
			level.Warn(i.logger).Log(fmt.Sprintf("Unknown prober %q", module.Prober), http.StatusBadRequest)
		}
		start := time.Now()
		success := prober(context.Background(), target.Target, module, registry, i.logger)
		duration := time.Since(start).Seconds()
		probeDurationGauge.WithLabelValues(target.Target).Set(duration)
		if success {
			probeSuccessGauge.WithLabelValues(target.Target).Set(1)
			level.Info(i.logger).Log("msg", "Probe succeeded", "duration_seconds", duration)
		} else {
			probeSuccessGauge.WithLabelValues(target.Target).Set(0)
			level.Error(i.logger).Log("msg", "Probe failed", "duration_seconds", duration)
		}
		// Register blackbox_exporter_build_info metrics, generally useful for
		// dashboards that depend on them for discovering targets.
		if err := registry.Register(version.NewCollector(i.c.Name())); err != nil {
			return nil, fmt.Errorf("couldn't register %s: %w", i.c.Name(), err)
		}
		gatherers = append(gatherers, registry)
	}
	handler := promhttp.HandlerFor(
		gatherers,
		promhttp.HandlerOpts{
			ErrorHandling:       promhttp.ContinueOnError,
			MaxRequestsInFlight: 0,
			Registry:            i.exporterMetricsRegistry,
		},
	)
	return handler, nil
}

// ScrapeConfigs satisfies Integration.ScrapeConfigs.
func (i *Integration) ScrapeConfigs() []config.ScrapeConfig {
	level.Info(i.logger).Log("msg", "ScrapeConfigs.......................")
	return []config.ScrapeConfig{{
		JobName:     i.c.Name(),
		MetricsPath: "/metrics",
	}}
}

// Run satisfies Integration.Run.
func (i *Integration) Run(ctx context.Context) error {
	level.Info(i.logger).Log("msg", "Run.......................")
	// We don't need to do anything here, so we can just wait for the context to
	// finish.
	<-ctx.Done()
	return ctx.Err()
}
