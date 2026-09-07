// Package config provides configuration loading and management from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/joho/godotenv"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// RecommendedMasterKeyLength is the minimum at-rest master key length we
// consider strong enough for the low-cost Argon2id parameters. It matches the
// output of the documented generator `openssl rand -base64 32` (44 chars),
// rounded down. Shared with Front Desk, whose FRONTDESK_MASTER_KEY protects
// member admin tokens and the TOTP secret with the same KDF.
const RecommendedMasterKeyLength = 32

// WeakMasterKey reports whether key is shorter than RecommendedMasterKeyLength.
// Callers warn rather than fail: rotating a master key invalidates everything
// encrypted under it, so an existing deployment must keep booting.
func WeakMasterKey(key string) bool {
	return len(key) < RecommendedMasterKeyLength
}

// Config holds the application configuration.
type Config struct {
	Port        string
	DatabaseURL string
	MasterKey   string

	DataDir              string
	AdminToken           string
	MetricsToken         string
	AllowHTTPProviders   bool
	AllowEmbed           bool
	RateLimitEnabled     bool
	RateLimitIPRPS       float64
	RateLimitIPBurst     int
	MaxRequestSize       int64
	CORSOrigins          []string
	AllowedProviderHosts []string
	DBMaxConns           int32
	DBMinConns           int32
	ModelsDevEnabled     bool
	DemoReadOnly         bool
	DemoShowToken        bool
	DebugLog             bool
	TrustedProxies       []*net.IPNet
	KnownProxies         []*net.IPNet

	// CookieSecure controls the Secure attribute on dashboard auth cookies.
	// "always" (default) forces Secure on so the session cookie is never sent
	// over cleartext HTTP; "auto" sets Secure from the request scheme (TLS or
	// X-Forwarded-Proto=https); "never" forces Secure off for a plain-HTTP LAN
	// deployment (the operator's explicit, accepted-risk opt-out). localhost is a
	// browser secure context, so "always" still works in local HTTP dev.
	CookieSecure string

	// Breached-password checking. When enabled (the default), new dashboard
	// passwords are checked against the Have I Been Pwned Pwned Passwords range
	// API using k-anonymity (only a 5-char SHA-1 prefix is sent). The check
	// fails open, so an unreachable endpoint never blocks a password change.
	// PwnedPasswordAPIURL points the check at a self-hosted mirror instead of
	// the public service for offline or egress-restricted deployments.
	PwnedPasswordCheckEnabled bool
	PwnedPasswordAPIURL       string

	// WebAuthn/FIDO2 configuration. When WEBAUTHN_RP_ID is set, passkey
	// login is enabled; otherwise the feature is completely disabled.
	WebAuthnRPID          string
	WebAuthnRPDisplayName string
	WebAuthnRPOrigins     []string

	// lookupIP is used for DNS resolution in ValidateProviderURL.
	// Defaults to net.LookupIP if nil. Used for testing.
	lookupIP func(host string) ([]net.IP, error)
}

// defaultKnownProviderHosts are always allowed as provider base_url hosts,
// regardless of the ALLOWED_PROVIDER_HOSTS env var. These correspond to the
// Known provider hosts used for ALLOWED_PROVIDER_HOSTS validation.
// Keep in sync with the hosts recognised in detectByHost
// (internal/provider/discovery.go): a new provider family added there should
// be listed here too. That switch stays the canonical source; this list is a
// flat extraction for config validation.
var defaultKnownProviderHosts = []string{
	"api.openai.com",
	"api.nano-gpt.com",
	"api.z.ai",
	"api.kimi.com",
	"kimi.com",
	"api.minimax.io",
	"minimax.io",
	"api.deepseek.com",
	"api.anthropic.com",
	"ollama.com",
	"opencode.ai",
	"api.x.ai",
	"generativelanguage.googleapis.com",
	"aiplatform.googleapis.com",
	"api.cohere.com",
	"api.cohere.ai",
	"openrouter.ai",
	"api.neuralwatt.com",
	"neuralwatt.com",
}

