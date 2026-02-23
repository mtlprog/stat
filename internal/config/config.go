package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	HorizonURL            string
	DatabaseURL           string
	CoinGeckoURL          string
	HorizonRetryMax       int
	HorizonRetryBaseDelay time.Duration
	CoinGeckoDelay        time.Duration
	CoinGeckoRetryMax    int
	QuoteWorkerInterval  time.Duration
	ReportWorkerInterval  time.Duration
	HTTPPort              string
	AdminAPIKey           string
	GoogleSheetsSpreadsheetID string
	GoogleCredentialsJSON     string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		HorizonURL:            envOrDefault("HORIZON_URL", "https://horizon.stellar.org"),
		DatabaseURL:           envOrDefaultWarn("DATABASE_URL", ""),
		CoinGeckoURL:          envOrDefault("COINGECKO_URL", "https://api.coingecko.com/api/v3"),
		HorizonRetryMax:       envOrDefaultInt("HORIZON_RETRY_MAX", 5),
		HorizonRetryBaseDelay: envOrDefaultDuration("HORIZON_RETRY_BASE_DELAY", 2*time.Second),
		CoinGeckoDelay:        envOrDefaultDuration("COINGECKO_DELAY", 6*time.Second),
		CoinGeckoRetryMax:    envOrDefaultInt("COINGECKO_RETRY_MAX", 5),
		QuoteWorkerInterval:  envOrDefaultDuration("QUOTE_WORKER_INTERVAL", 1*time.Hour),
		ReportWorkerInterval:  envOrDefaultDuration("REPORT_WORKER_INTERVAL", 24*time.Hour),
		HTTPPort:              envOrDefault("HTTP_PORT", "8080"),
		AdminAPIKey:               os.Getenv("ADMIN_API_KEY"),
		GoogleSheetsSpreadsheetID: os.Getenv("GOOGLE_SHEETS_SPREADSHEET_ID"),
		GoogleCredentialsJSON:     os.Getenv("GOOGLE_CREDENTIALS_JSON"),
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envOrDefaultWarn(key, defaultVal string) string {
	v := envOrDefault(key, defaultVal)
	if v == "" {
		slog.Warn("required env var not set", "key", key)
	}
	return v
}

func envOrDefaultInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			slog.Warn("invalid integer env var, using default", "key", key, "value", v, "default", defaultVal)
			return defaultVal
		}
		return n
	}
	return defaultVal
}

func envOrDefaultDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			slog.Warn("invalid duration env var, using default", "key", key, "value", v, "default", defaultVal)
			return defaultVal
		}
		return d
	}
	return defaultVal
}
