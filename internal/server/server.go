// Package server contains all the logic to initiliaze a new server
package server

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/grpcapi"
	"monitor-central/internal/handlers"
	"monitor-central/internal/observability"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
	"monitor-central/internal/templating/pages"
	"monitor-central/internal/utils/apikey"
	"monitor-central/internal/utils/email"
	"monitor-central/internal/utils/middleware"
	"monitor-central/internal/utils/session"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"google.golang.org/grpc"
)

type Server struct {
	db                *database.DB
	sessionStore      *repositories.SessionStore
	srv               *http.Server
	grpcSrv           *grpc.Server
	metricsSrv        *http.Server
	metrics           *observability.Metrics
	grpcAddr          string
	logger            *log.Logger
	retentionRepo     *repositories.RetentionRepository
	retentionConfig   config.RetentionConfig
	alertsService     *services.AlertsService
	hostHealthService *services.HostHealthService
	role              config.Role
	elector           jobLeader
	jobBeats          *services.JobHeartbeats
	location          *time.Location // APP_TIMEZONE: when the daily summary runs
	wg                sync.WaitGroup
}

// jobLeader decides whether this instance runs a background job (ADR-0007).
type jobLeader interface {
	Lead(ctx context.Context, job string) (bool, error)
	Close()
}

func New(logger *log.Logger, slogger *slog.Logger, obs *config.ObservabilityConfig) (*Server, error) {
	cfg, err := loadSettings()
	if err != nil {
		return nil, err
	}
	serverConfig, hasher, loginConfig, retentionConfig, dbConfig, ingestConfig :=
		cfg.server, cfg.hasher, cfg.login, cfg.retention, cfg.db, cfg.ingest
	scaling := cfg.scaling

	// Initialize Database
	db, err := database.New(dbConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			_ = db.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = db.CheckSchema(ctx)
	if err == nil {
		err = db.CheckAPIKeySecret(ctx, hasher)
	}
	cancel()
	if err != nil {
		return nil, err
	}

	smtpConfig, err := config.LoadSMTPConfig()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to load SMTP config: %w",
			err,
		)
	}

	emailClient := email.NewClient(smtpConfig)

	// Initialize Repositories
	userRepo := repositories.NewUserRepository(db)
	auditRepo := repositories.NewAuditRepository(db, logger)
	sessionStore := repositories.NewSessionStore(db, logger)
	defer func() {
		if !ready {
			sessionStore.StopCleanup()
		}
	}()
	monitoringRepo := repositories.NewMonitoringRepository(db, hasher)
	dashboardRepo := repositories.NewDashboardRepository(db)
	errorLogsRepo := repositories.NewErrorLogsRepository(db)
	hostsRepo := repositories.NewHostsRepository(db, hasher)
	alertsRepo := repositories.NewAlertsRepository(db)

	// Metrics (ADR-0004): read at scrape time from the pool and the outbox.
	metrics := observability.NewMetrics(slogger, observability.Options{
		DB:            db.DB,
		OutboxPending: alertsRepo.CountPendingNotifications,
	})

	// Initialize Session Manager
	sessionConfig := session.DefaultSessionConfig()
	sessionConfig.CookieSecure = serverConfig.CookieSecure
	sessionManager := session.NewSessionManager(sessionStore, sessionConfig)

	// Initialize Services
	authService := services.NewAuthService(userRepo, auditRepo, logger, loginConfig, repositories.NewLoginAttemptsRepository(db))
	userService := services.NewUserService(userRepo, auditRepo, logger)
	ingestionService := services.NewIngestionService(monitoringRepo)
	dashboardService := services.NewDashboardService(dashboardRepo)
	errorLogsService := services.NewErrorLogsService(errorLogsRepo, cfg.location)
	hostsService := services.NewHostsService(hostsRepo)
	alertsService := services.NewAlertsService(
		alertsRepo,
		emailClient,
	)
	alertsService.SetLocation(cfg.location)
	tb, tbErr := pages.NewPageBox(serverConfig.FrontendPath, logger)
	if tbErr != nil {
		return nil, fmt.Errorf("create pages: %w", tbErr)
	}

	// Initialize Handlers
	authHandler := handlers.NewAuthHandler(authService, sessionManager, tb, logger)
	userHandler := handlers.NewUserHandler(userService, sessionManager, tb, logger)
	// Heartbeats of the jobs this instance leads, for /health/ready (ADR-0010).
	jobBeats := services.NewJobHeartbeats(time.Now)
	healthHandler := handlers.NewHealthHandler(services.NewHealthService(db, jobBeats), logger)
	legalHandler := handlers.NewLegalHandler(tb, sessionManager)
	dashboardHandler := handlers.NewDashboardHandler(
		dashboardService,
		tb,
		logger,
	)
	errorLogsHandler := handlers.NewErrorLogsHandler(
		errorLogsService,
		tb,
		logger,
	)
	hostsHandler := handlers.NewHostsHandler(
		hostsService,
		tb,
		logger,
	)
	alertsHandler := handlers.NewAlertsHandler(
		alertsService,
		tb,
		logger,
	)
	// Initialize Middleware
	mdw := middleware.New(logger, sessionManager, authService)

	r := chi.NewRouter()

	// Middleware
	// Behind the load balancer, the client address comes from the trusted proxy (ADR-0007).
	r.Use(middleware.TrustedProxies(scaling.TrustedProxies))
	r.Use(chimiddleware.RequestID)
	r.Use(metrics.HTTPMiddleware(slogger))
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))
	// Violation reports come from the browser, without a session or CSRF headers.
	r.Post("/csp-report", middleware.CSPReport)
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(mdw.CSRF())
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Body = http.MaxBytesReader(w, r.Body, config.MaxRequestBodySize)
				next.ServeHTTP(w, r)
			})
		})

		// --- ADD ROUTES
		setupRoutes(
			r,
			logger,
			tb,
			authHandler,
			userHandler,
			healthHandler,
			legalHandler,
			dashboardHandler,
			errorLogsHandler,
			hostsHandler,
			alertsHandler,
			sessionManager,
			mdw,
		)
	})

	// We cannot forget this! This has to come last
	tb.MountStatic(r)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", serverConfig.Port),
		Handler:      middleware.SecurityHeaders(cfg.web)(httpHandler(scaling.Role, r, healthHandler.Live, healthHandler.Ready)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	metricsSrv := newMetricsServer(obs.MetricsAddr, metrics)

	ready = true
	return &Server{
		db:           db,
		sessionStore: sessionStore,
		srv:          srv,
		// Telemetry ingestion is served over gRPC on its own port (ADR-0003).
		grpcSrv:           grpcapi.NewServer(ingestionService, logger, metrics, slogger, ingestConfig.MaxInflight),
		metrics:           metrics,
		metricsSrv:        metricsSrv,
		grpcAddr:          fmt.Sprintf(":%d", serverConfig.GRPCPort),
		logger:            logger,
		retentionRepo:     repositories.NewRetentionRepository(db),
		retentionConfig:   retentionConfig,
		alertsService:     alertsService,
		hostHealthService: services.NewHostHealthService(hostsRepo, cfg.health.Concurrency),
		role:              scaling.Role,
		elector:           database.NewElector(db),
		jobBeats:          jobBeats,
		location:          cfg.location,
	}, nil
}

