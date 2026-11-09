package config

import "time"

const (
	HostHealthInterval    = 30 * time.Second
	HostHealthTimeout     = 5 * time.Second
	HostHealthFreshness   = 90 * time.Second
	HostHealthConcurrency = 8
)
