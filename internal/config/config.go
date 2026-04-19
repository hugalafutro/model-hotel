package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	DatabaseURL        string
	MasterKey          string
	DiscoveryInterval  time.Duration
	DataDir            string
	AllowHTTPProviders bool
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	cfg := &Config{
		Port:               getEnvWithDefault("PORT", ":8080"),
		DatabaseURL:        getEnv("DATABASE_URL"),
		MasterKey:          getEnv("MASTER_KEY"),
		DiscoveryInterval:  parseDuration(getEnvWithDefault("DISCOVERY_INTERVAL", "30m")),
		DataDir:            getEnvWithDefault("DATA_DIR", "./data"),
		AllowHTTPProviders: getBoolEnvWithDefault("ALLOW_HTTP_PROVIDERS", false),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("MASTER_KEY is required")
	}

	return cfg, nil
}

func (c *Config) String() string {
	var maskedKey string
	if len(c.MasterKey) > 4 {
		maskedKey = "***" + c.MasterKey[len(c.MasterKey)-4:]
	} else {
		maskedKey = "***"
	}

	return fmt.Sprintf(
		"Port: %s\nDatabaseURL: %s\nMasterKey: %s\nDiscoveryInterval: %s\nDataDir: %s\nAllowHTTPProviders: %t",
		c.Port, c.DatabaseURL, maskedKey, c.DiscoveryInterval, c.DataDir, c.AllowHTTPProviders,
	)
}

func getEnv(key string) string {
	return os.Getenv(key)
}

func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getBoolEnvWithDefault(key string, defaultValue bool) bool {
	value := strings.ToLower(os.Getenv(key))
	if value == "true" || value == "1" || value == "yes" {
		return true
	}
	if value == "false" || value == "0" || value == "no" {
		return false
	}
	return defaultValue
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Minute
	}
	return d
}