func (s *Server) Run() error {
	defer s.Close()
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.srv.Addr, err)
	}
	grpcLn, err := s.listenGRPC()
	if err != nil {
		_ = ln.Close()
		return err
	}
	stopMetrics, err := s.startMetrics()
	if err != nil {
		_ = ln.Close()
		_ = grpcLn.Close()
		return err
	}
	defer stopMetrics()

	// Start background jobs
	ctxBg, cancelBg := context.WithCancel(context.Background())
	defer func() { cancelBg(); s.wg.Wait() }()
	if s.role.RunsJobs() {
		s.startBackgroundJobs(ctxBg)
	}

	errChan := make(chan error, 2)

	go func() {
		log.Printf("Server starting on %s", s.srv.Addr)
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()
	if grpcLn != nil {
		go func() {
			log.Printf("gRPC ingestion starting on %s", s.grpcAddr)
			if err := s.grpcSrv.Serve(grpcLn); err != nil && err != grpc.ErrServerStopped {
				errChan <- fmt.Errorf("grpc: %w", err)
			}
		}()
	}

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	select {
	case err := <-errChan:
		s.grpcSrv.Stop()
		return fmt.Errorf("server error: %w", err)
	case sig := <-signalChan:
		log.Printf("Received %v, initiating graceful shutdown...", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop accepting telemetry and let in-flight batches finish within the same
	// deadline; an unfinished batch is simply retried by the agent (at-least-once).
	grpcDone := make(chan struct{})
	go func() { s.grpcSrv.GracefulStop(); close(grpcDone) }()
	if err := s.srv.Shutdown(ctx); err != nil {
		s.grpcSrv.Stop()
		log.Printf("Server forced to shutdown: %v", err)
		return err
	}
	select {
	case <-grpcDone:
	case <-ctx.Done():
		s.grpcSrv.Stop()
	}

	// Stop background jobs
	cancelBg()
	s.wg.Wait()

	log.Println("Server stopped gracefully")
	return nil
}

// Close gives up the job leads first, so another instance takes over at once.
func (s *Server) Close() { s.elector.Close(); s.sessionStore.StopCleanup(); _ = s.db.Close() }

// httpHandler is the full application, or only the health checks for a
// worker (ADR-0007, ADR-0010).
func httpHandler(role config.Role, app http.Handler, live, ready http.HandlerFunc) http.Handler {
	if role.Serves() {
		return app
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", ready)
	mux.HandleFunc("GET /health/ready", ready)
	mux.HandleFunc("GET /health/live", live)
	return mux
}

// listenGRPC opens the ingestion listener, or none for a worker (ADR-0007).
func (s *Server) listenGRPC() (net.Listener, error) {
	if !s.role.Serves() {
		return nil, nil
	}
	ln, err := net.Listen("tcp", s.grpcAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", s.grpcAddr, err)
	}
	return ln, nil
}

// newMetricsServer serves /metrics on its own listener (ADR-0004); an empty
// address disables it.
func newMetricsServer(addr string, metrics *observability.Metrics) *http.Server {
	if addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
}

// startMetrics starts the metrics listener, if configured, and returns the
// function that stops it.
func (s *Server) startMetrics() (func(), error) {
	if s.metricsSrv == nil {
		return func() {}, nil
	}
	ln, err := net.Listen("tcp", s.metricsSrv.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", s.metricsSrv.Addr, err)
	}
	go func() {
		log.Printf("Metrics listening on %s/metrics", s.metricsSrv.Addr)
		if err := s.metricsSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server: %v", err)
		}
	}()
	return func() { _ = s.metricsSrv.Close() }, nil
}

// settings is every configuration the server needs, loaded and validated
// before anything is started.
type settings struct {
	server    *config.ServerConfig
	hasher    *apikey.Hasher
	login     config.LoginConfig
	retention config.RetentionConfig
	db        *config.DBConfig
	ingest    *config.IngestConfig
	scaling   *config.ScalingConfig
	health    config.HealthConfig
	location  *time.Location
	web       config.WebSecurityConfig
}

func loadSettings() (*settings, error) {
	if err := config.LoadEnv(".env"); err != nil {
		return nil, fmt.Errorf("load environment: %w", err)
	}
	var s settings
	var err error
	if s.server, err = config.LoadServerConfig(); err != nil {
		return nil, err
	}
	secret, err := config.LoadAPIKeySecret()
	if err != nil {
		return nil, err
	}
	if s.hasher, err = apikey.New(secret); err != nil {
		return nil, err
	}
	if s.login, err = config.LoadLoginConfig(); err != nil {
		return nil, err
	}
	if s.retention, err = config.LoadRetentionConfig(); err != nil {
		return nil, err
	}
	if s.db, err = config.LoadDBConfig(); err != nil {
		return nil, fmt.Errorf("failed to load database config: %w", err)
	}
	if s.ingest, err = config.LoadIngestConfig(s.db.MaxOpenConns); err != nil {
		return nil, err
	}
	if s.scaling, err = config.LoadScalingConfig(); err != nil {
		return nil, err
	}
	if s.health, err = config.LoadHealthConfig(); err != nil {
		return nil, err
	}
	if s.location, err = config.LoadTimezone(); err != nil {
		return nil, err
	}
	if s.web, err = config.LoadWebSecurityConfig(); err != nil {
		return nil, err
	}
	return &s, nil
}
