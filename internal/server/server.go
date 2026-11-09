// Package server contains all the logic to initiliaze a new server
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/database"
	"monitor-central/internal/handlers"
	"monitor-central/internal/repositories"
	"monitor-central/internal/services"
	"monitor-central/internal/templating/pages"
	"monitor-central/internal/utils/apikey"
	"monitor-central/internal/utils/email"
	"monitor-central/internal/utils/middleware"
	"monitor-central/internal/utils/session"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	db                *database.DB
	sessionStore      *repositories.SessionStore
	srv               *http.Server
	logger            *log.Logger
	retentionRepo     *repositories.RetentionRepository
	retentionConfig   config.RetentionConfig
	alertsService     *services.AlertsService
	hostHealthService *services.HostHealthService
	wg                sync.WaitGroup
}

func New(logger *log.Logger) (*Server, error) {
	// Load App Configuration
	if err := config.LoadEnv(".env"); err != nil {
		return nil, fmt.Errorf("load environment: %w", err)
	}
	serverConfig, err := config.LoadServerConfig()
	if err != nil {
		return nil, err
	}
	secret, err := config.LoadAPIKeySecret()
	if err != nil {
		return nil, err
	}
	hasher, err := apikey.New(secret)
	if err != nil {
		return nil, err
	}
	loginConfig, err := config.LoadLoginConfig()
	if err != nil {
		return nil, err
	}
	retentionConfig, err := config.LoadRetentionConfig()
	if err != nil {
		return nil, err
	}

	dbConfig, err := config.LoadDBConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load database config: %w", err)
	}

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

	// Initialize Session Manager
	sessionConfig := session.DefaultSessionConfig()
	sessionConfig.CookieSecure = serverConfig.CookieSecure
	sessionManager := session.NewSessionManager(sessionStore, sessionConfig)

	// Initialize Services
	authService := services.NewAuthService(userRepo, auditRepo, logger, loginConfig)
	userService := services.NewUserService(userRepo, auditRepo, logger)
	ingestionService := services.NewIngestionService(monitoringRepo)
	dashboardService := services.NewDashboardService(dashboardRepo)
	errorLogsService := services.NewErrorLogsService(errorLogsRepo)
	hostsService := services.NewHostsService(hostsRepo)
	alertsService := services.NewAlertsService(
		alertsRepo,
		emailClient,
	)
	tb, tbErr := pages.NewPageBox(serverConfig.FrontendPath, logger)
	if tbErr != nil {
		return nil, fmt.Errorf("create pages: %w", tbErr)
	}

	// Initialize Handlers
	authHandler := handlers.NewAuthHandler(authService, sessionManager, tb, logger)
	userHandler := handlers.NewUserHandler(userService, sessionManager, tb, logger)
	healthHandler := handlers.NewHealthHandler(db)
	legalHandler := handlers.NewLegalHandler(tb, sessionManager)
	ingestionHandler := handlers.NewIngestionHandler(ingestionService)
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
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))
	// Machine API authentication is first, outside browser/session middleware.
	r.Route("/api/v1/ingest", func(r chi.Router) {
		r.Use(middleware.RequireHost(ingestionService))
		r.Post("/stats", ingestionHandler.IngestStats)
		r.Post("/errors", ingestionHandler.IngestErrors)
	})
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
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ready = true
	return &Server{
		db:                db,
		sessionStore:      sessionStore,
		srv:               srv,
		logger:            logger,
		retentionRepo:     repositories.NewRetentionRepository(db),
		retentionConfig:   retentionConfig,
		alertsService:     alertsService,
		hostHealthService: services.NewHostHealthService(hostsRepo),
	}, nil
}

func (s *Server) Run() error {
	defer s.Close()
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.srv.Addr, err)
	}

	// Start background jobs
	ctxBg, cancelBg := context.WithCancel(context.Background())
	defer func() { cancelBg(); s.wg.Wait() }()
	s.startBackgroundJobs(ctxBg)

	errChan := make(chan error, 1)

	go func() {
		log.Printf("Server starting on %s", s.srv.Addr)
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case sig := <-signalChan:
		log.Printf("Received %v, initiating graceful shutdown...", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.srv.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
		return err
	}

	// Stop background jobs
	cancelBg()
	s.wg.Wait()

	log.Println("Server stopped gracefully")
	return nil
}

func (s *Server) Close() { s.sessionStore.StopCleanup(); _ = s.db.Close() }
