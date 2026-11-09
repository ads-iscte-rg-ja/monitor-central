package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Configuration constants
const (
	MinPasswordLength    = 8
	MaxPasswordLength    = 128
	MaxEmailLength       = 254
	BcryptCost           = 12
	SessionMaxAge        = 24 * time.Hour
	DefaultRetentionDays = 730
	CleanupInterval      = 24 * time.Hour
	SessionCleanup       = 5 * time.Minute
	RequestTimeout       = 30 * time.Second
	ShutdownTimeout      = 30 * time.Second
	MaxRequestBodySize   = 1048576 // 1MB
)

// Context keys
type ContextKey string

const (
	UserIDContextKey          ContextKey = "user_id"
	HostIDContextKey          ContextKey = "host_id"
	IsAuthenticatedContextKey ContextKey = "is_authenticated"
)

// Database configuration errors
var (
	ErrDBHostRequired     = errors.New("POSTGRES_HOST is required")
	ErrDBUserRequired     = errors.New("POSTGRES_USER is required")
	ErrDBPasswordRequired = errors.New("POSTGRES_PASSWORD is required")
	ErrDBNameRequired     = errors.New("POSTGRES_DB is required")

	// Email configuration errors
	ErrSMTPHostRequired = errors.New("SMTP_HOST is required")
	ErrSMTPPortRequired = errors.New("SMTP_PORT is required")
	ErrSMTPUserRequired = errors.New("SMTP_USER is required")
	ErrSMTPPassRequired = errors.New("SMTP_PASS is required")
	ErrSMTPFromRequired = errors.New("SMTP_FROM is required")
)

// DBConfig holds database configuration
type DBConfig struct {
	Host            string
	Port            int
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// SMTPConfig holds email configuration
type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// LoadEnv loads environment variables from a .env file
func LoadEnv(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		// .env file is optional, not an error if missing
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		value = strings.Trim(value, `"'`)

		// Only set if not already defined (env vars take precedence)
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

// LoadDBConfig loads database configuration from environment variables
func LoadDBConfig() (*DBConfig, error) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		return nil, ErrDBHostRequired
	}

	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		return nil, ErrDBUserRequired
	}

	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		return nil, ErrDBPasswordRequired
	}

	name := os.Getenv("POSTGRES_DB")
	if name == "" {
		return nil, ErrDBNameRequired
	}

	return &DBConfig{
		Host:            host,
		Port:            GetEnvInt("POSTGRES_PORT", 5432),
		User:            user,
		Password:        password,
		Name:            name,
		SSLMode:         GetEnv("POSTGRES_SSLMODE", "disable"),
		MaxOpenConns:    GetEnvInt("POSTGRES_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    GetEnvInt("POSTGRES_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: 5 * time.Minute,
	}, nil
}

// AppConfig holds application configuration
type AppConfig struct {
	BaseURL string
}

// LoadAppConfig loads application configuration
func LoadAppConfig() *AppConfig {
	return &AppConfig{
		BaseURL: GetEnv("APP_URL", "http://localhost:3333"),
	}
}

// LoadSMTPConfig loads SMTP configuration
func LoadSMTPConfig() (*SMTPConfig, error) {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil, ErrSMTPHostRequired
	}

	portStr := os.Getenv("SMTP_PORT")
	if portStr == "" {
		return nil, ErrSMTPPortRequired
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP_PORT: %w", err)
	}

	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")

	isLocalSMTP :=
		host == "localhost" ||
			host == "127.0.0.1" ||
			host == "mailpit"

	if !isLocalSMTP {
		if user == "" {
			return nil, ErrSMTPUserRequired
		}

		if pass == "" {
			return nil, ErrSMTPPassRequired
		}
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		return nil, ErrSMTPFromRequired
	}

	return &SMTPConfig{
		Host: host,
		Port: port,
		User: user,
		Pass: pass,
		From: from,
	}, nil
}

// GetEnv returns environment variable value or default
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvRequired returns environment variable or error if not set
func GetEnvRequired(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

// GetEnvInt returns environment variable as int or default
func GetEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

// GetEnvBool returns environment variable as bool or default
func GetEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}
