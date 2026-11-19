package alertmetric

// Blocks shared by the metric queries. They are constants: each metric's query
// is joined at compile time, and no SQL is assembled at run time.
//
// Every query returns the columns of models.AlertMetricEvaluation, for the
// enabled rules of one metric, with the value between rulesSelect and rulesFrom.
const (
	rulesSelect = `SELECT a.id AS alert_id, a.name AS alert_name, a.host_id, h.name AS host_name, a.metric,
 a.condition, a.threshold::DOUBLE PRECISION AS threshold, a.severity,
 array_to_string(a.notification_channels, ',') AS channels_csv, (`
	rulesFrom = `)::DOUBLE PRECISION AS current_value
 FROM alerts a JOIN hosts h ON h.id = a.host_id`
	// latestStats joins each host's most recent statistics row as "latest".
	latestStats = `
 LEFT JOIN LATERAL (SELECT cpu_percent, ram_used_mb, ram_total_mb, disk_used_gb, disk_total_gb, collected_at
  FROM machine_stats WHERE host_id = h.id ORDER BY collected_at DESC LIMIT 1) AS latest ON TRUE`
	enabledRulesOf = `
 WHERE a.enabled = TRUE AND a.metric = `
	// freshStats ignores statistics older than 5 minutes: a silent host must
	// not keep firing on its last value (host_offline covers silence).
	freshStats = `
 AND latest.collected_at >= CURRENT_TIMESTAMP - INTERVAL '5 minutes' AND latest.collected_at <= CURRENT_TIMESTAMP`
)
