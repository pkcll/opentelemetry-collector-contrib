// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package prometheusreceiver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/pkcll/opentelemetry-collector-contrib/receiver/prometheusreceiver/internal/metadata"
)

func TestCreateDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig()
	assert.NotNil(t, cfg, "failed to create default config")
	assert.NoError(t, componenttest.CheckConfigStruct(cfg))
	assert.NotNil(t, cfg.(*Config).Registerer)
	assert.NotNil(t, cfg.(*Config).Gatherer)
	assert.Equal(t, 1*time.Minute, cfg.(*Config).GathererInterval)

	// apply option
	opt := func(c component.Config, o FactoryOption) *Config {
		o(c.(*Config))
		return c.(*Config)
	}
	// Should not be able to set to zero values
	assert.NotNil(t, opt(cfg, WithRegisterer(nil)).Registerer, "should not be nil")
	assert.NotNil(t, opt(cfg, WithGatherer(nil)).Gatherer, "should not be nil")
	assert.Equal(t, 1*time.Minute, opt(cfg, WithGathererInterval(0)).GathererInterval, "should not be 0")
	// Should be able to set to non-zero values
	r := prometheus.NewRegistry()
	assert.Equal(t, r, opt(cfg, WithRegisterer(r)).Registerer.(*prometheus.Registry), "should not be nil")
	assert.Equal(t, r, opt(cfg, WithGatherer(r)).Gatherer.(*prometheus.Registry), "should not be nil")
	assert.Equal(t, 10*time.Second, opt(cfg, WithGathererInterval(10*time.Second)).GathererInterval, "should not be 0")
}

func TestCreateReceiver(t *testing.T) {
	cfg := createDefaultConfig()
	// The default config does not provide scrape_config so we expect that metrics receiver
	// creation must also fail.
	creationSet := receivertest.NewNopSettings()
	mReceiver, _ := createMetricsReceiver(context.Background(), creationSet, cfg, consumertest.NewNop())
	assert.NotNil(t, mReceiver)
	assert.NotNil(t, mReceiver.(*pReceiver).cfg.PrometheusConfig.GlobalConfig)
}

func TestFactoryCanParseServiceDiscoveryConfigs(t *testing.T) {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config_sd.yaml"))
	require.NoError(t, err)
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	sub, err := cm.Sub(component.NewIDWithName(metadata.Type, "").String())
	require.NoError(t, err)
	assert.NoError(t, sub.Unmarshal(cfg))
}

func TestMultipleCreate(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	set := receivertest.NewNopSettings()
	firstRcvr, err := factory.CreateMetrics(context.Background(), set, cfg, consumertest.NewNop())
	require.NoError(t, err)
	host := componenttest.NewNopHost()
	require.NoError(t, err)
	require.NoError(t, firstRcvr.Start(context.Background(), host))
	require.NoError(t, firstRcvr.Shutdown(context.Background()))
	secondRcvr, err := factory.CreateMetrics(context.Background(), set, cfg, consumertest.NewNop())
	require.NoError(t, err)
	require.NoError(t, secondRcvr.Start(context.Background(), host))
	require.NoError(t, secondRcvr.Shutdown(context.Background()))
}
