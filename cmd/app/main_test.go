package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupBuildsTheConfiguredLogger(t *testing.T) {
	defer slog.SetDefault(slog.Default())
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("METRICS_ADDR", "127.0.0.1:9464")
	var out bytes.Buffer
	_, logger, obs, err := setup(&out, filepath.Join(t.TempDir(), "missing.env"))
	if err != nil {
		t.Fatal(err)
	}
	logger.Println("hello")
	if obs.MetricsAddr != "127.0.0.1:9464" || !strings.Contains(out.String(), `"msg":"hello"`) {
		t.Fatalf("unexpected setup: %+v %q", obs, out.String())
	}
}

func TestSetupReportsEachConfigurationError(t *testing.T) {
	defer slog.SetDefault(slog.Default())
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "env-is-a-dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, envFile, metrics, format, want string
	}{
		{"unreadable env file", filepath.Join(dir, "env-is-a-dir"), "127.0.0.1:9464", "json", "load environment"},
		{"bad metrics address", filepath.Join(dir, "missing.env"), "9464", "json", "observability configuration"},
		{"bad log format", filepath.Join(dir, "missing.env"), "127.0.0.1:9464", "xml", "logging configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("METRICS_ADDR", tc.metrics)
			t.Setenv("LOG_FORMAT", tc.format)
			t.Setenv("LOG_LEVEL", "info")
			if _, _, _, err := setup(io.Discard, tc.envFile); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error %q, got %v", tc.want, err)
			}
		})
	}
}
