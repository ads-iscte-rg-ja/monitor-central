package config

import (
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
)

type AdminConfig struct{ Email, Password, Name, NotificationEmail string }

func LoadAdminConfig() (*AdminConfig, error) {
	email, err := GetEnvRequired("ADMIN_EMAIL")
	if err != nil {
		return nil, err
	}
	password, err := GetEnvRequired("ADMIN_PASSWORD")
	if err != nil {
		return nil, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > 254 {
		return nil, fmt.Errorf("ADMIN_EMAIL must be a valid email address")
	}
	if len(password) < 12 || len(password) > 72 {
		return nil, fmt.Errorf("ADMIN_PASSWORD must contain 12–72 bytes")
	}
	notification := strings.TrimSpace(os.Getenv("ADMIN_NOTIFICATION_EMAIL"))
	if notification != "" {
		a, err := mail.ParseAddress(notification)
		if err != nil || a.Address != notification || len(notification) > 254 {
			return nil, fmt.Errorf("ADMIN_NOTIFICATION_EMAIL must be a valid email address")
		}
	}
	return &AdminConfig{Email: email, Password: password, Name: GetEnv("ADMIN_NAME", "Administrator"), NotificationEmail: notification}, nil
}

type ServerConfig struct {
	Port         int
	GRPCPort     int
	FrontendPath string
	CookieSecure bool
}

func LoadServerConfig() (*ServerConfig, error) {
	port, err := strconv.Atoi(GetEnv("PORT", "3333"))
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("PORT must be between 1 and 65535")
	}
	grpcPort, err := strconv.Atoi(GetEnv("GRPC_PORT", "3334"))
	if err != nil || grpcPort < 1 || grpcPort > 65535 || grpcPort == port {
		return nil, fmt.Errorf("GRPC_PORT must be between 1 and 65535 and differ from PORT")
	}
	secure, err := strconv.ParseBool(GetEnv("COOKIE_SECURE", "true"))
	if err != nil {
		return nil, fmt.Errorf("COOKIE_SECURE must be true or false")
	}
	if GetEnv("APP_ENV", "development") == "production" && !secure {
		return nil, fmt.Errorf("production requires COOKIE_SECURE=true")
	}
	return &ServerConfig{Port: port, GRPCPort: grpcPort, FrontendPath: GetEnv("FRONTEND_PATH", "frontend"), CookieSecure: secure}, nil
}