// defaultKnownProviderHostSuffixes accept any subdomain of a known provider
// domain. Keep in sync with the suffix rules in hostTypeRules
// (internal/provider/discovery.go): every domain whose subdomains detect as a
// first-class provider type must be creatable without an explicit
// ALLOWED_PROVIDER_HOSTS entry, or restricted installs reject hosts the
// detector advertises. Runtime dialing still enforces the private/reserved-IP
// checks (SafeDialer), so this does not widen SSRF exposure.
var defaultKnownProviderHostSuffixes = []string{
	".nano-gpt.com",
	".z.ai",
	".kimi.com",
	".minimax.io",
	".deepseek.com",
	".anthropic.com",
	".ollama.com",
	".opencode.ai",
	".x.ai",
	".cohere.com",
	".cohere.ai",
	".openrouter.ai",
	".neuralwatt.com",
}

// KnownProviderHosts returns the built-in provider host allowlist.
func KnownProviderHosts() []string {
	return slices.Clone(defaultKnownProviderHosts)
}

// LoadEnvFile loads the optional .env file into the process environment
// (existing variables win, so container `environment:` settings are never
// overridden). Load calls it, but the server calls it first thing so the
// logger can be initialised from .env-provided DEBUG_LOG / LOG_FORMAT before
// Load runs and starts logging. A missing file is not an error.
func LoadEnvFile() error {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error loading .env file: %w", err)
	}
	return nil
}

