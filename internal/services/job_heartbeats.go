package services

import (
	"sort"
	"sync"
	"time"
)

// JobHeartbeats watches the background jobs that this instance leads
// (ADR-0010, Heartbeat/Monitor). Each completed iteration is a beat; a led job
// whose last beat is older than twice its interval is stale (blocked). A job
// that fails still beats: failures have their own metric (ADR-0004).
type JobHeartbeats struct {
	mu   sync.Mutex
	now  func() time.Time
	jobs map[string]*jobBeat
}

type jobBeat struct {
	interval time.Duration
	last     time.Time
	leading  bool
}

// NewJobHeartbeats takes the clock, so tests need no sleeps.
func NewJobHeartbeats(now func() time.Time) *JobHeartbeats {
	return &JobHeartbeats{now: now, jobs: map[string]*jobBeat{}}
}

// Register declares a job and its interval when its loop starts.
func (h *JobHeartbeats) Register(job string, interval time.Duration) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jobs[job] = &jobBeat{interval: interval, last: h.now()}
}

// Starting records the leadership decision before an iteration. A job that
// becomes led starts its clock now, so an iteration that never ends is seen.
func (h *JobHeartbeats) Starting(job string, leading bool) {
	h.update(job, func(beat *jobBeat) {
		if leading && !beat.leading {
			beat.last = h.now()
		}
		beat.leading = leading
	})
}

// Finished is the beat: one iteration ended, with or without an error.
func (h *JobHeartbeats) Finished(job string) {
	h.update(job, func(beat *jobBeat) { beat.last = h.now() })
}

func (h *JobHeartbeats) update(job string, change func(*jobBeat)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if beat, ok := h.jobs[job]; ok {
		change(beat)
	}
}

// Stale lists, ordered, the led jobs without a beat for more than twice their interval.
func (h *JobHeartbeats) Stale() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	var stale []string
	for job, beat := range h.jobs {
		if beat.leading && now.Sub(beat.last) > 2*beat.interval {
			stale = append(stale, job)
		}
	}
	sort.Strings(stale)
	return stale
}
