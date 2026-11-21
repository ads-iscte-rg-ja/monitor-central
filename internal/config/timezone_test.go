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
