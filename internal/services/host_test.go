package services

import (
	"context"
	"encoding/hex"
	"errors"
	"monitor-central/internal/models"
	"testing"
)

type fakeHostsRepository struct {
	host    *models.Host
	hosts   []models.HostListItem
	key     string
	deleted int64
}

func (f *fakeHostsRepository) ListHosts(context.Context) ([]models.HostListItem, error) {
	return f.hosts, nil
}
func (f *fakeHostsRepository) CreateHost(_ context.Context, h *models.Host) error {
	h.ID = 41
	f.host = h
	return nil
}
func (f *fakeHostsRepository) DeleteHost(_ context.Context, id int64) error {
	f.deleted = id
	return nil
}
func (f *fakeHostsRepository) UpdateHost(_ context.Context, h *models.Host) error {
	f.host = h
	return nil
}
func (f *fakeHostsRepository) UpdateHostAPIKey(_ context.Context, id int64, key string) error {
	f.key = key
	return nil
}
func TestHostCreationAndKeyRotation(t *testing.T) {
	repo := &fakeHostsRepository{}
	s := NewHostsService(repo)
	h, err := s.CreateHost(context.Background(), models.CreateHostRequest{Name: " Server ", Hostname: " server.local "})
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != 41 || h.Name != "Server" || h.Hostname != "server.local" {
		t.Fatalf("unexpected host %+v", h)
	}
	if len(h.APIKey) != 64 {
		t.Fatal("expected 256-bit key")
	}
	if _, err := hex.DecodeString(h.APIKey); err != nil {
		t.Fatal(err)
	}
	key, err := s.RegenerateAPIKey(context.Background(), h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if key == h.APIKey || key != repo.key {
		t.Fatal("rotation must generate and persist a new key")
	}
	if err := s.DeleteHost(context.Background(), h.ID); err != nil || repo.deleted != 41 {
		t.Fatal("delete failed")
	}
	if err := s.DeleteHost(context.Background(), 0); !errors.Is(err, models.ErrHostIDRequired) {
		t.Fatal(err)
	}
}
func TestHostPageMasksCredentials(t *testing.T) {
	cpu, ram, disk := 45.5, 50.0, 24.0
	repo := &fakeHostsRepository{hosts: []models.HostListItem{{ID: 1, APIKeyMasked: "secr...tial", CPUPercent: &cpu, RAMPercent: &ram, DiskPercent: &disk}}}
	p, err := NewHostsService(repo).GetPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h := p.Hosts[0]
	if h.APIKey != "" || h.APIKeyMasked != "secr...tial" || h.CPUDisplay != "45.5%" || h.LastSeen != "Never" {
		t.Fatalf("unexpected page %+v", h)
	}
}
