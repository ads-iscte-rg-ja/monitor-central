package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretSetupPreservesEnvironmentAndRefusesRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "# keep\nOTHER=value\nHOST_API_KEY_SECRET=\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configureSecret(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# keep\nOTHER=value\nHOST_API_KEY_SECRET=") {
		t.Fatal("unrelated configuration changed")
	}
	value := strings.TrimSpace(strings.Split(string(data), "HOST_API_KEY_SECRET=")[1])
	secret, err := hex.DecodeString(value)
	if err != nil || len(secret) != 32 {
		t.Fatal("invalid generated secret")
	}
	if err := configureSecret(path); err == nil {
		t.Fatal("configured secret was replaced")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("secret changed")
	}
}
