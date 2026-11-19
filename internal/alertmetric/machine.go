package alertmetric

import (
	"fmt"
	"math"
)

// machineMetric is a percentage computed from the host's latest statistics.
type machineMetric struct{ name, query string }

const (
	cpuQuery = rulesSelect + `latest.cpu_percent` + rulesFrom + latestStats + enabledRulesOf + `'cpu'` + freshStats
	ramQuery = rulesSelect + `latest.ram_used_mb / NULLIF(latest.ram_total_mb, 0) * 100` + rulesFrom + latestStats +
		enabledRulesOf + `'ram'` + freshStats
	diskQuery = rulesSelect + `latest.disk_used_gb / NULLIF(latest.disk_total_gb, 0) * 100` + rulesFrom + latestStats +
		enabledRulesOf + `'disk'` + freshStats
)

func init() {
	register(machineMetric{"cpu", cpuQuery})
	register(machineMetric{"ram", ramQuery})
	register(machineMetric{"disk", diskQuery})
}

func (m machineMetric) Name() string { return m.name }
func (m machineMetric) Help() string { return m.name + ": percent" }
func (machineMetric) ValidThreshold(threshold float64) bool {
	return !math.IsNaN(threshold) && threshold <= 100
}
func (machineMetric) Breached(condition string, value, threshold float64) bool {
	return compare(condition, value, threshold)
}
func (machineMetric) Format(value float64) string { return fmt.Sprintf("%.1f%%", value) }
func (m machineMetric) FormatThreshold(condition string, threshold float64) string {
	return operator(condition) + " " + m.Format(threshold)
}
func (m machineMetric) Query() (string, []any) { return m.query, nil }
