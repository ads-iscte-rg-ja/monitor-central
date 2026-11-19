// Package alertmetric holds the alert metrics as strategies (ADR-0008). Each
// metric lives in its own file and registers itself; the evaluator, the
// service and the alert form only use the Metric interface and the registry,
// so a new metric is a new file plus the migration that allows its name.
package alertmetric

import (
	"fmt"
	"sort"
	"strings"
)

// Metric is what varies from one alert metric to another.
type Metric interface {
	// Name is the value stored in alerts.metric.
	Name() string
	// Help explains the threshold's unit on the alert form.
	Help() string
	// ValidThreshold applies the metric's own limits (the generic checks,
	// finite and positive, are the service's).
	ValidThreshold(threshold float64) bool
	// Breached reports whether the current value violates the rule.
	Breached(condition string, value, threshold float64) bool
	// Format shows a value in the metric's unit.
	Format(value float64) string
	// FormatThreshold shows the rule's condition, such as "> 80.0%".
	FormatThreshold(condition string, threshold float64) string
	// Query returns the enabled rules of this metric with their current
	// value, in one set-based statement (ADR-0005). The SQL is a constant.
	Query() (query string, args []any)
}

var registry = map[string]Metric{}

// register adds a metric; called from the init of the metric's file.
func register(m Metric) {
	if _, taken := registry[m.Name()]; taken {
		panic(fmt.Sprintf("alertmetric: %q registered twice", m.Name()))
	}
	registry[m.Name()] = m
}

// Lookup returns the metric with the given name (Simple Factory).
func Lookup(name string) (Metric, bool) {
	m, ok := registry[name]
	return m, ok
}

// All returns every metric, ordered by name.
func All() []Metric {
	metrics := make([]Metric, 0, len(registry))
	for _, m := range registry {
		metrics = append(metrics, m)
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].Name() < metrics[j].Name() })
	return metrics
}

// Names returns the name of every metric, ordered.
func Names() []string {
	all := All()
	names := make([]string, len(all))
	for i, m := range all {
		names[i] = m.Name()
	}
	return names
}

// Help joins every metric's help for the alert form.
func Help() string {
	all := All()
	parts := make([]string, len(all))
	for i, m := range all {
		parts[i] = m.Help()
	}
	return strings.Join(parts, ". ") + "."
}

// compare is the usual reading of a rule's condition.
func compare(condition string, value, threshold float64) bool {
	switch condition {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	default:
		return false
	}
}

func operator(condition string) string {
	if condition == "lt" {
		return "<"
	}
	return ">"
}
