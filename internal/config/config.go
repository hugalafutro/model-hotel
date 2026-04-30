package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseURL string
	MasterKey   string

	DataDir              string
	AdminToken           string
	AllowHTTPProviders   bool
	RateLimitEnabled     bool
	RateLimitIPRPS       float64
	RateLimitIPBurst     int
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
	"api.anthropic.com",
	"ollama.com",
	"opencode.ai",
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
		AdminToken:           getEnv("ADMIN_TOKEN"),
		AllowHTTPProviders:   getBoolEnvWithDefault("ALLOW_HTTP_PROVIDERS", false),
		RateLimitEnabled:     getBoolEnvWithDefault("RATE_LIMIT_ENABLED", true),
		RateLimitIPRPS:       getFloatEnvWithDefault("RATE_LIMIT_IP_RPS", 30),
		RateLimitIPBurst:     int(getIntEnvWithDefault("RATE_LIMIT_IP_BURST", 60)),
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

	var adminTokenDisplay string
	if c.AdminToken != "" {
		adminTokenDisplay = "***set***"
	} else {
		adminTokenDisplay = "(auto-generated)"
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
		"Port: %s\nDatabaseURL: %s\nMasterKey: %s\nDataDir: %s\nAdminToken: %s\nAllowHTTPProviders: %t\nRateLimitEnabled: %t\nMaxRequestSize: %d\nCORSOrigins: %v",
		c.Port, maskedURL, maskedKey, c.DataDir, adminTokenDisplay, c.AllowHTTPProviders, c.RateLimitEnabled, c.MaxRequestSize, c.CORSOrigins,
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

	// Built-in known provider hosts are always allowed (skip loopback check)
	for _, knownHost := range defaultKnownProviderHosts {
		if strings.EqualFold(host, knownHost) {
			return nil
		}
	}

	// If AllowedProviderHosts is set, the host must be in the allowlist.
	// Hosts explicitly listed here bypass the loopback restriction so that
	// localhost can be used as a provider URL in test environments.
	if len(c.AllowedProviderHosts) > 0 {
		for _, allowedHost := range c.AllowedProviderHosts {
			if strings.EqualFold(host, allowedHost) {
				return nil
			}
		}
		return fmt.Errorf("provider host %q is not in ALLOWED_PROVIDER_HOSTS allowlist", host)
	}

	// Block loopback addresses when not in the allowlist
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("loopback addresses are not allowed as provider URLs (add to ALLOWED_PROVIDER_HOSTS to permit)")
	}

	// Resolve the host and check all IPs
	ips, err := net.LookupIP(host)
	if err == nil {
		for _, ip := range ips {
			if ip.IsLoopback() {
				return fmt.Errorf("loopback addresses are not allowed as provider URLs (add to ALLOWED_PROVIDER_HOSTS to permit)")
			}
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

func getFloatEnvWithDefault(key string, defaultValue float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	result, err := strconv.ParseFloat(value, 64)
	if err != nil {
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
