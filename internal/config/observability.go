package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// ObservabilityConfig controls metrics and logs (ADR-0004).
type ObservabilityConfig struct {
	// MetricsAddr is the separate, non-public listener for /metrics; empty disables it.
	MetricsAddr string
	LogFormat   string
	LogLevel    string
}

func LoadObservabilityConfig() (*ObservabilityConfig, error) {
	metricsAddr, set := os.LookupEnv("METRICS_ADDR")
	if !set {
		metricsAddr = "127.0.0.1:9464"
	}
	cfg := &ObservabilityConfig{
		MetricsAddr: strings.TrimSpace(metricsAddr),
		LogFormat:   GetEnv("LOG_FORMAT", "json"),
		LogLevel:    GetEnv("LOG_LEVEL", "info"),
	}
	if cfg.MetricsAddr != "" {
		if _, port, err := net.SplitHostPort(cfg.MetricsAddr); err != nil || port == "" {
			return nil, fmt.Errorf("METRICS_ADDR must be host:port (e.g. 127.0.0.1:9464) or empty to disable metrics")
		}
	}
	return cfg, nil
}
