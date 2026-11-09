package services

import (
	"context"
	"log"
	"net/mail"
	"strings"

	"monitor-central/internal/models"
	"monitor-central/internal/repositories"
)

// UserService handles user business logic
type UserService struct {
	userRepo  *repositories.UserRepository
	auditRepo *repositories.AuditRepository
	logger    *log.Logger
}

// NewUserService creates a new UserService
func NewUserService(userRepo *repositories.UserRepository, auditRepo *repositories.AuditRepository, logger *log.Logger) *UserService {
	return &UserService{
		userRepo:  userRepo,
		auditRepo: auditRepo,
		logger:    logger,
	}
}

// GetProfile retrieves a user's profile
func (s *UserService) GetProfile(ctx context.Context, userID int64) (*models.User, error) {
	return s.userRepo.GetByID(ctx, userID)
}

// UpdateNotificationEmail changes the email used for alerts.
func (s *UserService) UpdateNotificationEmail(
	ctx context.Context,
	userID int64,
	notificationEmail string,
) error {
	notificationEmail = strings.ToLower(
		strings.TrimSpace(notificationEmail),
	)

	if notificationEmail == "" {
		return s.userRepo.UpdateNotificationEmail(ctx, userID, "")
	}
	if len(notificationEmail) > 254 {
		return models.ErrInvalidEmail
	}
	address, err := mail.ParseAddress(notificationEmail)
	if err != nil ||
		address.Address != notificationEmail {
		return models.ErrInvalidEmail
	}

	return s.userRepo.UpdateNotificationEmail(
		ctx,
		userID,
		notificationEmail,
	)
}
