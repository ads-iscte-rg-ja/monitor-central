package config

import "testing"

func TestLoadHealthConfig(t *testing.T) {
	for value, want := range map[string]int{"": DefaultHostHealthConcurrency, "1": 1, "1024": 1024} {
		t.Setenv("HEALTH_CONCURRENCY", value)
		c, err := LoadHealthConfig()
		if err != nil || c.Concurrency != want {
			t.Fatalf("%q: %+v %v", value, c, err)
		}
	}
	for _, value := range []string{"0", "1025", "many"} {
		t.Setenv("HEALTH_CONCURRENCY", value)
		if _, err := LoadHealthConfig(); err == nil {
			t.Fatalf("%q accepted", value)
		}
	}
}
