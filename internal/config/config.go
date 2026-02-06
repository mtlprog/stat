package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	HorizonURL           string
	DatabaseURL          string
	CoinGeckoURL         string
	HorizonRetryMax      int
	HorizonRetryBaseDelay time.Duration
	CoinGeckoDelay       time.Duration
	CoinGeckoRetryMax    int
	QuoteStaleThreshold  time.Duration
	QuoteWorkerInterval  time.Duration
	ReportWorkerInterval time.Duration
	HTTPPort             string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		HorizonURL:           envOrDefault("HORIZON_URL", "https://horizon.stellar.org"),
		DatabaseURL:          envOrDefault("DATABASE_URL", ""),
		CoinGeckoURL:         envOrDefault("COINGECKO_URL", "https://api.coingecko.com/api/v3"),
		HorizonRetryMax:      envOrDefaultInt("HORIZON_RETRY_MAX", 5),
		HorizonRetryBaseDelay: envOrDefaultDuration("HORIZON_RETRY_BASE_DELAY", 2*time.Second),
		CoinGeckoDelay:       envOrDefaultDuration("COINGECKO_DELAY", 6*time.Second),
		CoinGeckoRetryMax:    envOrDefaultInt("COINGECKO_RETRY_MAX", 5),
		QuoteStaleThreshold:  envOrDefaultDuration("QUOTE_STALE_THRESHOLD", 2*time.Hour),
		QuoteWorkerInterval:  envOrDefaultDuration("QUOTE_WORKER_INTERVAL", 1*time.Hour),
		ReportWorkerInterval: envOrDefaultDuration("REPORT_WORKER_INTERVAL", 24*time.Hour),
		HTTPPort:             envOrDefault("HTTP_PORT", "8080"),
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envOrDefaultInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func envOrDefaultDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}
