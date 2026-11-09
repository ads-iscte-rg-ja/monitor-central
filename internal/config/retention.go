package config

import (
	"fmt"
	"strconv"
	"time"
)

type RetentionConfig struct {
	StatsDays, ErrorDays, ResolvedEventDays, SentEmailDays, AuditDays int
	Interval                                                          time.Duration
	BatchSize                                                         int
}

func LoadRetentionConfig() (RetentionConfig, error) {
	c := RetentionConfig{}
	for _, item := range []struct {
		name string
		dest *int
	}{
		{"RETENTION_STATS_DAYS", &c.StatsDays}, {"RETENTION_ERRORS_DAYS", &c.ErrorDays},
		{"RETENTION_RESOLVED_EVENTS_DAYS", &c.ResolvedEventDays}, {"RETENTION_SENT_EMAIL_DAYS", &c.SentEmailDays},
		{"RETENTION_AUDIT_DAYS", &c.AuditDays},
	} {
		n, err := strconv.Atoi(GetEnv(item.name, "0"))
		if err != nil || n < 0 || n > 36500 {
			return c, fmt.Errorf("%s must be between 0 (disabled) and 36500", item.name)
		}
		*item.dest = n
	}
	var err error
	c.Interval, err = time.ParseDuration(GetEnv("CLEANUP_INTERVAL", "1h"))
	if err != nil || c.Interval < time.Minute || c.Interval > 24*time.Hour {
		return c, fmt.Errorf("CLEANUP_INTERVAL must be between 1m and 24h")
	}
	c.BatchSize, err = strconv.Atoi(GetEnv("CLEANUP_BATCH_SIZE", "1000"))
	if err != nil || c.BatchSize < 1 || c.BatchSize > 10000 {
		return c, fmt.Errorf("CLEANUP_BATCH_SIZE must be between 1 and 10000")
	}
	return c, nil
}
