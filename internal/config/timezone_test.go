package config

import "testing"

func TestLoadTimezone(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "")
	if loc, err := LoadTimezone(); err != nil || loc.String() != DefaultTimezone {
		t.Fatalf("default: %v %v", loc, err)
	}
	t.Setenv("APP_TIMEZONE", "America/New_York")
	if loc, err := LoadTimezone(); err != nil || loc.String() != "America/New_York" {
		t.Fatalf("configured: %v %v", loc, err)
	}
	t.Setenv("APP_TIMEZONE", "Mars/Olympus")
	if _, err := LoadTimezone(); err == nil {
		t.Fatal("unknown zone accepted")
	}
}

func TestLoadWebSecurityConfig(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("CSP_MODE", "")
	if c, err := LoadWebSecurityConfig(); err != nil || c.CSPMode != CSPEnforce || !c.HSTS {
		t.Fatalf("default: %+v %v", c, err)
	}
	t.Setenv("COOKIE_SECURE", "false")
	t.Setenv("CSP_MODE", "Report-Only")
	if c, err := LoadWebSecurityConfig(); err != nil || c.CSPMode != CSPReportOnly || c.HSTS {
		t.Fatalf("report-only over HTTP: %+v %v", c, err)
	}
	t.Setenv("CSP_MODE", "off")
	if _, err := LoadWebSecurityConfig(); err == nil {
		t.Fatal("unknown CSP_MODE accepted")
	}
}
