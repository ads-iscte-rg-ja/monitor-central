package server

import (
	"testing"
	"time"
)

// TE3 (ADR-0014): when the daily summary runs, for any instant, without waiting.
func TestNextDailySummaryIsNineInTheConfiguredZone(t *testing.T) {
	lisbon, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatal(err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		now  time.Time
		loc  *time.Location
		want time.Time
	}{
		{"before nine", time.Date(2026, 11, 21, 8, 59, 0, 0, lisbon), lisbon, time.Date(2026, 11, 21, 9, 0, 0, 0, lisbon)},
		{"at nine it is the next day", time.Date(2026, 11, 21, 9, 0, 0, 0, lisbon), lisbon, time.Date(2026, 11, 22, 9, 0, 0, 0, lisbon)},
		{"after nine", time.Date(2026, 11, 21, 23, 30, 0, 0, lisbon), lisbon, time.Date(2026, 11, 22, 9, 0, 0, 0, lisbon)},
		{"end of month", time.Date(2026, 11, 30, 12, 0, 0, 0, lisbon), lisbon, time.Date(2026, 12, 1, 9, 0, 0, 0, lisbon)},
		// The night clocks go forward (29 Mar 2026): still 09:00 local, 23 h later.
		{"DST starts", time.Date(2026, 3, 28, 10, 0, 0, 0, lisbon), lisbon, time.Date(2026, 3, 29, 9, 0, 0, 0, lisbon)},
		// 00:30 UTC on the 21st is already 09:30 in Tokyo: the next is the 22nd, 09:00 there.
		{"other zone", time.Date(2026, 11, 21, 0, 30, 0, 0, time.UTC), tokyo, time.Date(2026, 11, 22, 9, 0, 0, 0, tokyo)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextDailySummary(tc.now, tc.loc); !got.Equal(tc.want) || got.Location() != tc.loc {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	if got := nextDailySummary(time.Date(2026, 11, 21, 8, 0, 0, 0, lisbon), nil); got.Hour() != 9 {
		t.Fatalf("default zone: %s", got)
	}
}
