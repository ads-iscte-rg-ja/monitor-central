package alertmetric

import (
	"fmt"

	"monitor-central/internal/config"
)

// hostOfflineQuery is the minutes since the host was last seen: its last
// telemetry, or, for a host with a health endpoint, the start of the current
// unavailability. Only hosts with recent evidence are evaluated.
const hostOfflineQuery = rulesSelect + `EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - CASE WHEN h.health_endpoint = '' THEN h.last_seen_at
  ELSE COALESCE(h.unavailable_since, CURRENT_TIMESTAMP) END)) / 60.0` + rulesFrom + enabledRulesOf + `'host_offline'
 AND ((h.health_endpoint = '' AND h.last_seen_at IS NOT NULL)
  OR (h.health_endpoint <> '' AND h.health_check_ok IS NOT NULL
   AND h.health_checked_at >= CURRENT_TIMESTAMP - ($1 * INTERVAL '1 second')))`

func init() { register(hostOffline{}) }

// hostOffline is how long a host has been out of contact, in minutes.
type hostOffline struct{}

func (hostOffline) Name() string                { return "host_offline" }
func (hostOffline) Help() string                { return "host_offline: minutes" }
func (hostOffline) ValidThreshold(float64) bool { return true }
func (hostOffline) Format(value float64) string { return fmt.Sprintf("%.1f min offline", value) }
func (hostOffline) Breached(condition string, value, threshold float64) bool {
	return compare(condition, value, threshold)
}
func (h hostOffline) FormatThreshold(condition string, threshold float64) string {
	return operator(condition) + " " + h.Format(threshold)
}
func (hostOffline) Query() (string, []any) {
	return hostOfflineQuery, []any{config.HostHealthFreshness.Seconds()}
}
