// Package citi_blackbox_exporter embeds https://github.com/prometheus/blackbox_exporter
package citi_blackbox_exporter //nolint:golint

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
	"github.com/prometheus/blackbox_exporter/prober"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/version"
)

// Integration is the citi_blackbox_exporter integration. The integration scrapes metrics
type Integration struct {
	c                       *Config
	logger                  log.Logger
	exporterMetricsRegistry *prometheus.Registry
}

// Probers have prober type and it's handler to probe the target
var Probers = map[string]prober.ProbeFn{
	"http": prober.ProbeHTTP,
	"tcp":  prober.ProbeTCP,
	"icmp": prober.ProbeICMP,
	"dns":  prober.ProbeDNS,
}

// New creates a new citi_blackbox_exporter integration.
func New(log log.Logger, c *Config) (*Integration, error) {
	level.Info(log).Log("msg", "Starting citi_blackbox_exporter", "version", version.Info())
	level.Info(log).Log("build_context", version.BuildContext())
	level.Info(log).Log("Cofig", c.Modules)

	// Reload Config with SIGHUP signal
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
	gatherers := prometheus.Gatherers{i.exporterMetricsRegistry}
	for _, target := range i.c.Targets {
		registry := prometheus.NewRegistry()
		module := i.c.Modules[target.Module]
		prober, ok := Probers[module.Prober]
		if !ok {
			level.Warn(i.logger).Log(fmt.Sprintf("Unknown prober %q", module.Prober), http.StatusBadRequest)
		}
		prober(context.Background(), target.Target, module, registry, i.logger)
		// Register citi_blackbox_exporter_build_info metrics, generally useful for
		// dashboards that depend on them for discovering targets.
		if err := registry.Register(version.NewCollector(i.c.Name())); err != nil {
			return nil, fmt.Errorf("couldn't register %s: %w", i.c.Name(), err)
		}
		fr := i.GetFinalRegistry(registry, target)
		gatherers = append(gatherers, fr)
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

//GetFinalRegistry Prepares new Registry with the fetching metrics along with the additional labels
func (i *Integration) GetFinalRegistry(registry *prometheus.Registry, target Target) *prometheus.Registry {
	finalRegistry := prometheus.NewRegistry()
	mfs, _ := registry.Gather()
	for _, mf := range mfs {
		metrics := mf.GetMetric()
		ls := i.GetLabels(metrics, target)
		//Creating new Gauge Metric
		newMetric := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: *mf.Name,
			Help: *mf.Help,
		}, ls)
		//Adding All (Existing, Global & Target Level) Labels to the newly created metric
		for _, m := range metrics {
			finalLabels := make(prometheus.Labels)
			labels := m.GetLabel()
			for _, label := range labels {
				finalLabels[*label.Name] = *label.Value
			}
			for k, v := range i.c.Labels {
				finalLabels[k] = v
			}
			for k, v := range target.Labels {
				finalLabels[k] = v
			}
			finalLabels["target"] = target.Target
			for _, l := range ls {
				_, ok := finalLabels[l]
				if !ok {
					finalLabels[l] = "NOT_EXIST"
				}
			}
			newMetric.With(finalLabels).Add(*m.Gauge.Value)
		}
		//Registering the new metric
		finalRegistry.MustRegister(newMetric)
	}
	return finalRegistry
}

//GetLabels prepare the labels for metrics to append
func (i *Integration) GetLabels(ms []*io_prometheus_client.Metric, target Target) []string {
	var ls []string
	//Adding Exising labels to the metric
	for _, m := range ms {
		labels := m.GetLabel()
		for _, label := range labels {
			name := *label.Name
			if exist(ls, name) {
				ls = append(ls, name)
			}
		}
	}
	//Adding Global labels to the metric
	for gl := range i.c.Labels {
		ls = append(ls, gl)
	}
	// Adding Target Level labels to the metric
	for tl := range target.Labels {
		ls = append(ls, tl)
	}
	ls = append(ls, "target")
	return ls
}

//exists check whether slice is having element or not
func exist(ls []string, e string) bool {
	for _, l := range ls {
		if l == e {
			return false
		}
	}
	return true
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
