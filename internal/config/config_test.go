package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear any env vars that might affect defaults
	for _, key := range []string{"HORIZON_URL", "DATABASE_URL", "COINGECKO_URL", "HTTP_PORT", "HORIZON_RETRY_MAX"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	cfg := Load()

	if cfg.HorizonURL != "https://horizon.stellar.org" {
		t.Errorf("HorizonURL = %q, want default", cfg.HorizonURL)
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.CoinGeckoURL != "https://api.coingecko.com/api/v3" {
		t.Errorf("CoinGeckoURL = %q, want default", cfg.CoinGeckoURL)
	}
	if cfg.HorizonRetryMax != 5 {
		t.Errorf("HorizonRetryMax = %d, want 5", cfg.HorizonRetryMax)
	}
	if cfg.HorizonRetryBaseDelay != 2*time.Second {
		t.Errorf("HorizonRetryBaseDelay = %v, want 2s", cfg.HorizonRetryBaseDelay)
	}
	if cfg.CoinGeckoDelay != 6*time.Second {
		t.Errorf("CoinGeckoDelay = %v, want 6s", cfg.CoinGeckoDelay)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, want 8080", cfg.HTTPPort)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("HORIZON_URL", "https://custom-horizon.example.com")
	t.Setenv("DATABASE_URL", "postgres://localhost/testdb")
	t.Setenv("HTTP_PORT", "9090")
	t.Setenv("HORIZON_RETRY_MAX", "10")
	t.Setenv("HORIZON_RETRY_BASE_DELAY", "5s")

	cfg := Load()

	if cfg.HorizonURL != "https://custom-horizon.example.com" {
		t.Errorf("HorizonURL = %q, want override", cfg.HorizonURL)
	}
	if cfg.DatabaseURL != "postgres://localhost/testdb" {
		t.Errorf("DatabaseURL = %q, want override", cfg.DatabaseURL)
	}
	if cfg.HTTPPort != "9090" {
		t.Errorf("HTTPPort = %q, want 9090", cfg.HTTPPort)
	}
	if cfg.HorizonRetryMax != 10 {
		t.Errorf("HorizonRetryMax = %d, want 10", cfg.HorizonRetryMax)
	}
	if cfg.HorizonRetryBaseDelay != 5*time.Second {
		t.Errorf("HorizonRetryBaseDelay = %v, want 5s", cfg.HorizonRetryBaseDelay)
	}
}

func TestLoadInvalidEnvFallsBackToDefault(t *testing.T) {
	t.Setenv("HORIZON_RETRY_MAX", "not-a-number")
	t.Setenv("HORIZON_RETRY_BASE_DELAY", "invalid-duration")

	cfg := Load()

	if cfg.HorizonRetryMax != 5 {
		t.Errorf("HorizonRetryMax = %d, want default 5 on invalid input", cfg.HorizonRetryMax)
	}
	if cfg.HorizonRetryBaseDelay != 2*time.Second {
		t.Errorf("HorizonRetryBaseDelay = %v, want default 2s on invalid input", cfg.HorizonRetryBaseDelay)
	}
}
