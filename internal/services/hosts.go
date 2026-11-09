package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"monitor-central/internal/models"
)

type HostsRepository interface {
	ListHosts(
		ctx context.Context,
	) ([]models.HostListItem, error)
	CreateHost(
		ctx context.Context,
		host *models.Host,
	) error
	DeleteHost(
		ctx context.Context,
		hostID int64,
	) error
	UpdateHost(
		ctx context.Context,
		host *models.Host,
	) error
	UpdateHostAPIKey(
		ctx context.Context,
		hostID int64,
		apiKey string,
	) error
}

type HostsService struct {
	repo HostsRepository
}

func NewHostsService(
	repo HostsRepository,
) *HostsService {
	return &HostsService{repo: repo}
}

func (s *HostsService) GetPage(
	ctx context.Context,
) (*models.HostsPage, error) {
	hosts, err := s.repo.ListHosts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}

	for i := range hosts {
		hosts[i].APIKey = ""
		hosts[i].ContactTimeoutMinutes = hosts[i].ContactTimeoutSeconds / 60
		if hosts[i].ContactTimeoutMinutes == 0 {
			hosts[i].ContactTimeoutMinutes = 5
		}
		hosts[i].HealthChecked = "Never"
		if hosts[i].HealthCheckedAt != nil {
			hosts[i].HealthChecked = hosts[i].HealthCheckedAt.Format("02 Jan 2006, 15:04")
		}

		if hosts[i].LastSeenAt == nil {
			hosts[i].LastSeen = "Never"
		} else {
			hosts[i].LastSeen = hosts[i].LastSeenAt.Format(
				"02 Jan 2006, 15:04",
			)
		}

		hosts[i].CreatedAtDisplay = hosts[i].CreatedAt.Format(
			"02 Jan 2006, 15:04",
		)

		hosts[i].HasStats =
			hosts[i].CPUPercent != nil &&
				hosts[i].RAMPercent != nil &&
				hosts[i].DiskPercent != nil
		if hosts[i].HasStats {
			hosts[i].CPUDisplay = fmt.Sprintf(
				"%.1f%%",
				*hosts[i].CPUPercent,
			)

			hosts[i].RAMDisplay = fmt.Sprintf(
				"%.1f%%",
				*hosts[i].RAMPercent,
			)

			hosts[i].DiskDisplay = fmt.Sprintf(
				"%.1f%%",
				*hosts[i].DiskPercent,
			)
		} else {
			hosts[i].CPUDisplay = "—"
			hosts[i].RAMDisplay = "—"
			hosts[i].DiskDisplay = "—"
		}
	}

	return &models.HostsPage{
		Hosts: hosts,
	}, nil
}

func (s *HostsService) CreateHost(
	ctx context.Context,
	request models.CreateHostRequest,
) (*models.Host, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, models.ErrHostNameRequired
	}

	hostname := strings.TrimSpace(request.Hostname)
	if hostname == "" {
		return nil, models.ErrHostnameRequired
	}
	if len(name) > 200 || len(hostname) > 254 {
		return nil, models.ErrFieldTooLarge
	}
	endpoint, timeout, err := validateHostHealth(request.HealthEndpoint, request.ContactTimeoutMinutes)
	if err != nil {
		return nil, err
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return nil, err
	}

	host := &models.Host{
		Name:                  name,
		Hostname:              hostname,
		APIKey:                apiKey,
		Status:                "unknown",
		HealthEndpoint:        endpoint,
		ContactTimeoutSeconds: timeout,
	}

	if err := s.repo.CreateHost(ctx, host); err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}

	return host, nil
}

func (s *HostsService) DeleteHost(
	ctx context.Context,
	hostID int64,
) error {
	if hostID <= 0 {
		return models.ErrHostIDRequired
	}

	if err := s.repo.DeleteHost(ctx, hostID); err != nil {
		return fmt.Errorf("delete host: %w", err)
	}

	return nil
}

func (s *HostsService) UpdateHost(
	ctx context.Context,
	hostID int64,
	request models.UpdateHostRequest,
) error {
	if hostID <= 0 {
		return models.ErrHostIDRequired
	}

	name := strings.TrimSpace(request.Name)
	if name == "" {
		return models.ErrHostNameRequired
	}

	hostname := strings.TrimSpace(request.Hostname)
	if hostname == "" {
		return models.ErrHostnameRequired
	}
	if len(name) > 200 || len(hostname) > 254 {
		return models.ErrFieldTooLarge
	}
	endpoint, timeout, err := validateHostHealth(request.HealthEndpoint, request.ContactTimeoutMinutes)
	if err != nil {
		return err
	}

	host := &models.Host{
		ID:                    hostID,
		Name:                  name,
		Hostname:              hostname,
		HealthEndpoint:        endpoint,
		ContactTimeoutSeconds: timeout,
	}

	if err := s.repo.UpdateHost(ctx, host); err != nil {
		return fmt.Errorf("update host: %w", err)
	}

	return nil
}

func (s *HostsService) RegenerateAPIKey(
	ctx context.Context,
	hostID int64,
) (string, error) {
	if hostID <= 0 {
		return "", models.ErrHostIDRequired
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return "", err
	}

	if err := s.repo.UpdateHostAPIKey(
		ctx,
		hostID,
		apiKey,
	); err != nil {
		return "", fmt.Errorf(
			"update host API key: %w",
			err,
		)
	}

	return apiKey, nil
}

func generateAPIKey() (string, error) {
	apiKeyBytes := make([]byte, 32)

	if _, err := rand.Read(apiKeyBytes); err != nil {
		return "", fmt.Errorf(
			"generate API key: %w",
			err,
		)
	}

	return hex.EncodeToString(apiKeyBytes), nil
}
