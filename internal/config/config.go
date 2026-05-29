package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	HorizonURL                string
	DatabaseURL               string
	CoinGeckoURL              string
	StellarExpertURL          string
	HorizonRetryMax           int
	HorizonRetryBaseDelay     time.Duration
	CoinGeckoDelay            time.Duration
	CoinGeckoRetryMax         int
	HTTPPort                  string
	GoogleSheetsSpreadsheetID string
	GoogleCredentialsJSON     string
	GristAPIURL               string
	GristAPIKey               string
	GristDocID                string
	GristTableID              string
	GristChatID               int64
	GristTopicID              int64
	NotifyMentions            string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		HorizonURL:                envOrDefault("HORIZON_URL", "https://horizon.stellar.org"),
		DatabaseURL:               envOrDefaultWarn("DATABASE_URL", ""),
		CoinGeckoURL:              envOrDefault("COINGECKO_URL", "https://api.coingecko.com/api/v3"),
		StellarExpertURL:          envOrDefault("STELLAR_EXPERT_URL", "https://api.stellar.expert"),
		HorizonRetryMax:           envOrDefaultInt("HORIZON_RETRY_MAX", 5),
		HorizonRetryBaseDelay:     envOrDefaultDuration("HORIZON_RETRY_BASE_DELAY", 2*time.Second),
		CoinGeckoDelay:            envOrDefaultDuration("COINGECKO_DELAY", 6*time.Second),
		CoinGeckoRetryMax:         envOrDefaultInt("COINGECKO_RETRY_MAX", 5),
		HTTPPort:                  envOrDefault("HTTP_PORT", "8080"),
		GoogleSheetsSpreadsheetID: os.Getenv("GOOGLE_SHEETS_SPREADSHEET_ID"),
		GoogleCredentialsJSON:     os.Getenv("GOOGLE_CREDENTIALS_JSON"),
		GristAPIURL:               envOrDefault("GRIST_API_URL", "https://montelibero.getgrist.com"),
		GristAPIKey:               os.Getenv("GRIST_KEY"),
		GristDocID:                envOrDefault("GRIST_DOC_ID", "oNYTdHkEstf9X7dkh7yH11"),
		GristTableID:              envOrDefault("GRIST_TABLE_ID", "Messages"),
		GristChatID:               envOrDefaultInt64("GRIST_CHAT_ID", -1002871416798),
		GristTopicID:              envOrDefaultInt64("GRIST_TOPIC_ID", 0),
		NotifyMentions:            envOrDefault("NOTIFY_MENTIONS", "@xdefrag"),
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

func envOrDefaultInt64(key string, defaultVal int64) int64 {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			slog.Info("invalid int64 env var, using default", "key", key, "value", v, "default", defaultVal)
			return defaultVal
		}
		return n
	}
	return defaultVal
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
