// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package prometheusreceiver // import "github.com/pkcll/opentelemetry-collector-contrib/receiver/prometheusreceiver"

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/pkcll/prometheus/scrape"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"

	"github.com/pkcll/opentelemetry-collector-contrib/receiver/prometheusreceiver/internal"
)

const (
	defaultGCInterval = 2 * time.Minute
	gcIntervalDelta   = 1 * time.Minute
)

// pReceiver is the type that provides Prometheus scraper/receiver functionality.
type pReceiver struct {
	cfg            *Config
	consumer       consumer.Metrics
	cancelFunc     context.CancelFunc
	configLoaded   chan struct{}
	loadConfigOnce sync.Once

	settings          receiver.Settings
	registerer        prometheus.Registerer
	gatherer          prometheus.Gatherer
	unregisterMetrics func()
	skipOffsetting    bool // for testing only
}

type PromReceiver struct {
	*pReceiver
}

func NewPrometheusReceiver(set receiver.Settings, cfg *Config, next consumer.Metrics) *PromReceiver {
	return &PromReceiver{newPrometheusReceiver(set, cfg, next)}
}

// New creates a new prometheus.Receiver reference.
func newPrometheusReceiver(set receiver.Settings, cfg *Config, next consumer.Metrics) *pReceiver {
	var (
		r prometheus.Registerer = prometheus.DefaultRegisterer
		g prometheus.Gatherer   = prometheus.DefaultGatherer
	)
	if cfg.Registerer != nil {
		r = cfg.Registerer
	}
	if cfg.Gatherer != nil {
		g = cfg.Gatherer
	}
	pr := &pReceiver{
		cfg:          cfg,
		consumer:     next,
		settings:     set,
		configLoaded: make(chan struct{}),
		registerer: prometheus.WrapRegistererWith(
			prometheus.Labels{"receiver": set.ID.String()},
			r),
		gatherer: g,
	}
	return pr
}

// Start is the method that starts Prometheus scraping. It
// is controlled by having previously defined a Configuration using perhaps New.
func (r *pReceiver) Start(_ context.Context, host component.Host) error {
	discoveryCtx, cancel := context.WithCancel(context.Background())
	r.cancelFunc = cancel

	logger := internal.NewZapToGokitLogAdapter(r.settings.Logger)

	err := r.initPrometheusComponents(discoveryCtx, logger, host)
	if err != nil {
		r.settings.Logger.Error("Failed to initPrometheusComponents Prometheus components", zap.Error(err))
		return err
	}

	r.loadConfigOnce.Do(func() {
		close(r.configLoaded)
	})

	return nil
}

func (r *pReceiver) initPrometheusComponents(
	ctx context.Context,
	logger log.Logger,
	_ component.Host,
) error {
	var startTimeMetricRegex *regexp.Regexp
	var err error
	if r.cfg.StartTimeMetricRegex != "" {
		startTimeMetricRegex, err = regexp.Compile(r.cfg.StartTimeMetricRegex)
		if err != nil {
			return err
		}
	}

	store, err := internal.NewAppendable(
		r.consumer,
		r.settings,
		gcInterval(r.cfg.PrometheusConfig),
		r.cfg.UseStartTimeMetric,
		startTimeMetricRegex,
		useCreatedMetricGate.IsEnabled(),
		enableNativeHistogramsGate.IsEnabled(),
		r.cfg.PrometheusConfig.GlobalConfig.ExternalLabels,
		r.cfg.TrimMetricSuffixes,
	)
	if err != nil {
		return err
	}

	loop, err := scrape.NewGathererLoop(ctx, logger, store, r.registerer, r.gatherer, 10*time.Millisecond)
	if err != nil {
		return err
	}
	r.unregisterMetrics = func() {
		loop.UnregisterMetrics()
	}
	go func() {
		// The scrape manager needs to wait for the configuration to be loaded before beginning
		<-r.configLoaded
		r.settings.Logger.Info("Starting gatherer loop")
		loop.Run(nil)
	}()
	return nil
}

// gcInterval returns the longest scrape interval used by a scrape config,
// plus a delta to prevent race conditions.
// This ensures jobs are not garbage collected between scrapes.
func gcInterval(cfg *PromConfig) time.Duration {
	gcInterval := defaultGCInterval
	if time.Duration(cfg.GlobalConfig.ScrapeInterval)+gcIntervalDelta > gcInterval {
		gcInterval = time.Duration(cfg.GlobalConfig.ScrapeInterval) + gcIntervalDelta
	}
	for _, scrapeConfig := range cfg.ScrapeConfigs {
		if time.Duration(scrapeConfig.ScrapeInterval)+gcIntervalDelta > gcInterval {
			gcInterval = time.Duration(scrapeConfig.ScrapeInterval) + gcIntervalDelta
		}
	}
	return gcInterval
}

// Shutdown stops and cancels the underlying Prometheus scrapers.
func (r *pReceiver) Shutdown(context.Context) error {
	if r.cancelFunc != nil {
		r.cancelFunc()
	}
	if r.unregisterMetrics != nil {
		r.unregisterMetrics()
	}
	return nil
}
