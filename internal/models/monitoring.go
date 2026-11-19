package models

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrHostNotFound          = errors.New("host not found")
	ErrInvalidAPIKey         = errors.New("invalid API key")
	ErrFutureTimestamp       = errors.New("timestamp cannot be in the future")
	ErrBatchTooLarge         = errors.New("error batch exceeds 100 records")
	ErrStatsBatchTooLarge    = errors.New("statistics batch exceeds 100 records")
	ErrEmptyStatsBatch       = errors.New("statistics batch cannot be empty")
	ErrMissingStatID         = errors.New("statistics source ID must be positive")
	ErrDuplicateSourceID     = errors.New("duplicate source ID in batch")
	ErrFieldTooLarge         = errors.New("monitoring field exceeds its size limit")
	ErrInvalidCPUPercent     = errors.New("CPU percent must be between 0 and 100")
	ErrInvalidRAMUsage       = errors.New("invalid RAM usage")
	ErrInvalidDiskUsage      = errors.New("invalid disk usage")
	ErrEmptyErrorBatch       = errors.New("error batch cannot be empty")
	ErrMissingErrorID        = errors.New("error ID is required")
	ErrMissingAppName        = errors.New("app name is required")
	ErrMissingEnvironment    = errors.New("environment is required")
	ErrInvalidErrorLevel     = errors.New("invalid error level")
	ErrMissingErrorMessage   = errors.New("error message is required")
	ErrInvalidMetadata       = errors.New("error metadata must be valid JSON")
	ErrInvalidMetricsRange   = errors.New("invalid metrics range")
	ErrHostNameRequired      = errors.New("host name is required")
	ErrHostnameRequired      = errors.New("hostname is required")
	ErrHealthEndpoint        = errors.New("health endpoint must be an absolute HTTP(S) URL without credentials, query, or fragment, at most 2048 bytes")
	ErrContactTimeout        = errors.New("offline timeout must be between 1 and 1440 minutes")
	ErrHostIDRequired        = errors.New("host ID is required")
	ErrAlertNameRequired     = errors.New("alert name is required")
	ErrAlertHostRequired     = errors.New("alert host is required")
	ErrInvalidAlertMetric    = errors.New("invalid alert metric")
	ErrInvalidAlertCondition = errors.New("invalid alert condition")
	ErrInvalidAlertThreshold = errors.New("invalid alert threshold")
	ErrInvalidAlertSeverity  = errors.New("invalid alert severity")
	ErrAlertChannelRequired  = errors.New("at least one notification channel is required")
	ErrAlertIDRequired       = errors.New("alert ID is required")
	ErrAlertNotFound         = errors.New("alert not found")
	ErrAlertEventIDRequired  = errors.New(
		"alert event ID is required",
	)
	ErrAlertEventNotFound = errors.New(
		"alert event not found",
	)
)