// Load reads configuration from environment variables and applies defaults.
func Load() (*Config, error) {
	if err := LoadEnvFile(); err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:        EnvOr("PORT", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		MasterKey:   os.Getenv("MASTER_KEY"),

		DataDir:              EnvOr("DATA_DIR", "./data"),
		AdminToken:           os.Getenv("ADMIN_TOKEN"),
		MetricsToken:         os.Getenv("METRICS_TOKEN"),
		AllowHTTPProviders:   BoolEnv("ALLOW_HTTP_PROVIDERS", false),
		AllowEmbed:           BoolEnv("ALLOW_EMBED", false),
		RateLimitEnabled:     BoolEnv("RATE_LIMIT_ENABLED", true),
		RateLimitIPRPS:       min(max(envNumber("RATE_LIMIT_IP_RPS", 30.0, parseFloat), 0), 10000),
		RateLimitIPBurst:     min(max(envNumber("RATE_LIMIT_IP_BURST", 60, strconv.Atoi), 1), 10000),
		MaxRequestSize:       min(max(envNumber("MAX_REQUEST_SIZE", int64(50*1024*1024), parseInt64), 1024), 100*1024*1024), // 1KB–100MB; default 50MB covers multipart audio uploads (OpenAI limit: 25MB); also sizes the body read budget (httpx.NewServer), so a larger ceiling lengthens the longest hold a hostile body can buy
		CORSOrigins:          parseCORSOrigins(EnvOr("CORS_ORIGINS", "http://localhost:5173,http://localhost:8081")),
		AllowedProviderHosts: util.SplitAndTrim(os.Getenv("ALLOWED_PROVIDER_HOSTS")),
		DBMaxConns:           min(max(envNumber("DATABASE_MAX_CONNS", int32(25), parseInt32), 1), 1000),
		DBMinConns:           min(max(envNumber("DATABASE_MIN_CONNS", int32(5), parseInt32), 1), 1000),
		ModelsDevEnabled:     BoolEnv("MODELSDEV_ENABLED", true),
		DemoReadOnly:         BoolEnv("DEMO_READONLY", false),
		DemoShowToken:        BoolEnv("DEMO_SHOW_TOKEN", false),
		DebugLog:             BoolEnv("DEBUG_LOG", false),
		TrustedProxies:       LoadTrustedProxies(),
		KnownProxies:         LoadKnownProxies(),

		PwnedPasswordCheckEnabled: BoolEnv("PWNED_PASSWORD_CHECK_ENABLED", true),
		PwnedPasswordAPIURL:       EnvOr("PWNED_PASSWORD_API_URL", "https://api.pwnedpasswords.com"),

		WebAuthnRPID:          os.Getenv("WEBAUTHN_RP_ID"),
		WebAuthnRPDisplayName: EnvOr("WEBAUTHN_RP_DISPLAY_NAME", "Model Hotel"),
		WebAuthnRPOrigins:     parseCORSOrigins(os.Getenv("WEBAUTHN_RP_ORIGINS")),

		CookieSecure: NormalizeCookieSecure(os.Getenv("COOKIE_SECURE")),
	}

	// If DATABASE_URL is not set, construct it from POSTGRES_* components.
	// This eliminates duplication: the password only needs to be set once.
	if cfg.DatabaseURL == "" {
		pgUser := EnvOr("POSTGRES_USER", "modelhotel")
		pgPass := os.Getenv("POSTGRES_PASSWORD")
		pgHost := EnvOr("POSTGRES_HOST", "db")
		pgDB := EnvOr("POSTGRES_DB", "modelhotel")
		if pgPass == "" {
			return nil, fmt.Errorf("DATABASE_URL or POSTGRES_PASSWORD is required")
		}
		cfg.DatabaseURL = fmt.Sprintf("postgres://%s:%s@%s:5432/%s", pgUser, pgPass, pgHost, pgDB)
	}

	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("MASTER_KEY is required")
	}

	// The at-rest encryption KDF (Argon2id) uses deliberately low cost
	// parameters on the assumption that MASTER_KEY is a high-entropy random
	// value rather than a memorable passphrase (see internal/auth/encryption.go).
	// Warn — but do not fail — when the key is shorter than the documented
	// generator (`openssl rand -base64 32` → 44 chars) so existing deployments
	// keep booting (rotating MASTER_KEY would invalidate all encrypted keys),
	// while operators are nudged toward a stronger value.
	if WeakMasterKey(cfg.MasterKey) {
		debuglog.Warn("config: MASTER_KEY is shorter than recommended — a low-entropy key weakens at-rest encryption of provider credentials; generate a strong one with `openssl rand -base64 32`",
			"length", len(cfg.MasterKey), "recommended_min", RecommendedMasterKeyLength)
	}

	// DEMO_SHOW_TOKEN publishes the admin token on the login screen, which is
	// only acceptable when every admin mutation is already blocked. Refuse to
	// expose it unless DEMO_READONLY is also on; warn so the operator knows the
	// flag is inert rather than silently ignored.
	if cfg.DemoShowToken && !cfg.DemoReadOnly {
		debuglog.Warn("config: DEMO_SHOW_TOKEN is set but DEMO_READONLY is not — the admin token will NOT be shown on the login screen; enable DEMO_READONLY to use this demo feature")
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

	// Log-export status, shown as booleans only — never the METRICS_TOKEN value
	// (mirrors how Admin Token is masked and Master Key is omitted). The env
	// checks must stay in sync with debuglog.JSONFormat() and
	// otelexport.LogsEnabled(); config reads env directly rather than importing
	// those packages so foundational config stays free of the OTel SDK.
	logFormat := "text"
	if strings.EqualFold(strings.TrimSpace(os.Getenv("LOG_FORMAT")), "json") {
		logFormat = "json"
	}
	otlpLogs := "disabled"
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != "" {
		otlpLogs = "enabled"
	}
	metrics := "disabled"
	if c.MetricsToken != "" {
		metrics = "enabled"
	}

	// Build label-value rows.
	// Database URL and Master Key are omitted: a technical user can find
	// them in .env or docker-compose.yml, a layman user does not need them.
	rows := []configRow{
		{"Port", c.Port},
		{"Data Dir", c.DataDir},
		{"Admin Token", adminTokenDisplay},
		{"HTTP Providers", fmt.Sprintf("%t", c.AllowHTTPProviders)},
		{"Allow Embed", fmt.Sprintf("%t", c.AllowEmbed)},
		{"Rate Limiting", fmt.Sprintf("%t", c.RateLimitEnabled)},
		{"Breached-PW Check", fmt.Sprintf("%t", c.PwnedPasswordCheckEnabled)},
		{"Max Request Size", util.FormatBytes(c.MaxRequestSize)},
		{"Debug Log", fmt.Sprintf("%t", c.DebugLog)},
		{"Log Format", logFormat},
		{"Metrics", metrics},
		{"OTLP Logs", otlpLogs},
	}

	// Only surfaced when enabled — it is a niche demo-hardening flag, so a
	// permanent "Read-Only Mode: false" row would be noise for normal deploys.
	if c.DemoReadOnly {
		rows = append(rows, configRow{"Read-Only Mode", "true"})
	}

	// Calculate label column width (include "CORS Origins" to avoid
	// misalignment if other labels are shorter)
	labelW := len("CORS Origins")
	for _, r := range rows {
		labelW = max(labelW, len(r.label))
	}

	// Max value width that fits within a reasonable frame
	const indent = "   "
	const gap = "  "
	maxValW := maxFrameW - len(indent) - labelW - len(gap)

	// Add CORS origins as multi-line rows
	rows = append(rows, formatCORSOriginRows(c.CORSOrigins)...)

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
		contentW = max(contentW, len(l))
	}
	contentW = min(contentW+len(indent), maxFrameW) // right margin matches left indent

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

