package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"strings"
	"sync"
	"time"

	"monitor-central/internal/config"
	"monitor-central/internal/models"

	"golang.org/x/crypto/bcrypt"
)

// AuthService handles authentication business logic
type AuthService struct {
	userRepo  AuthUserRepository
	auditRepo AuthAuditRepository
	logger    *log.Logger
	limiter   *loginLimiter
	dummyHash []byte
}

type AuthUserRepository interface {
	GetByEmail(context.Context, string) (*models.User, error)
	UpdateLastLogin(context.Context, int64) error
	Exists(context.Context, int64) (bool, error)
}
type AuthAuditRepository interface {
	Create(context.Context, int64, string, string) error
}

var ErrLoginRateLimited = errors.New("too many login attempts; try again later")
var dummyPasswordHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("monitor-nonexistent-account-dummy"), config.BcryptCost)
	if err != nil {
		panic(err)
	}
	return hash
})

// NewAuthService creates a new AuthService
func NewAuthService(userRepo AuthUserRepository, auditRepo AuthAuditRepository, logger *log.Logger, limits config.LoginConfig) *AuthService {
	return &AuthService{
		userRepo:  userRepo,
		auditRepo: auditRepo,
		logger:    logger,
		limiter:   newLoginLimiter(limits),
		dummyHash: dummyPasswordHash(),
	}
}

// Login authenticates a user and returns the user ID
func (s *AuthService) Login(ctx context.Context, req *models.LoginRequest, ipAddress string) (int64, error) {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if !s.limiter.allow(ipAddress, req.Email, time.Now()) {
		return 0, ErrLoginRateLimited
	}
	select {
	case s.limiter.work <- struct{}{}:
		defer func() { <-s.limiter.work }()
	default:
		return 0, ErrLoginRateLimited
	}
	// Validate email format
	if err := ValidateEmail(req.Email); err != nil {
		return 0, err
	}

	// Get user
	user, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(req.Password))
			return 0, models.ErrInvalidCredentials
		}
		s.logger.Printf("Database error during login: %v", err)
		return 0, fmt.Errorf("login error: %w", err)
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return 0, models.ErrInvalidCredentials
	}

	// Update last login time
	if err := s.userRepo.UpdateLastLogin(ctx, user.ID); err != nil {
		s.logger.Printf("Failed to update last login: %v", err)
	}

	// Log login
	if err := s.auditRepo.Create(ctx, user.ID, "login", ipAddress); err != nil {
		s.logger.Printf("Failed to log audit entry: %v", err)
	}

	return user.ID, nil
}

// Logout logs the logout action
func (s *AuthService) Logout(ctx context.Context, userID int64, ipAddress string) {
	if userID > 0 {
		if err := s.auditRepo.Create(ctx, userID, "logout", ipAddress); err != nil {
			s.logger.Printf("Failed to log audit entry: %v", err)
		}
	}
}

// UserExists checks if a user exists by ID
func (s *AuthService) UserExists(ctx context.Context, userID int64) (bool, error) {
	return s.userRepo.Exists(ctx, userID)
}

// HashPassword generates a bcrypt hash from a plain password
func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), config.BcryptCost)
	if err != nil {
		return "", fmt.Errorf("error hashing password: %w", err)
	}
	return string(hashed), nil
}

// ValidateEmail validates email format
func ValidateEmail(email string) error {
	if len(email) == 0 || len(email) > config.MaxEmailLength {
		return models.ErrInvalidEmail
	}

	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return models.ErrInvalidEmail
	}

	return nil
}
