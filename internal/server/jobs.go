package server

import (
	"context"
	"fmt"
	"time"

	"monitor-central/internal/config"
)

// startBackgroundJobs launches all background cleanup jobs
func (s *Server) startBackgroundJobs(ctx context.Context) {
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.checkHostHealth(ctx) }()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.cleanupRetainedData(ctx)
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.evaluateMetricAlerts(ctx)
	}()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sendDailyAlertSummary(ctx)
	}()
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.deliverNotifications(ctx) }()
}

func (s *Server) checkHostHealth(ctx context.Context) {
	s.jobBeats.Register("host_health", config.HostHealthInterval)
	ticker := time.NewTicker(config.HostHealthInterval)
	defer ticker.Stop()
	for {
		if err := s.runJob(ctx, "host_health", s.hostHealthService.Check); err != nil && ctx.Err() == nil {
			s.logger.Printf("host health checks: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) deliverNotifications(ctx context.Context) {
	const interval = 5 * time.Second
	s.jobBeats.Register("notifications", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.runJob(ctx, "notifications", s.alertsService.DeliverNotifications); err != nil && ctx.Err() == nil {
			s.logger.Printf("notification delivery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) cleanupRetainedData(ctx context.Context) {
	s.jobBeats.Register("retention", s.retentionConfig.Interval)
	ticker := time.NewTicker(s.retentionConfig.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var counts map[string]int64
			err := s.runJob(ctx, "retention", func(ctx context.Context) error {
				cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				var err error
				counts, err = s.retentionRepo.Cleanup(cleanupCtx, s.retentionConfig, time.Now().UTC())
				return err
			})
			if err != nil {
				s.logger.Printf("Retention cleanup: %v", err)
				continue
			}
			for category, count := range counts {
				if count > 0 {
					s.logger.Printf("Retention cleanup: %s removed=%d", category, count)
				}
			}
		}
	}
}

func (s *Server) evaluateMetricAlerts(
	ctx context.Context,
) {
	const evaluationInterval = 15 * time.Second
	s.jobBeats.Register("host_availability_alerts", evaluationInterval)
	s.jobBeats.Register("metric_alerts", evaluationInterval)

	ticker := time.NewTicker(evaluationInterval)
	defer ticker.Stop()

	s.logger.Println(
		"Metric alert evaluation job started",
	)

	evaluate := func() {
		if err := s.runJob(ctx, "host_availability_alerts", s.alertsService.EvaluateHostAvailability); err != nil {
			s.logger.Printf(
				"Error evaluating host availability: %v",
				err,
			)
			return
		}

		if err := s.runJob(ctx, "metric_alerts", s.alertsService.EvaluateMetricAlerts); err != nil {
			s.logger.Printf(
				"Error evaluating metric alerts: %v",
				err,
			)
		}
	}

	// Evaluate once immediately when the server starts.
	evaluate()

	for {
		select {
		case <-ctx.Done():
			s.logger.Println(
				"Stopping metric alert evaluation job",
			)
			return

		case <-ticker.C:
			evaluate()
		}
	}
}

func (s *Server) sendDailyAlertSummary(
	ctx context.Context,
) {
	lisbonLocation, err := time.LoadLocation(
		"Europe/Lisbon",
	)
	if err != nil {
		s.logger.Printf(
			"Failed to load Lisbon timezone: %v",
			err,
		)
		return
	}

	s.logger.Println(
		"Daily alert summary job started",
	)
	s.jobBeats.Register("daily_summary", 24*time.Hour)

	for {
		now := time.Now().In(lisbonLocation)

		nextRun := time.Date(
			now.Year(),
			now.Month(),
			now.Day(),
			9,
			0,
			0,
			0,
			lisbonLocation,
		)

		if !nextRun.After(now) {
			nextRun = nextRun.AddDate(0, 0, 1)
		}

		s.logger.Printf(
			"Next daily alert summary scheduled for %s",
			nextRun.Format("02 Jan 2006, 15:04 MST"),
		)

		timer := time.NewTimer(
			time.Until(nextRun),
		)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			s.logger.Println(
				"Stopping daily alert summary job",
			)
			return

		case runTime := <-timer.C:
			if err := s.runJob(ctx, "daily_summary", func(ctx context.Context) error {
				return s.alertsService.SendDailyAlertSummary(ctx, runTime.UTC())
			}); err != nil {
				s.logger.Printf(
					"Error sending daily alert summary: %v",
					err,
				)
				continue
			}

			s.logger.Println("Daily alert summary queued")
		}
	}
}

// runJob runs one iteration of a background job, if this instance leads it
// (ADR-0007), records its duration and outcome (ADR-0004) and its heartbeat
// (ADR-0010). An error caused by shutdown is not counted as a failure.
func (s *Server) runJob(ctx context.Context, name string, run func(context.Context) error) error {
	if s.elector != nil {
		lead, err := s.elector.Lead(ctx, name)
		s.metrics.SetLeader(name, lead)
		s.jobBeats.Starting(name, lead && err == nil)
		if err != nil {
			return fmt.Errorf("leader election: %w", err)
		}
		if !lead {
			return nil
		}
	} else {
		s.jobBeats.Starting(name, true)
	}
	defer s.jobBeats.Finished(name)
	started := time.Now()
	err := run(ctx)
	if ctx.Err() == nil {
		s.metrics.ObserveJob(name, started, err)
	}
	return err
}