// maxFrameW is the widest the startup banner is allowed to be, in characters.
const maxFrameW = 80

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// formatCORSOriginRows renders one row per origin. The first carries the
// "CORS Origins" label and the rest a blank one, so the padRight(label,
// labelW) + gap alignment keeps the values stacked. Truncation is String's
// job, which applies the same cut to every row it prints.
func formatCORSOriginRows(origins []string) []configRow {
	if len(origins) == 0 {
		return []configRow{{"CORS Origins", "(none)"}}
	}
	result := make([]configRow, 0, len(origins))
	for i, o := range origins {
		label := ""
		if i == 0 {
			label = "CORS Origins"
		}
		result = append(result, configRow{label, o})
	}
	return result
}

// ValidateProviderURL checks that a provider base_url does not resolve to a
// private or reserved address (loopback, RFC 1918/ULA, link-local, CGNAT, or
// cloud-metadata — see util.IsBlockedIP) and, if AllowedProviderHosts is set,
// is in the allowed list. Built-in known provider hosts (OpenAI, Nano-GPT,
// Z.AI, DeepSeek, Ollama) are always allowed regardless of the
// ALLOWED_PROVIDER_HOSTS env var.
func (c *Config) ValidateProviderURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	// Built-in known provider hosts are always allowed (skip the IP checks)
	for _, knownHost := range defaultKnownProviderHosts {
		if strings.EqualFold(host, knownHost) {
			return nil
		}
	}
	lowerHost := strings.ToLower(host)
	if slices.ContainsFunc(defaultKnownProviderHostSuffixes, func(suffix string) bool {
		return strings.HasSuffix(lowerHost, suffix)
	}) {
		return nil
	}

	// If AllowedProviderHosts is set, the host must be in the allowlist.
	// Hosts explicitly listed here bypass the private/reserved-IP checks so that
	// internal LLM servers (or localhost in tests) can be used as provider URLs.
	if len(c.AllowedProviderHosts) > 0 {
		if slices.ContainsFunc(c.AllowedProviderHosts, func(allowed string) bool {
			return strings.EqualFold(host, allowed)
		}) {
			return nil
		}
		return fmt.Errorf("provider host %q is not in ALLOWED_PROVIDER_HOSTS allowlist", host)
	}

	// Block loopback addresses when not in the allowlist
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("loopback addresses are not allowed as provider URLs (add to ALLOWED_PROVIDER_HOSTS to permit)")
	}

	// Resolve the host and block any private/reserved address. This mirrors
	// the runtime SafeDialer (util.IsBlockedIP) so a base_url accepted here
	// cannot later be silently refused at dial time, and closes the
	// creation-time SSRF gap (e.g. http://10.0.0.1, http://169.254.169.254).
	// Hosts that legitimately point at internal infrastructure must be added
	// to ALLOWED_PROVIDER_HOSTS, which bypasses this check above.
	lookupIP := c.lookupIP
	if lookupIP == nil {
		lookupIP = net.LookupIP
	}
	ips, err := lookupIP(host)
	if err == nil {
		if i := slices.IndexFunc(ips, util.IsBlockedIP); i >= 0 {
			return fmt.Errorf("host %q resolves to private/reserved address %s: not allowed as provider URL (add to ALLOWED_PROVIDER_HOSTS to permit)", host, ips[i])
		}
	}

	return nil
}

