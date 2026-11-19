package config

import (
	"strings"
	"testing"
)

func TestAdminCredentialsRequiredAndValidated(t *testing.T) {
	t.Setenv("ADMIN_EMAIL", "")
	t.Setenv("ADMIN_PASSWORD", "")
	if _, err := LoadAdminConfig(); err == nil {
		t.Fatal("missing credentials accepted")
	}
	t.Setenv("ADMIN_EMAIL", "Operator <admin@example.com>")
	t.Setenv("ADMIN_PASSWORD", "test-only-admin-password")
	if _, err := LoadAdminConfig(); err == nil {
		t.Fatal("display-name email accepted")
	}
	t.Setenv("ADMIN_EMAIL", " Admin@Example.com ")
	t.Setenv("ADMIN_NOTIFICATION_EMAIL", "")
	for _, password := range []string{"short", strings.Repeat("x", 73)} {
		t.Setenv("ADMIN_PASSWORD", password)
		if _, err := LoadAdminConfig(); err == nil {
			t.Fatal("invalid bcrypt password length accepted")
		}
	}
	t.Setenv("ADMIN_PASSWORD", "test-only-admin-password")
	admin, err := LoadAdminConfig()
	if err != nil || admin.Email != "admin@example.com" {
		t.Fatalf("unexpected credentials %v", err)
	}
}

func TestProductionCookieConfiguration(t *testing.T) {
	t.Setenv("PORT", "3333")
	t.Setenv("APP_ENV", "production")
	t.Setenv("COOKIE_SECURE", "false")
	if _, err := LoadServerConfig(); err == nil {
		t.Fatal("insecure production cookies accepted")
	}
	t.Setenv("COOKIE_SECURE", "invalid")
	if _, err := LoadServerConfig(); err == nil {
		t.Fatal("malformed cookie setting accepted")
	}
	t.Setenv("COOKIE_SECURE", "true")
	if cfg, err := LoadServerConfig(); err != nil || !cfg.CookieSecure {
		t.Fatalf("secure setting rejected: %v", err)
	}
}

func TestGRPCPortConfiguration(t *testing.T) {
	t.Setenv("PORT", "3333")
	t.Setenv("APP_ENV", "development")
	t.Setenv("COOKIE_SECURE", "false")
	for _, port := range []string{"0", "65536", "not-a-port", "3333"} {
		t.Setenv("GRPC_PORT", port)
		if _, err := LoadServerConfig(); err == nil {
			t.Fatalf("GRPC_PORT=%s accepted (must be a valid port different from PORT)", port)
		}
	}
	t.Setenv("GRPC_PORT", "3334")
	if cfg, err := LoadServerConfig(); err != nil || cfg.GRPCPort != 3334 {
		t.Fatalf("valid GRPC_PORT rejected: %v", err)
	}
}

func TestObservabilityConfiguration(t *testing.T) {
	cfg, err := LoadObservabilityConfig()
	if err != nil || cfg.MetricsAddr != "127.0.0.1:9464" || cfg.LogFormat != "json" || cfg.LogLevel != "info" {
		t.Fatalf("unexpected defaults %+v %v", cfg, err)
	}
	t.Setenv("METRICS_ADDR", "")
	if cfg, err := LoadObservabilityConfig(); err != nil || cfg.MetricsAddr != "" {
		t.Fatalf("an empty METRICS_ADDR must disable metrics: %+v %v", cfg, err)
	}
	t.Setenv("METRICS_ADDR", "9464")
	if _, err := LoadObservabilityConfig(); err == nil {
		t.Fatal("METRICS_ADDR without host:port accepted")
	}
}

func TestIngestAdmissionLimit(t *testing.T) {
	if cfg, err := LoadIngestConfig(25); err != nil || cfg.MaxInflight != 15 {
		t.Fatalf("default must be 60%% of the pool (15 of 25): %+v %v", cfg, err)
	}
	if cfg, err := LoadIngestConfig(2); err != nil || cfg.MaxInflight != 1 {
		t.Fatalf("small pools keep at least one ingestion slot: %+v %v", cfg, err)
	}
	if _, err := LoadIngestConfig(1); err == nil {
		t.Fatal("a pool of 1 leaves nothing for the dashboard")
	}
	for _, v := range []string{"0", "25", "26", "x"} {
		t.Setenv("INGEST_MAX_INFLIGHT", v)
		if _, err := LoadIngestConfig(25); err == nil {
			t.Fatalf("INGEST_MAX_INFLIGHT=%s accepted with a pool of 25", v)
		}
	}
	t.Setenv("INGEST_MAX_INFLIGHT", "20")
	if cfg, err := LoadIngestConfig(25); err != nil || cfg.MaxInflight != 20 {
		t.Fatalf("explicit limit rejected: %+v %v", cfg, err)
	}
}