type Host struct {
	ID                    int64      `json:"id" db:"id"`
	Name                  string     `json:"name" db:"name"`
	Hostname              string     `json:"hostname" db:"hostname"`
	APIKey                string     `json:"-" db:"-"`
	Status                string     `json:"status" db:"status"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty" db:"last_seen_at"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
	HealthEndpoint        string     `json:"health_endpoint" db:"health_endpoint"`
	ContactTimeoutSeconds int        `json:"contact_timeout_seconds" db:"contact_timeout_seconds"`
}

type HostHealthTarget struct {
	ID             int64  `db:"id"`
	HealthEndpoint string `db:"health_endpoint"`
}

type MachineStat struct {
	ID          int64     `json:"id" db:"id"`
	HostID      int64     `json:"host_id" db:"host_id"`
	SourceID    *int64    `json:"source_id,omitempty" db:"source_id"`
	CPUPercent  float64   `json:"cpu_percent" db:"cpu_percent"`
	RAMUsedMB   float64   `json:"ram_used_mb" db:"ram_used_mb"`
	RAMTotalMB  float64   `json:"ram_total_mb" db:"ram_total_mb"`
	DiskUsedGB  float64   `json:"disk_used_gb" db:"disk_used_gb"`
	DiskTotalGB float64   `json:"disk_total_gb" db:"disk_total_gb"`
	CollectedAt time.Time `json:"collected_at" db:"collected_at"`
}

type MachineStatsRequest struct {
	ID          int64     `json:"id,omitempty"`
	CPUPercent  float64   `json:"cpu_percent"`
	RAMUsedMB   float64   `json:"ram_used_mb"`
	RAMTotalMB  float64   `json:"ram_total_mb"`
	DiskUsedGB  float64   `json:"disk_used_gb"`
	DiskTotalGB float64   `json:"disk_total_gb"`
	CollectedAt time.Time `json:"collected_at"`
}

type MachineStatsBatchRequest struct {
	Stats []MachineStatsRequest `json:"stats"`
}

// StatAcknowledgement is deliberately small so a complete batch acknowledgement
// stays bounded without echoing the submitted telemetry.
type StatAcknowledgement struct {
	ID       int64 `json:"id"`
	HostID   int64 `json:"host_id"`
	SourceID int64 `json:"source_id"`
}

// RecordValidationError identifies the rejected member of an atomic batch.
// None of the batch is persisted when validation fails.
type RecordValidationError struct {
	SourceID int64
	Err      error
}

func (e *RecordValidationError) Error() string { return e.Err.Error() }
func (e *RecordValidationError) Unwrap() error { return e.Err }

// ErrorLog represents an error stored by the central monitor.
type ErrorLog struct {
	ID          int64           `json:"id" db:"id"`
	SourceID    *int64          `json:"source_id,omitempty" db:"source_id"`
	HostID      int64           `json:"host_id" db:"host_id"`
	AppName     string          `json:"app_name" db:"app_name"`
	Environment string          `json:"environment" db:"environment"`
	Level       string          `json:"level" db:"level"`
	Message     string          `json:"message" db:"message"`
	StackTrace  *string         `json:"stack_trace,omitempty" db:"stack_trace"`
	Metadata    json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}

// ErrorLogInput represents one error received from a remote server.
type ErrorLogInput struct {
	ID          int64           `json:"id"`
	AppName     string          `json:"app_name"`
	Environment string          `json:"environment"`
	Level       string          `json:"level"`
	Message     string          `json:"message"`
	StackTrace  *string         `json:"stack_trace,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// ErrorLogsRequest represents a batch of errors.
type ErrorLogsRequest struct {
	Errors []ErrorLogInput `json:"errors"`
}

// DashboardSummary contains the values displayed in the dashboard cards.
type DashboardSummary struct {
	HostsOnline         int     `json:"hosts_online"`
	HostsTotal          int     `json:"hosts_total"`
	ErrorsToday         int     `json:"errors_today"`
	CriticalErrorsToday int     `json:"critical_errors_today"`
	ActiveAlerts        int     `json:"active_alerts"`
	FiringAlerts        int     `json:"firing_alerts"`
	AverageCPU          float64 `json:"average_cpu"`
}

// DashboardHost represents a host shown in the dashboard selector.
type DashboardHost struct {
	ID       int64  `json:"id" db:"id"`
	Name     string `json:"name" db:"name"`
	Hostname string `json:"hostname" db:"hostname"`
	Status   string `json:"status" db:"status"`
}

// MetricPoint represents one aggregated point in the charts.
type MetricPoint struct {
	CPUPercent  float64   `json:"cpu_percent" db:"cpu_percent"`
	RAMUsedMB   float64   `json:"ram_used_mb" db:"ram_used_mb"`
	RAMTotalMB  float64   `json:"ram_total_mb" db:"ram_total_mb"`
	DiskUsedGB  float64   `json:"disk_used_gb" db:"disk_used_gb"`
	DiskTotalGB float64   `json:"disk_total_gb" db:"disk_total_gb"`
	CollectedAt time.Time `json:"collected_at" db:"collected_at"`
}

// DashboardMetrics contains the host selector and chart data.
type DashboardMetrics struct {
	Latest       *MetricPoint    `json:"latest,omitempty"`
	Hosts        []DashboardHost `json:"hosts"`
	SelectedHost *DashboardHost  `json:"selected_host,omitempty"`
	Range        string          `json:"range"`
	Stats        []MetricPoint   `json:"stats"`
}

// ErrorLogFilters contains the filters available on the error logs page.
type ErrorLogFilters struct {
	HostID   int64
	Level    string
	AppName  string
	Search   string
	Page     int
	PageSize int
}

// ErrorLogListItem represents one error displayed in the error logs table.
type ErrorLogListItem struct {
	ID               int64           `json:"id" db:"id"`
	HostID           int64           `json:"host_id" db:"host_id"`
	HostName         string          `json:"host_name" db:"host_name"`
	AppName          string          `json:"app_name" db:"app_name"`
	Environment      string          `json:"environment" db:"environment"`
	Level            string          `json:"level" db:"level"`
	Message          string          `json:"message" db:"message"`
	StackTrace       *string         `json:"stack_trace,omitempty" db:"stack_trace"`
	Metadata         json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	MetadataText     string          `json:"-"`
	CreatedAtDisplay string          `json:"-"`
}

// ErrorLogsPage contains all data needed to render the error logs page.
type ErrorLogsPage struct {
	Errors      []ErrorLogListItem
	Hosts       []DashboardHost
	AppNames    []string
	TotalErrors int
	Page        int
	PageSize    int
	TotalPages  int
	Filters     ErrorLogFilters
}

// HostListItem contains the data displayed on one host card.
type HostListItem struct {
	ID                    int64      `json:"id" db:"id"`
	Name                  string     `json:"name" db:"name"`
	Hostname              string     `json:"hostname" db:"hostname"`
	APIKey                string     `json:"-" db:"-"`
	Status                string     `json:"status" db:"status"`
	LastSeenAt            *time.Time `json:"last_seen_at,omitempty" db:"last_seen_at"`
	CreatedAt             time.Time  `json:"created_at" db:"created_at"`
	HealthEndpoint        string     `json:"health_endpoint" db:"health_endpoint"`
	ContactTimeoutSeconds int        `json:"contact_timeout_seconds" db:"contact_timeout_seconds"`
	HealthCheckedAt       *time.Time `json:"health_checked_at,omitempty" db:"health_checked_at"`
	HealthCheckOK         *bool      `json:"health_check_ok,omitempty" db:"health_check_ok"`
	ContactTimeoutMinutes int        `json:"-" db:"-"`
	HealthChecked         string     `json:"-" db:"-"`

	CPUPercent  *float64 `json:"cpu_percent,omitempty" db:"cpu_percent"`
	RAMPercent  *float64 `json:"ram_percent,omitempty" db:"ram_percent"`
	DiskPercent *float64 `json:"disk_percent,omitempty" db:"disk_percent"`

	APIKeyMasked     string `json:"api_key_masked" db:"api_key_masked"`
	LastSeen         string `json:"last_seen" db:"-"`
	CreatedAtDisplay string `json:"created_at_display" db:"-"`
	HasStats         bool   `json:"has_stats" db:"-"`

	CPUDisplay  string `json:"cpu_display" db:"-"`
	RAMDisplay  string `json:"ram_display" db:"-"`
	DiskDisplay string `json:"disk_display" db:"-"`
}

// HostsPage contains the information needed to render the hosts page.
type HostsPage struct {
	Hosts []HostListItem
}

// CreateHostRequest contains the fields required to register a host.
type CreateHostRequest struct {
	Name                  string
	Hostname              string
	HealthEndpoint        string
	ContactTimeoutMinutes int
}

// UpdateHostRequest contains editable host fields.
type UpdateHostRequest struct {
	Name                  string
	Hostname              string
	HealthEndpoint        string
	ContactTimeoutMinutes int
}

type AlertRuleListItem struct {
	ID               int64    `json:"id" db:"id"`
	Name             string   `json:"name" db:"name"`
	HostID           *int64   `json:"host_id" db:"host_id"`
	HostName         string   `json:"host_name" db:"host_name"`
	Metric           string   `json:"metric" db:"metric"`
	Condition        string   `json:"condition" db:"condition"`
	Threshold        float64  `json:"threshold" db:"threshold"`
	ThresholdDisplay string   `json:"threshold_display" db:"-"`
	Severity         string   `json:"severity" db:"severity"`
	ChannelsCSV      string   `json:"-" db:"channels_csv"`
	Channels         []string `json:"channels" db:"-"`
	Enabled          bool     `json:"enabled" db:"enabled"`
}

type AlertEventListItem struct {
	ID                    int64     `json:"id" db:"id"`
	AlertName             string    `json:"alert_name" db:"alert_name"`
	HostID                int64     `json:"host_id" db:"host_id"`
	HostName              string    `json:"host_name" db:"host_name"`
	TriggeredValue        float64   `json:"triggered_value" db:"triggered_value"`
	Status                string    `json:"status" db:"status"`
	FiredAt               time.Time `json:"fired_at" db:"fired_at"`
	FiredAtDisplay        string    `json:"fired_at_display" db:"-"`
	Metric                string    `json:"metric" db:"metric"`
	TriggeredValueDisplay string    `json:"triggered_value_display" db:"-"`
}

type AlertHostOption struct {
	ID       int64  `json:"id" db:"id"`
	Name     string `json:"name" db:"name"`
	Hostname string `json:"hostname" db:"hostname"`
}

type AlertsPage struct {
	Alerts      []AlertRuleListItem
	AlertEvents []AlertEventListItem
	Hosts       []AlertHostOption
}

type CreateAlertRequest struct {
	Name      string
	HostID    int64
	Metric    string
	Condition string
	Threshold float64
	Severity  string
	Channels  []string
	Enabled   bool
}

type AlertRule struct {
	ID        int64
	Name      string
	HostID    int64
	Metric    string
	Condition string
	Threshold float64
	Severity  string
	Channels  []string
	Enabled   bool
}

type UpdateAlertRequest struct {
	Name      string
	HostID    int64
	Metric    string
	Condition string
	Threshold float64
	Severity  string
	Channels  []string
	Enabled   bool
}

type AlertMetricEvaluation struct {
	AlertID      int64   `db:"alert_id"`
	AlertName    string  `db:"alert_name"`
	HostID       int64   `db:"host_id"`
	HostName     string  `db:"host_name"`
	Metric       string  `db:"metric"`
	Condition    string  `db:"condition"`
	Threshold    float64 `db:"threshold"`
	CurrentValue float64 `db:"current_value"`
	Severity     string  `db:"severity"`
	ChannelsCSV  string  `db:"channels_csv"`
}

type AlertEvent struct {
	ID             int64
	AlertID        int64
	HostID         int64
	TriggeredValue float64
	Status         string
	FiredAt        time.Time
}

// EmailNotification is persisted in the same transaction as its alert event.
type EmailNotification struct {
	ID        int64
	EventID   *int64
	Recipient string
	Subject   string
	TextBody  string
	HTMLBody  string
	DedupeKey string
	Attempts  int
}

type DailyAlertSummaryItem struct {
	AlertName      string     `db:"alert_name"`
	HostName       string     `db:"host_name"`
	Metric         string     `db:"metric"`
	Severity       string     `db:"severity"`
	TriggeredValue float64    `db:"triggered_value"`
	Status         string     `db:"status"`
	FiredAt        time.Time  `db:"fired_at"`
	ResolvedAt     *time.Time `db:"resolved_at"`
}
