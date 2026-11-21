package config

import (
	"fmt"
	"os"
	"strings"
	"time"
	// The zone database inside the binary: APP_TIMEZONE works even on a host
	// or image without tzdata.
	_ "time/tzdata"
)

// DefaultTimezone is the zone for alert times, error times and the daily summary
// when APP_TIMEZONE is not set (ADR-0014).
const DefaultTimezone = "Europe/Lisbon"

// DefaultLocation is DefaultTimezone; the embedded zone database always has it.
func DefaultLocation() *time.Location {
	loc, err := time.LoadLocation(DefaultTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// LoadTimezone reads APP_TIMEZONE once at start-up; an unknown zone stops it.
func LoadTimezone() (*time.Location, error) {
	name := strings.TrimSpace(os.Getenv("APP_TIMEZONE"))
	if name == "" {
		name = DefaultTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("APP_TIMEZONE %q is not a known time zone: %w", name, err)
	}
	return loc, nil
}
