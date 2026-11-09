package models

import (
	"errors"
	"time"
)

// Domain errors
var (
	ErrInvalidEmail       = errors.New("invalid email format")
	ErrWeakPassword       = errors.New("password does not meet requirements")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrEmailExists        = errors.New("email already exists")
)

// User represents a user account
type User struct {
	ID                int64      `json:"id" db:"id"`
	Email             string     `json:"email" db:"email"`
	NotificationEmail string     `json:"notification_email" db:"notification_email"`
	Name              string     `json:"name" db:"name"`
	PasswordHash      string     `json:"-" db:"password_hash"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

// AuditLogEntry represents an audit log entry
type AuditLogEntry struct {
	ID        int64     `json:"id" db:"id"`
	UserID    int64     `json:"user_id" db:"user_id"`
	Action    string    `json:"action" db:"action"`
	IPAddress string    `json:"ip_address" db:"ip_address"`
	Timestamp time.Time `json:"timestamp" db:"timestamp"`
}

// Session represents a session record (for SCS store)
type Session struct {
	Token  string    `db:"token"`
	Data   []byte    `db:"data"`
	Expiry time.Time `db:"expiry"`
}

// LoginRequest represents login credentials
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ErrorResponse represents API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// SuccessResponse represents API success response
type SuccessResponse struct {
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data,omitempty"`
}
