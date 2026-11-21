package config

import (
	"testing"
	"time"
)

func TestLoadDashboardConfigDefaults(t *testing.T) {
	t.Setenv("DASHBOARD_MAX_INFLIGHT", "")
	t.Setenv("DASHBOARD_QUERY_TIMEOUT", "")
	// The pool of the load tests: 25 connections, 15 for the ingestion.
	c, err := LoadDashboardConfig(25, 15)
	if err != nil || c.MaxInflight != 5 || c.QueryTimeout != DefaultDashboardQueryTimeout {
		t.Fatalf("got %+v %v", c, err)
	}
	// The default never stops a small pool from starting: one slot.
	if c, err = LoadDashboardConfig(2, 1); err != nil || c.MaxInflight != 1 {
		t.Fatalf("small pool: %+v %v", c, err)
	}
}

func TestLoadDashboardConfigOverrides(t *testing.T) {
	t.Setenv("DASHBOARD_MAX_INFLIGHT", "8")
	t.Setenv("DASHBOARD_QUERY_TIMEOUT", "3s")
	c, err := LoadDashboardConfig(25, 15)
	if err != nil || c.MaxInflight != 8 || c.QueryTimeout != 3*time.Second {
		t.Fatalf("got %+v %v", c, err)
	}
}

func TestLoadDashboardConfigRejectsInvalid(t *testing.T) {
	for _, tc := range []struct{ inflight, timeout string }{
		{"0", ""}, {"many", ""}, {"10", ""}, // 10 + 15 leaves nothing of 25
		{"", "500ms"}, {"", "26s"}, {"", "soon"},
	} {
		t.Setenv("DASHBOARD_MAX_INFLIGHT", tc.inflight)
		t.Setenv("DASHBOARD_QUERY_TIMEOUT", tc.timeout)
		if _, err := LoadDashboardConfig(25, 15); err == nil {
			t.Fatalf("%+v accepted", tc)
		}
	}
}