// EnvOr reads an environment variable, falling back to defaultValue when it is
// unset or empty. Exported so the Front Desk binary, which reads its own
// environment rather than this Config, resolves its knobs identically.
func EnvOr(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// NormalizeCookieSecure validates COOKIE_SECURE against its allowed values
// ("always", "auto", "never"), falling back to "always" for unset or
// unrecognized input. Exported so the Front Desk binary, which reads its own
// environment rather than this Config, resolves the knob identically.
func NormalizeCookieSecure(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "always":
		return "always"
	case "auto":
		return "auto"
	case "never":
		return "never"
	default:
		// Secure-by-default: unset or unrecognized values force Secure on so a
		// misconfiguration cannot silently ship the session cookie over cleartext.
		return "always"
	}
}

// BoolEnv reads a boolean environment variable, warning once and returning
// defaultValue for anything outside the truthy/falsy set. Exported alongside
// EnvOr so every binary reads the same spellings.
func BoolEnv(key string, defaultValue bool) bool {
	raw := os.Getenv(key)
	if value, ok := debuglog.EnvBool(raw); ok {
		return value
	}
	if raw != "" {
		debuglog.Warn("config: ignoring unrecognized boolean env value, using default",
			"key", key, "value", raw, "default", defaultValue)
	}
	return defaultValue
}

// envNumber reads a numeric environment variable through parse, warning once
// and returning def when the value is unparseable. The warn text says
// "integer" for every integer width and "float" for float64, which is what
// parseLabel resolves.
func envNumber[T int | int32 | int64 | float64](key string, def T, parse func(string) (T, error)) T {
	value := os.Getenv(key)
	if value == "" {
		return def
	}
	result, err := parse(value)
	if err != nil {
		kind := "integer"
		if _, isFloat := any(def).(float64); isFloat {
			kind = "float"
		}
		debuglog.Warn("config: ignoring invalid "+kind+" env value, using default",
			"key", key, "value", value, "default", def)
		return def
	}
	return result
}

// parseInt64, parseInt32 and parseFloat adapt strconv to envNumber's signature.
func parseInt64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

func parseInt32(s string) (int32, error) {
	v, err := strconv.ParseInt(s, 10, 32)
	return int32(v), err
}

func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

func parseCORSOrigins(value string) []string {
	result := util.SplitAndTrim(value)
	if result == nil {
		return []string{}
	}
	// Reject "*" wildcard — it is incompatible with credentials=true (CORS spec
	// forbids it) and would silently break auth. Force users to list explicit origins.
	if slices.Contains(result, "*") {
		debuglog.Warn("CORS_ORIGINS contains '*' wildcard, which is incompatible with credentials=true; removing it")
		result = slices.DeleteFunc(result, func(o string) bool { return o == "*" })
	}
	return result
}
