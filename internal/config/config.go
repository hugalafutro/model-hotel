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
	DBMaxConns           int32
	DBMinConns           int32
	ModelsDevEnabled     bool
}

// defaultKnownProviderHosts are always allowed as provider base_url hosts,
// regardless of the ALLOWED_PROVIDER_HOSTS env var. These correspond to the
// built-in provider types (OpenAI, Nano-GPT, Z.AI Coding Plan, DeepSeek, Ollama).
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
		DBMaxConns:           int32(clampInt64(getIntEnvWithDefault("DATABASE_MAX_CONNS", 25), 1, 1000)),
		DBMinConns:           int32(clampInt64(getIntEnvWithDefault("DATABASE_MIN_CONNS", 5), 1, 1000)),
		ModelsDevEnabled:     getBoolEnvWithDefault("MODELSDEV_ENABLED", true),
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
			u.User = nil
			maskedURL = fmt.Sprintf("%s://***@%s%s", u.Scheme, u.Host, u.Path)
		} else {
			maskedURL = u.String()
		}
	}

	// Build label-value rows
	type row struct{ label, value string }
	rows := []row{
		{"Port", c.Port},
		{"Database URL", maskedURL},
		{"Master Key", maskedKey},
		{"Data Dir", c.DataDir},
		{"Admin Token", adminTokenDisplay},
		{"HTTP Providers", fmt.Sprintf("%t", c.AllowHTTPProviders)},
		{"Rate Limiting", fmt.Sprintf("%t", c.RateLimitEnabled)},
		{"Max Request Size", formatBytes(c.MaxRequestSize)},
	}

	// Calculate label column width
	labelW := 0
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

	// Add CORS origins with truncation for long lists
	rows = append(rows, row{"CORS Origins", formatCORSOrigins(c.CORSOrigins, maxValW)})

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
	contentW++ // ensure at least 1 space of right margin
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
	sb.WriteString("╚" + border + "╝")

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

func formatCORSOrigins(origins []string, maxLen int) string {
	if len(origins) == 0 {
		return "(none)"
	}

	all := strings.Join(origins, ", ")
	if len(all) <= maxLen {
		return all
	}

	// Show as many as fit with "... and N more" suffix
	for keep := len(origins) - 1; keep >= 1; keep-- {
		partial := strings.Join(origins[:keep], ", ")
		suffix := fmt.Sprintf(", ... and %d more", len(origins)-keep)
		if len(partial)+len(suffix) <= maxLen {
			return partial + suffix
		}
	}

	return fmt.Sprintf("%d origins configured", len(origins))
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

func clampInt64(value, min, max int64) int64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
