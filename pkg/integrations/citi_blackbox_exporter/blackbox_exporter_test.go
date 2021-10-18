package citi_blackbox_exporter //nolint:golint

import (
	"bytes"
	"io"
	"io/ioutil"
	logv2 "log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/gorilla/mux"
	"github.com/prometheus/prometheus/pkg/textparse"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadConfig(name string) Config {
	var c Config
	data, err := os.ReadFile(name)
	if err != nil {
		logv2.Fatalf("Config File : %s Not found to load : %v", name, err)
	}
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		logv2.Fatalf("cannot unmarshal data: %v", err)
	}
	logv2.Printf("Loaded Config : %+v", c)
	return c
}

func TestCitiBalckBoxExporterCases(t *testing.T) {
	tt := []struct {
		name                   string
		cfg                    Config
		expectedMetrics        []string
		expectConstructorError bool
	}{
		// Test that exporter metrics are included when configured to do so.
		{
			name: "Include exporter metrics",
			cfg:  loadConfig("test_data/valid_config.yaml"),
			expectedMetrics: []string{
				"probe_http_duration_seconds",
				"probe_http_status_code",
			},
		},
	}

	logger := log.NewNopLogger()

	for _, test := range tt {

		t.Run(test.name, func(t *testing.T) {
			integration, err := New(logger, &test.cfg)

			if test.expectConstructorError {
				require.Error(t, err, "expected failure when setting up citi_blackbox_exporter")
				return
			}
			require.NoError(t, err, "failed to setup citi_blackbox_exporter")

			r := mux.NewRouter()
			handler, err := integration.MetricsHandler()
			require.NoError(t, err)
			r.Handle("/metrics", handler)
			require.NoError(t, err)

			srv := httptest.NewServer(r)
			defer srv.Close()

			res, err := http.Get(srv.URL + "/metrics")
			require.NoError(t, err)

			body, err := ioutil.ReadAll(res.Body)
			require.NoError(t, err)

			foundMetricNames := map[string]bool{}
			for _, name := range test.expectedMetrics {
				foundMetricNames[name] = false
			}

			p := textparse.NewPromParser(body)
			for {
				entry, err := p.Next()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)

				if entry == textparse.EntryHelp {
					matchMetricNames(foundMetricNames, p)
				}
			}

			for metric, exists := range foundMetricNames {
				require.True(t, exists, "could not find metric %s", metric)
			}
		})

	}
}

func matchMetricNames(names map[string]bool, p textparse.Parser) {
	for name := range names {
		metricName, _ := p.Help()
		if bytes.Equal([]byte(name), metricName) {
			names[name] = true
		}
	}
}
