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
