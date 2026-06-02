// Package config provides configuration loading and management from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// Config holds the application configuration.
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
	DBMaxConns           int32
	DBMinConns           int32
	ModelsDevEnabled     bool
	DebugLog             bool
	TrustedProxies       []*net.IPNet

	// WebAuthn/FIDO2 configuration. When WEBAUTHN_RP_ID is set, passkey
	// login is enabled; otherwise the feature is completely disabled.
	WebAuthnRPID          string
	WebAuthnRPDisplayName string
	WebAuthnRPOrigins     []string
}

// defaultKnownProviderHosts are always allowed as provider base_url hosts,
// regardless of the ALLOWED_PROVIDER_HOSTS env var. These correspond to the
// Known provider hosts used for ALLOWED_PROVIDER_HOSTS validation.
// Keep in sync with provider types detected by DetectProviderType in
// internal/provider/discovery.go. New providers added there should be
// listed here too. The canonical source is DetectProviderType's switch cases;
// this list is a flat extraction for config validation.
var defaultKnownProviderHosts = []string{
	"api.openai.com",
	"api.nano-gpt.com",
	"api.z.ai",
	"api.deepseek.com",
	"api.anthropic.com",
	"ollama.com",
	"opencode.ai",
	"api.x.ai",
	"generativelanguage.googleapis.com",
	"api.cohere.com",
	"api.cohere.ai",
	"openrouter.ai",
}

// KnownProviderHosts returns the built-in provider host allowlist.
func KnownProviderHosts() []string {
	return append([]string{}, defaultKnownProviderHosts...)
}

// Load reads configuration from environment variables and applies defaults.
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
		RateLimitIPRPS:       clampFloat(getFloatEnvWithDefault("RATE_LIMIT_IP_RPS", 30), 0, 10000),
		RateLimitIPBurst:     int(clampInt64(getIntEnvWithDefault("RATE_LIMIT_IP_BURST", 60), 1, 10000)),
		MaxRequestSize:       clampInt64(getIntEnvWithDefault("MAX_REQUEST_SIZE", 10*1024*1024), 1024, 100*1024*1024), // 1KB–100MB
		CORSOrigins:          parseCORSOrigins(getEnvWithDefault("CORS_ORIGINS", "http://localhost:5173,http://localhost:8081")),
		AllowedProviderHosts: parseProviderHosts(getEnvWithDefault("ALLOWED_PROVIDER_HOSTS", "")),
		//nolint:gosec // port value validated to be within uint16 range
		DBMaxConns: int32(clampInt64(getIntEnvWithDefault("DATABASE_MAX_CONNS", 25), 1, 1000)),
		//nolint:gosec // port value validated to be within uint16 range
		DBMinConns:       int32(clampInt64(getIntEnvWithDefault("DATABASE_MIN_CONNS", 5), 1, 1000)),
		ModelsDevEnabled: getBoolEnvWithDefault("MODELSDEV_ENABLED", true),
		DebugLog:         getBoolEnvWithDefault("DEBUG_LOG", false),
		TrustedProxies:   LoadTrustedProxies(),

		WebAuthnRPID:          getEnv("WEBAUTHN_RP_ID"),
		WebAuthnRPDisplayName: getEnvWithDefault("WEBAUTHN_RP_DISPLAY_NAME", "Model Hotel"),
		WebAuthnRPOrigins:     parseCORSOrigins(getEnv("WEBAUTHN_RP_ORIGINS")),
	}

	// If DATABASE_URL is not set, construct it from POSTGRES_* components.
	// This eliminates duplication: the password only needs to be set once.
	if cfg.DatabaseURL == "" {
		pgUser := getEnvWithDefault("POSTGRES_USER", "modelhotel")
		pgPass := getEnv("POSTGRES_PASSWORD")
		pgHost := getEnvWithDefault("POSTGRES_HOST", "db")
		pgDB := getEnvWithDefault("POSTGRES_DB", "modelhotel")
		if pgPass == "" {
			return nil, fmt.Errorf("DATABASE_URL or POSTGRES_PASSWORD is required")
		}
		cfg.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:5432/%s", pgUser, pgPass, pgHost, pgDB)
	}

	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("MASTER_KEY is required")
	}

	return cfg, nil
}

type configRow struct{ label, value string }

