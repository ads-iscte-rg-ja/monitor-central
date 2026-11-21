package server

import (
	"strings"
	"testing"
)

func validSettingsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "3333")
	t.Setenv("GRPC_PORT", "3334")
	t.Setenv("APP_ENV", "development")
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("HOST_API_KEY_SECRET", strings.Repeat("ab", 32))
	t.Setenv("POSTGRES_HOST", "127.0.0.1")
	t.Setenv("POSTGRES_USER", "monitor")
	t.Setenv("POSTGRES_PASSWORD", "secret")
	t.Setenv("POSTGRES_DB", "monitor")
	t.Setenv("POSTGRES_MAX_OPEN_CONNS", "25")
}

func TestLoadSettingsDerivesTheIngestionLimitFromThePool(t *testing.T) {
	validSettingsEnv(t)
	s, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.ingest.MaxInflight != 15 || s.db.MaxOpenConns != 25 || s.server.GRPCPort != 3334 || s.hasher == nil {
		t.Fatalf("unexpected settings: ingest=%+v db.max=%d grpc=%d", s.ingest, s.db.MaxOpenConns, s.server.GRPCPort)
	}
}

func TestLoadSettingsStopsAtTheFirstInvalidSetting(t *testing.T) {
	for _, tc := range []struct{ name, key, value, want string }{
		{"server port", "PORT", "0", "PORT"},
		{"API key secret", "HOST_API_KEY_SECRET", "short", "HOST_API_KEY_SECRET"},
		{"login limits", "LOGIN_ATTEMPTS_PER_IP", "-1", ""},
		{"database", "POSTGRES_HOST", "", "database config"},
		{"ingestion limit", "INGEST_MAX_INFLIGHT", "25", "INGEST_MAX_INFLIGHT"},
		{"instance role", "APP_ROLE", "leader", "APP_ROLE"},
		{"health concurrency", "HEALTH_CONCURRENCY", "0", "HEALTH_CONCURRENCY"},
		{"time zone", "APP_TIMEZONE", "Mars/Olympus", "APP_TIMEZONE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validSettingsEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := loadSettings(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}
