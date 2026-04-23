package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/user/llm-proxy/internal/util"
)

type Config struct {
	Port        string
	DatabaseURL string
	MasterKey   string

	DataDir              string
	AllowHTTPProviders   bool
	RateLimitEnabled     bool
	MaxRequestSize       int64
	CORSOrigins          []string
	AllowedProviderHosts []string
}

// defaultKnownProviderHosts are always allowed as provider base_url hosts,
// regardless of the ALLOWED_PROVIDER_HOSTS env var. These correspond to the
// built-in provider types (OpenAI, Nano-GPT, Z.AI, DeepSeek, Ollama).
var defaultKnownProviderHosts = []string{
	"api.openai.com",
	"api.nano-gpt.com",
	"api.z.ai",
	"api.deepseek.com",
	"ollama.com",
}

func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	cfg := &Config{
		Port:        getEnvWithDefault("PORT", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL"),
		MasterKey:   getEnv("MASTER_KEY"),

		DataDir:              getEnvWithDefault("DATA_DIR", "./data"),
		AllowHTTPProviders:   getBoolEnvWithDefault("ALLOW_HTTP_PROVIDERS", false),
		RateLimitEnabled:     getBoolEnvWithDefault("RATE_LIMIT_ENABLED", true),
		MaxRequestSize:       getIntEnvWithDefault("MAX_REQUEST_SIZE", 10*1024*1024), // 10MB
		CORSOrigins:          parseCORSOrigins(getEnvWithDefault("CORS_ORIGINS", "http://localhost:5173,http://localhost:8081")),
		AllowedProviderHosts: parseProviderHosts(getEnvWithDefault("ALLOWED_PROVIDER_HOSTS", "")),
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

	var maskedURL string
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		maskedURL = "***"
	} else {
		if u.User != nil {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
		maskedURL = u.String()
	}

	return fmt.Sprintf(
		"Port: %s\nDatabaseURL: %s\nMasterKey: %s\nDataDir: %s\nAllowHTTPProviders: %t\nRateLimitEnabled: %t\nMaxRequestSize: %d\nCORSOrigins: %v",
		c.Port, maskedURL, maskedKey, c.DataDir, c.AllowHTTPProviders, c.RateLimitEnabled, c.MaxRequestSize, c.CORSOrigins,
	)
}

// ValidateProviderURL checks that a provider base_url is not a loopback address
// and (if AllowedProviderHosts is set) is in the allowed list.
// Built-in known provider hosts (OpenAI, Nano-GPT, Z.AI, DeepSeek, Ollama) are
// always allowed regardless of the ALLOWED_PROVIDER_HOSTS env var.
func (c *Config) ValidateProviderURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	// Always block loopback addresses
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("loopback addresses are not allowed as provider URLs")
	}

	// Resolve the host and check all IPs
	ips, err := net.LookupIP(host)
	if err == nil {
		for _, ip := range ips {
			if ip.IsLoopback() {
				return fmt.Errorf("loopback addresses are not allowed as provider URLs")
			}
		}
	}

	// Built-in known provider hosts are always allowed
	for _, knownHost := range defaultKnownProviderHosts {
		if strings.EqualFold(host, knownHost) {
			return nil
		}
	}

	// If AllowedProviderHosts is set, the host must be in the allowlist
	if len(c.AllowedProviderHosts) > 0 {
		allowed := false
		for _, allowedHost := range c.AllowedProviderHosts {
			if strings.EqualFold(host, allowedHost) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("provider host %q is not in ALLOWED_PROVIDER_HOSTS allowlist", host)
		}
	}

	return nil
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

func getIntEnvWithDefault(key string, defaultValue int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	var result int64
	if _, err := fmt.Sscanf(value, "%d", &result); err != nil {
		return defaultValue
	}
	return result
}

func parseCORSOrigins(value string) []string {
	result := util.SplitAndTrim(value)
	if result == nil {
		return []string{}
	}
	return result
}

func parseProviderHosts(value string) []string {
	return util.SplitAndTrim(value)
}