func (c *Config) String() string {
	var adminTokenDisplay string
	if c.AdminToken != "" {
		adminTokenDisplay = "***set***"
	} else {
		adminTokenDisplay = "(auto-generated)"
	}

	// Build label-value rows.
	// Database URL and Master Key are omitted: a technical user can find
	// them in .env or docker-compose.yml, a layman user does not need them.
	rows := []configRow{
		{"Port", c.Port},
		{"Data Dir", c.DataDir},
		{"Admin Token", adminTokenDisplay},
		{"HTTP Providers", fmt.Sprintf("%t", c.AllowHTTPProviders)},
		{"Rate Limiting", fmt.Sprintf("%t", c.RateLimitEnabled)},
		{"Max Request Size", formatBytes(c.MaxRequestSize)},
		{"Debug Log", fmt.Sprintf("%t", c.DebugLog)},
	}

	// Calculate label column width (include "CORS Origins" to avoid
	// misalignment if other labels are shorter)
	labelW := len("CORS Origins")
	for _, r := range rows {
		if len(r.label) > labelW {
			labelW = len(r.label)
		}
	}

	// Max value width that fits within a reasonable frame
	const maxFrameW = 80
	const indent = "   "
	const gap = "  "
	maxValW := maxFrameW - len(indent) - labelW - len(gap)

	// Add CORS origins as multi-line rows
	corsRows := formatCORSOriginRows(c.CORSOrigins, labelW, len(indent), len(gap))
	rows = append(rows, corsRows...)

	// Build content lines, truncating values that exceed maxValW
	contentLines := []string{
		indent + "Starting Model Hotel",
		"",
	}
	for _, r := range rows {
		val := r.value
		if len(val) > maxValW {
			val = val[:maxValW-3] + "..."
		}
		contentLines = append(contentLines, indent+padRight(r.label, labelW)+gap+val)
	}
	contentLines = append(contentLines, "")

	// Calculate content width, capped at maxFrameW
	contentW := 0
	for _, l := range contentLines {
		if len(l) > contentW {
			contentW = len(l)
		}
	}
	contentW += len(indent) // right margin matches left indent
	if contentW > maxFrameW {
		contentW = maxFrameW
	}

	// Build double-line frame
	var sb strings.Builder
	border := strings.Repeat("═", contentW)
	sb.WriteString("╔" + border + "╗\n")
	for _, l := range contentLines {
		sb.WriteString("║" + padRight(l, contentW) + "║\n")
	}
	sb.WriteString("╚" + border + "╝\n")

	return sb.String()
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func formatBytes(b int64) string {
	const (
		KB int64 = 1024
		MB int64 = KB * 1024
		GB int64 = MB * 1024
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%d GB", b/GB)
	case b >= MB:
		return fmt.Sprintf("%d MB", b/MB)
	case b >= KB:
		return fmt.Sprintf("%d KB", b/KB)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatCORSOriginRows(origins []string, labelW, indentLen, gapLen int) []configRow {
	if len(origins) == 0 {
		return []configRow{{"CORS Origins", "(none)"}}
	}

	// Calculate max value width from the same logic used for other rows.
	const maxFrameW = 80
	maxValW := maxFrameW - indentLen - labelW - gapLen

	// First origin gets the "CORS Origins" label; others use a blank label
	// so the padRight(label, labelW) + gap alignment keeps values stacked.
	result := make([]configRow, 0, len(origins))
	for i, o := range origins {
		v := o
		if len(v) > maxValW {
			v = v[:maxValW-3] + "..."
		}
		if i == 0 {
			result = append(result, configRow{"CORS Origins", v})
		} else {
			result = append(result, configRow{"", v})
		}
	}
	return result
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

	// Resolve the host and check all IPs.
	// Store the resolved IPs to detect DNS rebinding: if the host later resolves
	// to a different set of IPs, the provider URL should be re-validated.
	// For now, we block any host that currently resolves to a loopback address.
	ips, err := net.LookupIP(host)
	if err == nil {
		for _, ip := range ips {
			if ip.IsLoopback() {
				return fmt.Errorf("host %q resolves to loopback address %s — not allowed as provider URL (add to ALLOWED_PROVIDER_HOSTS to permit)", host, ip)
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

	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
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
	// Reject "*" wildcard — it is incompatible with credentials=true (CORS spec
	// forbids it) and would silently break auth. Force users to list explicit origins.
	for i, o := range result {
		if o == "*" {
			debuglog.Warn("CORS_ORIGINS contains '*' wildcard, which is incompatible with credentials=true; removing it")
			result = append(result[:i], result[i+1:]...)
			return parseCORSOrigins(strings.Join(result, ","))
		}
	}
	return result
}

func parseProviderHosts(value string) []string {
	return util.SplitAndTrim(value)
}

func clampInt64(value, minVal, maxVal int64) int64 {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

func clampFloat(value, minVal, maxVal float64) float64 {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
