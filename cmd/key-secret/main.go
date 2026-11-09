// key-secret configures a private HMAC secret without printing it.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := configureSecret(".env"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("HOST_API_KEY_SECRET configured in .env. Back up this file securely; the secret was not printed.")
}

func configureSecret(path string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("environment file must be a regular file")
		}
		if info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("environment file must be private (mode 0600); adjust its permissions before configuring secrets")
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	lines := strings.Split(string(data), "\n")
	index := -1
	for i, line := range lines {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != "HOST_API_KEY_SECRET" {
			continue
		}
		if index >= 0 || strings.TrimSpace(strings.Trim(parts[1], `"' `)) != "" {
			return fmt.Errorf("HOST_API_KEY_SECRET already configured; refusing to replace it")
		}
		index = i
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	setting := "HOST_API_KEY_SECRET=" + hex.EncodeToString(secret)
	if index >= 0 {
		lines[index] = setting
	} else {
		lines = append(lines, setting)
	}
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	file, err := os.CreateTemp(filepath.Dir(path), ".monitor-env-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(content); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
