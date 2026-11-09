package config

import (
	"encoding/hex"
	"fmt"
	"strconv"
)

func LoadAPIKeySecret() ([]byte, error) {
	secret, err := hex.DecodeString(GetEnv("HOST_API_KEY_SECRET", ""))
	if err != nil || len(secret) != 32 {
		return nil, fmt.Errorf("HOST_API_KEY_SECRET must be 32 cryptographically random bytes encoded as 64 hex characters")
	}
	return secret, nil
}

type LoginConfig struct {
	PerIP, PerAccount, MaxConcurrent int
}

func LoadLoginConfig() (LoginConfig, error) {
	c := LoginConfig{}
	for _, setting := range []struct {
		name     string
		fallback int
		dest     *int
	}{
		{"LOGIN_ATTEMPTS_PER_IP", 20, &c.PerIP},
		{"LOGIN_ATTEMPTS_PER_ACCOUNT", 5, &c.PerAccount},
		{"LOGIN_MAX_CONCURRENT", 4, &c.MaxConcurrent},
	} {
		n, err := strconv.Atoi(GetEnv(setting.name, strconv.Itoa(setting.fallback)))
		if err != nil || n < 1 || n > 10000 {
			return c, fmt.Errorf("%s must be between 1 and 10000", setting.name)
		}
		*setting.dest = n
	}
	return c, nil
}
