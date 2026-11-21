package alertmetric

import (
	"math"
	"strings"
	"testing"
)

func metric(t *testing.T, name string) Metric {
	t.Helper()
	m, ok := Lookup(name)
	if !ok {
		t.Fatalf("%s is not registered", name)
	}
	return m
}

func TestRegistryHoldsEveryMetricInOrder(t *testing.T) {
	want := "buffer_quarantined,cpu,disk,error_level,error_rate,host_offline,ram"
	if got := strings.Join(Names(), ","); got != want {
		t.Fatalf("registered metrics: got %s, want %s", got, want)
	}
	if _, ok := Lookup("swap"); ok {
		t.Fatal("an unknown metric must not be found")
	}
	for _, m := range All() {
		if !strings.HasPrefix(m.Help(), m.Name()+":") || !strings.Contains(Help(), m.Help()) {
			t.Errorf("%s: help must name the metric and appear on the form", m.Name())
		}
		query, _ := m.Query()
		if !strings.Contains(query, "a.metric = '"+m.Name()+"'") {
			t.Errorf("%s: the query must select only this metric's rules", m.Name())
		}
	}
}

func TestRegisteringANameTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate name must be refused")
		}
	}()
	register(errorRate{})
}

func TestThresholdLimitsPerMetric(t *testing.T) {
	for _, tc := range []struct {
		metric    string
		threshold float64
		valid     bool
	}{
		{"cpu", 100, true}, {"ram", 100.5, false}, {"disk", math.NaN(), false},
		{"error_level", 3, true}, {"error_level", 3.5, false}, {"error_level", 6, false},
		{"error_rate", 1000, true}, {"host_offline", 90, true},
		{"buffer_quarantined", 1, true}, {"buffer_quarantined", 1.5, false},
	} {
		if got := metric(t, tc.metric).ValidThreshold(tc.threshold); got != tc.valid {
			t.Errorf("%s threshold %v: valid=%v, want %v", tc.metric, tc.threshold, got, tc.valid)
		}
	}
}

func TestBreachAndFormatPerMetric(t *testing.T) {
	for _, tc := range []struct {
		metric, condition     string
		value, threshold      float64
		breached              bool
		shown, shownThreshold string
	}{
		{"cpu", "gt", 95, 80, true, "95.0%", "> 80.0%"},
		{"ram", "lt", 50, 10, false, "50.0%", "< 10.0%"},
		{"disk", "eq", 95, 80, false, "95.0%", "> 80.0%"},
		{"error_rate", "gt", 3, 2, true, "3/min", "> 2/min"},
		// error_level matches at or above, whatever the condition.
		{"error_level", "lt", 4, 3, true, "CRITICAL", "≥ ERROR"},
		{"error_level", "gt", 2, 3, false, "WARN", "≥ ERROR"},
		{"error_level", "gt", 0, 3, false, "UNKNOWN", "≥ ERROR"},
		{"host_offline", "gt", 12.5, 10, true, "12.5 min offline", "> 10.0 min offline"},
	} {
		m := metric(t, tc.metric)
		if got := m.Breached(tc.condition, tc.value, tc.threshold); got != tc.breached {
			t.Errorf("%s %s %v vs %v: breached=%v, want %v", tc.metric, tc.condition, tc.value, tc.threshold, got, tc.breached)
		}
		if got := m.Format(tc.value); got != tc.shown {
			t.Errorf("%s: Format(%v)=%q, want %q", tc.metric, tc.value, got, tc.shown)
		}
		if got := m.FormatThreshold(tc.condition, tc.threshold); got != tc.shownThreshold {
			t.Errorf("%s: FormatThreshold=%q, want %q", tc.metric, got, tc.shownThreshold)
		}
	}
	if _, args := metric(t, "host_offline").Query(); len(args) != 1 {
		t.Error("host_offline takes the health freshness as its only argument")
	}
}

func TestBufferQuarantinedMatchesAtOrAboveTheThreshold(t *testing.T) {
	m := metric(t, "buffer_quarantined")
	if !m.Breached("gt", 1, 1) || !m.Breached("lt", 5, 1) || m.Breached("gt", 0, 1) {
		t.Fatal("buffer_quarantined must match at or above the threshold, whatever the condition")
	}
	if m.Format(3) != "3 quarantined" || m.FormatThreshold("gt", 1) != "≥ 1 quarantined" {
		t.Fatalf("format: %q / %q", m.Format(3), m.FormatThreshold("gt", 1))
	}
	if query, args := m.Query(); !strings.Contains(query, "host_buffer_status") || args != nil {
		t.Fatal("buffer_quarantined must read the agents' last buffer reports")
	}
}
