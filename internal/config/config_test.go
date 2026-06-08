package config

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidateProviderURL
// ---------------------------------------------------------------------------

func TestValidateProviderURL_BlockLocalhost(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("http://localhost:3000/v1")
	if err == nil {
		t.Error("expected error for localhost URL, got nil")
	}
}

func TestValidateProviderURL_BlockLocalhostHTTPS(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("https://localhost:3000/v1")
	if err == nil {
		t.Error("expected error for localhost URL over HTTPS, got nil")
	}
}

func TestValidateProviderURL_Block127001(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("http://127.0.0.1:8080/v1")
	if err == nil {
		t.Error("expected error for 127.0.0.1 URL, got nil")
	}
}

func TestValidateProviderURL_Block127001HTTPS(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("https://127.0.0.1:8080/v1")
	if err == nil {
		t.Error("expected error for 127.0.0.1 URL over HTTPS, got nil")
	}
}

func TestValidateProviderURL_BlockIPv6Loopback(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("http://[::1]:8080/v1")
	if err == nil {
		t.Error("expected error for ::1 loopback URL, got nil")
	}
}

func TestValidateProviderURL_AllowKnownProvider(t *testing.T) {
	cfg := &Config{}
	tests := []struct {
		name string
		url  string
	}{
		{"OpenAI", "https://api.openai.com/v1"},
		{"Anthropic", "https://api.anthropic.com/v1"},
		{"DeepSeek", "https://api.deepseek.com/v1"},
		{"NanoGPT", "https://api.nano-gpt.com/v1"},
		{"ZAI", "https://api.z.ai/v1"},
		{"Ollama", "https://ollama.com/api"},
		{"OpenCode", "https://opencode.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := cfg.ValidateProviderURL(tc.url)
			if err != nil {
				t.Errorf("ValidateProviderURL(%q) returned unexpected error: %v", tc.url, err)
			}
		})
	}
}

func TestValidateProviderURL_AllowKnownProviderSubdomain(t *testing.T) {
	// Use mock DNS to avoid real lookups (which take ~2s per non-existent domain)
	cfg := &Config{
		lookupIP: func(host string) ([]net.IP, error) {
			// Return a public IP to simulate successful resolution
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		},
	}
	tests := []struct {
		name string
		url  string
	}{
		{"OpenAI subdomain", "https://custom.api.openai.com/v1"},
		{"Anthropic subdomain", "https://custom.api.anthropic.com/v1"},
		{"DeepSeek subdomain", "https://proxy.api.deepseek.com/v1"},
		{"NanoGPT subdomain", "https://custom.nano-gpt.com/v1"},
		{"ZAI subdomain", "https://custom.z.ai/v1"},
		{"Ollama subdomain", "https://custom.ollama.com/v1"},
		{"OpenCode subdomain", "https://custom.opencode.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := cfg.ValidateProviderURL(tc.url)
			if err != nil {
				t.Errorf("ValidateProviderURL(%q) returned unexpected error: %v", tc.url, err)
			}
		})
	}
}

func TestValidateProviderURL_BlockUnknownWithoutAllowList(t *testing.T) {
	// When AllowedProviderHosts is empty and the host is not a known provider,
	// it should be allowed (empty allowlist means allow all)
	cfg := &Config{AllowedProviderHosts: nil}
	err := cfg.ValidateProviderURL("https://custom-llm.example.com/v1")
	if err != nil {
		t.Errorf("with empty AllowedProviderHosts, unknown hosts should be allowed, got error: %v", err)
	}
}

func TestValidateProviderURL_AllowListAllowsMatch(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"custom-llm.example.com", "another.host.com"}}
	err := cfg.ValidateProviderURL("https://custom-llm.example.com/v1")
	if err != nil {
		t.Errorf("ValidateProviderURL should allow host in allowlist, got error: %v", err)
	}
}

func TestValidateProviderURL_AllowListBlocksMismatch(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"custom-llm.example.com"}}
	err := cfg.ValidateProviderURL("https://evil.example.com/v1")
	if err == nil {
		t.Error("ValidateProviderURL should block host not in allowlist")
	}
}

func TestValidateProviderURL_AllowListDoesNotBlockKnownProviders(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"custom-llm.example.com"}}
	// Known providers are always allowed, even if not in the allowlist
	err := cfg.ValidateProviderURL("https://api.openai.com/v1")
	if err != nil {
		t.Errorf("known provider should always be allowed, got error: %v", err)
	}
}

func TestValidateProviderURL_AllowListCaseInsensitive(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"Custom-LLM.Example.COM"}}
	err := cfg.ValidateProviderURL("https://custom-llm.example.com/v1")
	if err != nil {
		t.Errorf("allowlist matching should be case-insensitive, got error: %v", err)
	}
}

func TestValidateProviderURL_InvalidURL(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("://invalid")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestValidateProviderURL_EmptyHost(t *testing.T) {
	cfg := &Config{}
	err := cfg.ValidateProviderURL("http:///v1")
	if err == nil {
		t.Error("expected error for URL with empty host, got nil")
	}
}

func TestValidateProviderURL_LocalhostAllowedWhenInAllowList(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"localhost"}}
	// Loopback hosts explicitly listed in ALLOWED_PROVIDER_HOSTS bypass
	// the loopback restriction so that localhost can be used as a provider
	// URL in test environments.
	err := cfg.ValidateProviderURL("https://localhost:3000/v1")
	if err != nil {
		t.Errorf("localhost should be allowed when in allowlist, got error: %v", err)
	}
}

func TestValidateProviderURL_127001AllowedWhenInAllowList(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"127.0.0.1"}}
	err := cfg.ValidateProviderURL("https://127.0.0.1:3000/v1")
	if err != nil {
		t.Errorf("127.0.0.1 should be allowed when in allowlist, got error: %v", err)
	}
}

func TestValidateProviderURL_LocalhostBlockedWithoutAllowList(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: nil}
	err := cfg.ValidateProviderURL("https://localhost:3000/v1")
	if err == nil {
		t.Error("localhost should be blocked when not in allowlist")
	}
}

func TestValidateProviderURL_127001BlockedWithoutAllowList(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: nil}
	err := cfg.ValidateProviderURL("https://127.0.0.1:3000/v1")
	if err == nil {
		t.Error("127.0.0.1 should be blocked when not in allowlist")
	}
}

func TestValidateProviderURL_IPv6LoopbackAllowedWhenInAllowList(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"::1"}}
	err := cfg.ValidateProviderURL("http://[::1]:8080/v1")
	if err != nil {
		t.Errorf("::1 should be allowed when in allowlist, got error: %v", err)
	}
}

func TestValidateProviderURL_IPv6LoopbackBlockedWithoutAllowList(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: nil}
	err := cfg.ValidateProviderURL("http://[::1]:8080/v1")
	if err == nil {
		t.Error("::1 should be blocked when not in allowlist")
	}
}

// ---------------------------------------------------------------------------
// Config.Load (environment-based)
// ---------------------------------------------------------------------------

func TestLoad_RequiredDatabaseURL(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("POSTGRES_PASSWORD")
	os.Unsetenv("MASTER_KEY")

	_, err := Load()
	if err == nil {
		t.Error("expected error when DATABASE_URL and POSTGRES_PASSWORD are missing, got nil")
	}
}

func TestLoad_RequiredMasterKey(t *testing.T) {
	os.Unsetenv("MASTER_KEY")
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	defer os.Unsetenv("DATABASE_URL")

	_, err := Load()
	if err == nil {
		t.Error("expected error when MASTER_KEY is missing, got nil")
	}
}

func TestLoad_ConstructsDatabaseURL(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	os.Setenv("POSTGRES_USER", "myuser")
	os.Setenv("POSTGRES_PASSWORD", "mypass")
	os.Setenv("POSTGRES_HOST", "myhost")
	os.Setenv("POSTGRES_DB", "mydb")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")
	defer os.Unsetenv("POSTGRES_USER")
	defer os.Unsetenv("POSTGRES_PASSWORD")
	defer os.Unsetenv("POSTGRES_HOST")
	defer os.Unsetenv("POSTGRES_DB")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	expected := "postgres://myuser:mypass@myhost:5432/mydb"
	if cfg.DatabaseURL != expected {
		t.Errorf("expected constructed DATABASE_URL %q, got %q", expected, cfg.DatabaseURL)
	}
}

func TestLoad_DatabaseURLOverride(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://override:pass@custom:5433/db")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	os.Setenv("POSTGRES_PASSWORD", "ignored")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")
	defer os.Unsetenv("POSTGRES_PASSWORD")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.DatabaseURL != "postgres://override:pass@custom:5433/db" {
		t.Errorf("DATABASE_URL should take precedence over POSTGRES_* components, got %q", cfg.DatabaseURL)
	}
}

func TestLoad_SuccessWithDefaults(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Port != ":8080" {
		t.Errorf("expected default port :8080, got %q", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost/test" {
		t.Errorf("expected DATABASE_URL to be set, got %q", cfg.DatabaseURL)
	}
	if cfg.RateLimitEnabled != true {
		t.Error("expected RateLimitEnabled to default to true")
	}
	if cfg.DataDir != "./data" {
		t.Errorf("expected default DataDir ./data, got %q", cfg.DataDir)
	}
}

func TestLoad_CustomPort(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	os.Setenv("PORT", ":9090")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")
	defer os.Unsetenv("PORT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.Port != ":9090" {
		t.Errorf("expected port :9090, got %q", cfg.Port)
	}
}

func TestLoad_RateLimitEnabled(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	os.Setenv("RATE_LIMIT_ENABLED", "false")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")
	defer os.Unsetenv("RATE_LIMIT_ENABLED")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.RateLimitEnabled != false {
		t.Error("expected RateLimitEnabled to be false")
	}
}

func TestLoad_AllowHTTPProviders(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	os.Setenv("ALLOW_HTTP_PROVIDERS", "true")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")
	defer os.Unsetenv("ALLOW_HTTP_PROVIDERS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.AllowHTTPProviders != true {
		t.Error("expected AllowHTTPProviders to be true")
	}
}

func TestLoad_MaxRequestSize(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	os.Setenv("MAX_REQUEST_SIZE", "5242880")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")
	defer os.Unsetenv("MAX_REQUEST_SIZE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.MaxRequestSize != 5242880 {
		t.Errorf("expected MaxRequestSize 5242880, got %d", cfg.MaxRequestSize)
	}
}

func TestLoad_DefaultMaxRequestSize(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	os.Setenv("MASTER_KEY", "test-master-key-12345")
	defer os.Unsetenv("DATABASE_URL")
	defer os.Unsetenv("MASTER_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.MaxRequestSize != 10*1024*1024 {
		t.Errorf("expected default MaxRequestSize %d, got %d", 10*1024*1024, cfg.MaxRequestSize)
	}
}

// ---------------------------------------------------------------------------
// Config.String (masking)
// ---------------------------------------------------------------------------

func TestConfig_String_MasksMasterKey(t *testing.T) {
	cfg := &Config{
		Port:        ":8080",
		DatabaseURL: "postgres://user:secret@localhost/db",
		MasterKey:   "super-secret-master-key-12345",
	}
	s := cfg.String()
	// Master key and database URL are intentionally omitted from the banner.
	// Verify they don't appear in the output at all.
	if contains(s, "super-secret-master-key-12345") {
		t.Error("Config.String() must not leak the master key")
	}
	if contains(s, "secret") {
		t.Error("Config.String() must not leak the database password")
	}
}

func TestConfig_String_OmitsSensitiveURLs(t *testing.T) {
	cfg := &Config{
		Port:        ":8080",
		DatabaseURL: "postgres://admin:MyS3cret!@localhost:5432/mydb",
		MasterKey:   "test-key",
	}
	s := cfg.String()
	// Neither the database URL nor the master key should appear.
	if contains(s, "MyS3cret!") {
		t.Error("Config.String() must not leak the database password")
	}
	if contains(s, "test-key") {
		t.Error("Config.String() must not leak the master key")
	}
	if contains(s, "Database URL") {
		t.Error("Config.String() should not contain 'Database URL' row")
	}
	if contains(s, "Master Key") {
		t.Error("Config.String() should not contain 'Master Key' row")
	}
}

func TestConfig_String_ShortMasterKey(t *testing.T) {
	cfg := &Config{
		MasterKey: "abc",
	}
	s := cfg.String()
	if contains(s, "abc") {
		t.Error("Config.String() must not leak the master key")
	}
	if contains(s, "Master Key") {
		t.Error("Config.String() should not contain 'Master Key' row")
	}
}

func TestConfig_String_AdminTokenSet(t *testing.T) {
	cfg := &Config{
		AdminToken: "my-admin-token",
	}
	s := cfg.String()
	if contains(s, "my-admin-token") {
		t.Error("Config.String() should not leak admin token")
	}
	if !contains(s, "***set***") {
		t.Error("Config.String() should show ***set*** when admin token is set")
	}
}

func TestConfig_String_AdminTokenEmpty(t *testing.T) {
	cfg := &Config{
		AdminToken: "",
	}
	s := cfg.String()
	if !contains(s, "(auto-generated)") {
		t.Error("Config.String() should show (auto-generated) when admin token is empty")
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func TestConfig_String_CORSOriginsMultiLine(t *testing.T) {
	cfg := &Config{
		Port:        ":8080",
		MasterKey:   "test-key",
		CORSOrigins: []string{"http://localhost:5173", "http://localhost:8081"},
	}
	s := cfg.String()

	// Both origins must appear on separate lines
	if !contains(s, "http://localhost:5173") {
		t.Error("first CORS origin should appear in output")
	}
	if !contains(s, "http://localhost:8081") {
		t.Error("second CORS origin should appear in output")
	}

	// They must appear on separate lines, not comma-separated on one line
	lines := strings.Split(s, "\n")
	corsLines := 0
	for _, line := range lines {
		if contains(line, "http://localhost:5173") || contains(line, "http://localhost:8081") {
			corsLines++
		}
	}
	if corsLines != 2 {
		t.Errorf("expected each CORS origin on its own line, got %d CORS lines", corsLines)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Loopback DNS resolution test
// ---------------------------------------------------------------------------

func TestValidateProviderURL_ResolvesLoopback(t *testing.T) {
	// Create a test server to get a real reachable URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// The test server URL uses 127.0.0.1, which should be blocked
	cfg := &Config{}
	err := cfg.ValidateProviderURL(server.URL)
	if err == nil {
		t.Error("httptest server URL (127.0.0.1) should be blocked by ValidateProviderURL")
	}
}

func TestValidateProviderURL_AllowsNonLocalhostCustom(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"my-proxy.example.com"}}
	err := cfg.ValidateProviderURL("https://my-proxy.example.com/v1")
	if err != nil {
		t.Errorf("non-localhost, known-provider-like URL should be allowed, got error: %v", err)
	}
}

func TestValidateProviderURL_HTTPSchemes(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"my-proxy.example.com"}}
	// Both http and https should pass ValidateProviderURL — scheme enforcement
	// is a separate concern handled by the API handler
	err := cfg.ValidateProviderURL("http://my-proxy.example.com/v1")
	if err != nil {
		t.Errorf("http scheme should pass ValidateProviderURL (scheme check is separate), got: %v", err)
	}
	err = cfg.ValidateProviderURL("https://my-proxy.example.com/v1")
	if err != nil {
		t.Errorf("https scheme should pass ValidateProviderURL, got: %v", err)
	}
}

func TestValidateProviderURL_AllowListMultipleEntries(t *testing.T) {
	cfg := &Config{AllowedProviderHosts: []string{"host-a.com", "host-b.com", "host-c.com"}}

	for _, host := range []string{"host-a.com", "host-b.com", "host-c.com"} {
		url := "https://" + host + "/v1"
		err := cfg.ValidateProviderURL(url)
		if err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", url, err)
		}
	}

	err := cfg.ValidateProviderURL("https://host-d.com/v1")
	if err == nil {
		t.Error("expected host-d.com to be blocked (not in allowlist)")
	}
}

// ---------------------------------------------------------------------------
// clampInt
// ---------------------------------------------------------------------------

func TestClampInt_WithinRange(t *testing.T) {
	result := clampInt(5, 1, 10)
	if result != 5 {
		t.Errorf("clampInt(5, 1, 10) = %d, want 5", result)
	}
}

func TestClampInt_BelowMin(t *testing.T) {
	result := clampInt(0, 1, 10)
	if result != 1 {
		t.Errorf("clampInt(0, 1, 10) = %d, want 1", result)
	}
}

func TestClampInt_AboveMax(t *testing.T) {
	result := clampInt(15, 1, 10)
	if result != 10 {
		t.Errorf("clampInt(15, 1, 10) = %d, want 10", result)
	}
}

func TestClampInt_AtMin(t *testing.T) {
	result := clampInt(1, 1, 10)
	if result != 1 {
		t.Errorf("clampInt(1, 1, 10) = %d, want 1", result)
	}
}

func TestClampInt_AtMax(t *testing.T) {
	result := clampInt(10, 1, 10)
	if result != 10 {
		t.Errorf("clampInt(10, 1, 10) = %d, want 10", result)
	}
}

func TestClampInt_NegativeValues(t *testing.T) {
	result := clampInt(-5, -10, -1)
	if result != -5 {
		t.Errorf("clampInt(-5, -10, -1) = %d, want -5", result)
	}
}

func TestClampInt_BelowMinNegative(t *testing.T) {
	result := clampInt(-15, -10, -1)
	if result != -10 {
		t.Errorf("clampInt(-15, -10, -1) = %d, want -10", result)
	}
}

// ---------------------------------------------------------------------------
// clampInt32
// ---------------------------------------------------------------------------

func TestClampInt32_WithinRange(t *testing.T) {
	result := clampInt32(5, 1, 10)
	if result != 5 {
		t.Errorf("clampInt32(5, 1, 10) = %d, want 5", result)
	}
}

func TestClampInt32_BelowMin(t *testing.T) {
	result := clampInt32(0, 1, 10)
	if result != 1 {
		t.Errorf("clampInt32(0, 1, 10) = %d, want 1", result)
	}
}

func TestClampInt32_AboveMax(t *testing.T) {
	result := clampInt32(15, 1, 10)
	if result != 10 {
		t.Errorf("clampInt32(15, 1, 10) = %d, want 10", result)
	}
}

func TestClampInt32_AtMin(t *testing.T) {
	result := clampInt32(1, 1, 10)
	if result != 1 {
		t.Errorf("clampInt32(1, 1, 10) = %d, want 1", result)
	}
}

func TestClampInt32_AtMax(t *testing.T) {
	result := clampInt32(10, 1, 10)
	if result != 10 {
		t.Errorf("clampInt32(10, 1, 10) = %d, want 10", result)
	}
}

func TestClampInt32_NegativeValues(t *testing.T) {
	result := clampInt32(-5, -10, -1)
	if result != -5 {
		t.Errorf("clampInt32(-5, -10, -1) = %d, want -5", result)
	}
}

func TestClampInt32_BelowMinNegative(t *testing.T) {
	result := clampInt32(-15, -10, -1)
	if result != -10 {
		t.Errorf("clampInt32(-15, -10, -1) = %d, want -10", result)
	}
}

// ---------------------------------------------------------------------------
// getIntEnvAsInt
// ---------------------------------------------------------------------------

func TestGetIntEnvAsInt_ValidValue(t *testing.T) {
	os.Setenv("TEST_INTASINT", "42")
	defer os.Unsetenv("TEST_INTASINT")
	result := getIntEnvAsInt("TEST_INTASINT", 0)
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestGetIntEnvAsInt_Empty(t *testing.T) {
	os.Unsetenv("TEST_INTASINT_MISSING")
	result := getIntEnvAsInt("TEST_INTASINT_MISSING", 99)
	if result != 99 {
		t.Errorf("expected default 99, got %d", result)
	}
}

func TestGetIntEnvAsInt_InvalidString(t *testing.T) {
	os.Setenv("TEST_INTASINT", "not-a-number")
	defer os.Unsetenv("TEST_INTASINT")
	result := getIntEnvAsInt("TEST_INTASINT", 50)
	if result != 50 {
		t.Errorf("expected fallback default 50, got %d", result)
	}
}

func TestGetIntEnvAsInt_NegativeValue(t *testing.T) {
	os.Setenv("TEST_INTASINT", "-5")
	defer os.Unsetenv("TEST_INTASINT")
	result := getIntEnvAsInt("TEST_INTASINT", 0)
	if result != -5 {
		t.Errorf("expected -5, got %d", result)
	}
}

func TestGetIntEnvAsInt_ZeroValue(t *testing.T) {
	os.Setenv("TEST_INTASINT", "0")
	defer os.Unsetenv("TEST_INTASINT")
	result := getIntEnvAsInt("TEST_INTASINT", 10)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// getIntEnvAsInt32
// ---------------------------------------------------------------------------

func TestGetIntEnvAsInt32_ValidValue(t *testing.T) {
	os.Setenv("TEST_INTASINT32", "42")
	defer os.Unsetenv("TEST_INTASINT32")
	result := getIntEnvAsInt32("TEST_INTASINT32", 0)
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestGetIntEnvAsInt32_Empty(t *testing.T) {
	os.Unsetenv("TEST_INTASINT32_MISSING")
	result := getIntEnvAsInt32("TEST_INTASINT32_MISSING", 99)
	if result != 99 {
		t.Errorf("expected default 99, got %d", result)
	}
}

func TestGetIntEnvAsInt32_InvalidString(t *testing.T) {
	os.Setenv("TEST_INTASINT32", "not-a-number")
	defer os.Unsetenv("TEST_INTASINT32")
	result := getIntEnvAsInt32("TEST_INTASINT32", 50)
	if result != 50 {
		t.Errorf("expected fallback default 50, got %d", result)
	}
}

func TestGetIntEnvAsInt32_NegativeValue(t *testing.T) {
	os.Setenv("TEST_INTASINT32", "-5")
	defer os.Unsetenv("TEST_INTASINT32")
	result := getIntEnvAsInt32("TEST_INTASINT32", 0)
	if result != -5 {
		t.Errorf("expected -5, got %d", result)
	}
}

func TestGetIntEnvAsInt32_Overflow(t *testing.T) {
	os.Setenv("TEST_INTASINT32", "9999999999")
	defer os.Unsetenv("TEST_INTASINT32")
	result := getIntEnvAsInt32("TEST_INTASINT32", 10)
	if result != 10 {
		t.Errorf("expected fallback default 10 for overflow, got %d", result)
	}
}

func TestGetIntEnvAsInt32_ZeroValue(t *testing.T) {
	os.Setenv("TEST_INTASINT32", "0")
	defer os.Unsetenv("TEST_INTASINT32")
	result := getIntEnvAsInt32("TEST_INTASINT32", 10)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestClampInt64_WithinRange(t *testing.T) {
	result := clampInt64(5, 1, 10)
	if result != 5 {
		t.Errorf("clampInt64(5, 1, 10) = %d, want 5", result)
	}
}

func TestClampInt64_BelowMin(t *testing.T) {
	result := clampInt64(0, 1, 10)
	if result != 1 {
		t.Errorf("clampInt64(0, 1, 10) = %d, want 1", result)
	}
}

func TestClampInt64_AboveMax(t *testing.T) {
	result := clampInt64(15, 1, 10)
	if result != 10 {
		t.Errorf("clampInt64(15, 1, 10) = %d, want 10", result)
	}
}

func TestClampInt64_AtMin(t *testing.T) {
	result := clampInt64(1, 1, 10)
	if result != 1 {
		t.Errorf("clampInt64(1, 1, 10) = %d, want 1", result)
	}
}

func TestClampInt64_AtMax(t *testing.T) {
	result := clampInt64(10, 1, 10)
	if result != 10 {
		t.Errorf("clampInt64(10, 1, 10) = %d, want 10", result)
	}
}

func TestClampInt64_NegativeValues(t *testing.T) {
	result := clampInt64(-5, -10, -1)
	if result != -5 {
		t.Errorf("clampInt64(-5, -10, -1) = %d, want -5", result)
	}
}

func TestClampInt64_BelowMinNegative(t *testing.T) {
	result := clampInt64(-15, -10, -1)
	if result != -10 {
		t.Errorf("clampInt64(-15, -10, -1) = %d, want -10", result)
	}
}

// ---------------------------------------------------------------------------
// clampFloat
// ---------------------------------------------------------------------------

func TestClampFloat_WithinRange(t *testing.T) {
	result := clampFloat(0.5, 0.0, 1.0)
	if result != 0.5 {
		t.Errorf("clampFloat(0.5, 0, 1) = %g, want 0.5", result)
	}
}

func TestClampFloat_BelowMin(t *testing.T) {
	result := clampFloat(-0.1, 0.0, 1.0)
	if result != 0.0 {
		t.Errorf("clampFloat(-0.1, 0, 1) = %g, want 0", result)
	}
}

func TestClampFloat_AboveMax(t *testing.T) {
	result := clampFloat(1.5, 0.0, 1.0)
	if result != 1.0 {
		t.Errorf("clampFloat(1.5, 0, 1) = %g, want 1", result)
	}
}

// ---------------------------------------------------------------------------
// getBoolEnvWithDefault
// ---------------------------------------------------------------------------

func TestGetBoolEnvWithDefault_TrueValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"True", "True", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"YES", "YES", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tc.value)
			defer os.Unsetenv("TEST_BOOL")
			result := getBoolEnvWithDefault("TEST_BOOL", false)
			if result != tc.want {
				t.Errorf("getBoolEnvWithDefault(%q) = %v, want %v", tc.value, result, tc.want)
			}
		})
	}
}

func TestGetBoolEnvWithDefault_FalseValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"false", "false"},
		{"FALSE", "FALSE"},
		{"0", "0"},
		{"no", "no"},
		{"NO", "NO"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("TEST_BOOL", tc.value)
			defer os.Unsetenv("TEST_BOOL")
			result := getBoolEnvWithDefault("TEST_BOOL", true)
			if result != false {
				t.Errorf("getBoolEnvWithDefault(%q) = %v, want false", tc.value, result)
			}
		})
	}
}

func TestGetBoolEnvWithDefault_DefaultOnEmpty(t *testing.T) {
	os.Unsetenv("TEST_BOOL_MISSING")
	result := getBoolEnvWithDefault("TEST_BOOL_MISSING", true)
	if result != true {
		t.Error("expected default true when env var is missing")
	}

	result = getBoolEnvWithDefault("TEST_BOOL_MISSING", false)
	if result != false {
		t.Error("expected default false when env var is missing")
	}
}

func TestGetBoolEnvWithDefault_DefaultOnGarbage(t *testing.T) {
	os.Setenv("TEST_BOOL", "maybe")
	defer os.Unsetenv("TEST_BOOL")
	result := getBoolEnvWithDefault("TEST_BOOL", true)
	if result != true {
		t.Error("expected default value for unrecognized string")
	}
}

// ---------------------------------------------------------------------------
// getIntEnvWithDefault
// ---------------------------------------------------------------------------

func TestGetIntEnvWithDefault_ValidInt(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	defer os.Unsetenv("TEST_INT")
	result := getIntEnvWithDefault("TEST_INT", 0)
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestGetIntEnvWithDefault_Empty(t *testing.T) {
	os.Unsetenv("TEST_INT_MISSING")
	result := getIntEnvWithDefault("TEST_INT_MISSING", 99)
	if result != 99 {
		t.Errorf("expected default 99, got %d", result)
	}
}

func TestGetIntEnvWithDefault_InvalidString(t *testing.T) {
	os.Setenv("TEST_INT", "not-a-number")
	defer os.Unsetenv("TEST_INT")
	result := getIntEnvWithDefault("TEST_INT", 50)
	if result != 50 {
		t.Errorf("expected fallback default 50, got %d", result)
	}
}

func TestGetIntEnvWithDefault_NegativeValue(t *testing.T) {
	os.Setenv("TEST_INT", "-5")
	defer os.Unsetenv("TEST_INT")
	result := getIntEnvWithDefault("TEST_INT", 0)
	if result != -5 {
		t.Errorf("expected -5, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// getFloatEnvWithDefault
// ---------------------------------------------------------------------------

func TestGetFloatEnvWithDefault_ValidFloat(t *testing.T) {
	os.Setenv("TEST_FLOAT", "3.14")
	defer os.Unsetenv("TEST_FLOAT")
	result := getFloatEnvWithDefault("TEST_FLOAT", 0.0)
	if result != 3.14 {
		t.Errorf("expected 3.14, got %g", result)
	}
}

func TestGetFloatEnvWithDefault_Empty(t *testing.T) {
	os.Unsetenv("TEST_FLOAT_MISSING")
	result := getFloatEnvWithDefault("TEST_FLOAT_MISSING", 2.5)
	if result != 2.5 {
		t.Errorf("expected default 2.5, got %g", result)
	}
}

func TestGetFloatEnvWithDefault_InvalidString(t *testing.T) {
	os.Setenv("TEST_FLOAT", "abc")
	defer os.Unsetenv("TEST_FLOAT")
	result := getFloatEnvWithDefault("TEST_FLOAT", 1.0)
	if result != 1.0 {
		t.Errorf("expected fallback default 1.0, got %g", result)
	}
}

// ---------------------------------------------------------------------------
// formatCORSOrigins
// ---------------------------------------------------------------------------

func TestFormatCORSOriginRows_EmptyList(t *testing.T) {
	result := formatCORSOriginRows([]string{}, 16, 3, 2)
	if len(result) != 1 {
		t.Fatalf("expected 1 row for empty list, got %d", len(result))
	}
	if result[0].label != "CORS Origins" || result[0].value != "(none)" {
		t.Errorf("expected (none), got label=%q value=%q", result[0].label, result[0].value)
	}
}

func TestFormatCORSOriginRows_SingleOrigin(t *testing.T) {
	result := formatCORSOriginRows([]string{"http://localhost:5173"}, 16, 3, 2)
	if len(result) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result))
	}
	if result[0].label != "CORS Origins" {
		t.Errorf("expected label 'CORS Origins', got %q", result[0].label)
	}
	if result[0].value != "http://localhost:5173" {
		t.Errorf("expected 'http://localhost:5173', got %q", result[0].value)
	}
}

func TestFormatCORSOriginRows_MultipleOrigins(t *testing.T) {
	origins := []string{"http://a.com", "http://b.com", "http://c.com"}
	result := formatCORSOriginRows(origins, 16, 3, 2)
	if len(result) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(result))
	}
	if result[0].label != "CORS Origins" {
		t.Errorf("first row label should be 'CORS Origins', got %q", result[0].label)
	}
	if result[0].value != "http://a.com" {
		t.Errorf("first row value should be 'http://a.com', got %q", result[0].value)
	}
	for i, r := range result[1:] {
		if r.label != "" {
			t.Errorf("continuation row %d label should be empty, got %q", i, r.label)
		}
	}
	if result[1].value != "http://b.com" {
		t.Errorf("second row value should be 'http://b.com', got %q", result[1].value)
	}
	if result[2].value != "http://c.com" {
		t.Errorf("third row value should be 'http://c.com', got %q", result[2].value)
	}
}

func TestFormatCORSOriginRows_Truncation(t *testing.T) {
	longOrigin := "http://example.com/very/long/path/that/exceeds/the/max/value/width/by/quite/a/lot"
	origins := []string{longOrigin, "http://b.com"}
	// maxValW = 80 - 3 - 16 - 2 = 59
	result := formatCORSOriginRows(origins, 16, 3, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}
	// The first origin should be truncated since it's > 59 chars
	if !contains(result[0].value, "...") {
		t.Errorf("expected truncation for long origin, got %q", result[0].value)
	}
}

// ---------------------------------------------------------------------------
// padRight
// ---------------------------------------------------------------------------

func TestPadRight_ShorterThanWidth(t *testing.T) {
	result := padRight("hi", 5)
	if result != "hi   " {
		t.Errorf("expected %q, got %q", "hi   ", result)
	}
}

func TestPadRight_ExactWidth(t *testing.T) {
	result := padRight("hello", 5)
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestPadRight_LongerThanWidth(t *testing.T) {
	result := padRight("hello world", 5)
	if result != "hello world" {
		t.Errorf("expected unchanged string for longer than width, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------

func TestFormatBytes_Bytes(t *testing.T) {
	result := formatBytes(500)
	if result != "500 B" {
		t.Errorf("expected %q, got %q", "500 B", result)
	}
}

func TestFormatBytes_Kilobytes(t *testing.T) {
	result := formatBytes(2048)
	if result != "2 KB" {
		t.Errorf("expected %q, got %q", "2 KB", result)
	}
}

func TestFormatBytes_Megabytes(t *testing.T) {
	result := formatBytes(5 * 1024 * 1024)
	if result != "5 MB" {
		t.Errorf("expected %q, got %q", "5 MB", result)
	}
}

func TestFormatBytes_Gigabytes(t *testing.T) {
	result := formatBytes(3 * 1024 * 1024 * 1024)
	if result != "3 GB" {
		t.Errorf("expected %q, got %q", "3 GB", result)
	}
}

func TestFormatBytes_Zero(t *testing.T) {
	result := formatBytes(0)
	if result != "0 B" {
		t.Errorf("expected %q, got %q", "0 B", result)
	}
}

func TestFormatBytes_JustUnderKB(t *testing.T) {
	result := formatBytes(1023)
	if result != "1023 B" {
		t.Errorf("expected %q, got %q", "1023 B", result)
	}
}

// ---------------------------------------------------------------------------
// parseCORSOrigins
// ---------------------------------------------------------------------------

func TestParseCORSOrigins_EmptyString(t *testing.T) {
	result := parseCORSOrigins("")
	if len(result) != 0 {
		t.Errorf("expected empty slice for empty string, got %v", result)
	}
}

func TestParseCORSOrigins_SingleOrigin(t *testing.T) {
	result := parseCORSOrigins("http://localhost:5173")
	if len(result) != 1 {
		t.Fatalf("expected 1 origin, got %d", len(result))
	}
	if result[0] != "http://localhost:5173" {
		t.Errorf("expected 'http://localhost:5173', got %q", result[0])
	}
}

func TestParseCORSOrigins_MultipleOrigins(t *testing.T) {
	result := parseCORSOrigins("http://a.com,http://b.com,http://c.com")
	if len(result) != 3 {
		t.Fatalf("expected 3 origins, got %d", len(result))
	}
	if result[0] != "http://a.com" {
		t.Errorf("expected 'http://a.com', got %q", result[0])
	}
	if result[1] != "http://b.com" {
		t.Errorf("expected 'http://b.com', got %q", result[1])
	}
	if result[2] != "http://c.com" {
		t.Errorf("expected 'http://c.com', got %q", result[2])
	}
}

func TestParseCORSOrigins_TrimWhitespace(t *testing.T) {
	result := parseCORSOrigins("  http://a.com  ,  http://b.com  ,  http://c.com  ")
	if len(result) != 3 {
		t.Fatalf("expected 3 origins, got %d", len(result))
	}
	if result[0] != "http://a.com" {
		t.Errorf("expected trimmed 'http://a.com', got %q", result[0])
	}
	if result[1] != "http://b.com" {
		t.Errorf("expected trimmed 'http://b.com', got %q", result[1])
	}
	if result[2] != "http://c.com" {
		t.Errorf("expected trimmed 'http://c.com', got %q", result[2])
	}
}

func TestParseCORSOrigins_MixedEmptyEntries(t *testing.T) {
	result := parseCORSOrigins("http://a.com, ,http://b.com")
	if len(result) != 2 {
		t.Fatalf("expected 2 origins (empty entries filtered), got %d", len(result))
	}
	if result[0] != "http://a.com" {
		t.Errorf("expected 'http://a.com', got %q", result[0])
	}
	if result[1] != "http://b.com" {
		t.Errorf("expected 'http://b.com', got %q", result[1])
	}
}

func TestParseCORSOrigins_WildcardRemoved(t *testing.T) {
	result := parseCORSOrigins("*")
	if len(result) != 0 {
		t.Errorf("expected empty slice for wildcard, got %v", result)
	}
}

func TestParseCORSOrigins_WildcardWithOtherOrigins(t *testing.T) {
	result := parseCORSOrigins("http://a.com,*,http://b.com")
	if len(result) != 2 {
		t.Fatalf("expected 2 origins (wildcard removed), got %d", len(result))
	}
	if result[0] != "http://a.com" {
		t.Errorf("expected 'http://a.com', got %q", result[0])
	}
	if result[1] != "http://b.com" {
		t.Errorf("expected 'http://b.com', got %q", result[1])
	}
}

func TestParseCORSOrigins_DuplicatesNotRemoved(t *testing.T) {
	result := parseCORSOrigins("http://a.com,http://a.com,http://b.com")
	if len(result) != 3 {
		t.Fatalf("expected 3 origins (duplicates kept), got %d", len(result))
	}
}

func TestParseCORSOrigins_OnlyWhitespace(t *testing.T) {
	result := parseCORSOrigins("   ,   ,   ")
	if len(result) != 0 {
		t.Errorf("expected empty slice for whitespace-only, got %v", result)
	}
}

func TestGetEnvWithDefault(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		envValue     string
		defaultValue string
		want         string
	}{
		{
			name:         "set_var_returns_env",
			key:          "TEST_VAR",
			envValue:     "env_value",
			defaultValue: "default_value",
			want:         "env_value",
		},
		{
			name:         "unset_var_returns_default",
			key:          "MISSING_VAR",
			envValue:     "",
			defaultValue: "default_value",
			want:         "default_value",
		},
		{
			name:         "empty_env_var_returns_default",
			key:          "EMPTY_VAR",
			envValue:     "",
			defaultValue: "default_value",
			want:         "default_value",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
				defer t.Cleanup(func() { t.Setenv(tt.key, "") })
			}
			got := getEnvWithDefault(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvWithDefault(%q, %q) = %q, want %q", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests moved from config_coverage_test.go
// ---------------------------------------------------------------------------

// TestLoad_ALLOW_HTTP_PROVIDERS tests that ALLOW_HTTP_PROVIDERS=true sets
// AllowHTTPProviders to true, and unset defaults to false.
func TestLoad_ALLOW_HTTP_PROVIDERS(t *testing.T) {
	// Test with ALLOW_HTTP_PROVIDERS=true
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	t.Setenv("MASTER_KEY", "test-master-key")
	t.Setenv("ALLOW_HTTP_PROVIDERS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.AllowHTTPProviders != true {
		t.Error("Expected AllowHTTPProviders=true when ALLOW_HTTP_PROVIDERS=true")
	}

	// Test with ALLOW_HTTP_PROVIDERS unset (should default to false)
	t.Setenv("ALLOW_HTTP_PROVIDERS", "")
	cfg2, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg2.AllowHTTPProviders != false {
		t.Error("Expected AllowHTTPProviders=false when ALLOW_HTTP_PROVIDERS is unset")
	}
}

// TestLoad_CORS_ORIGINS tests that CORS_ORIGINS is parsed correctly with multiple entries.
func TestLoad_CORS_ORIGINS(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	t.Setenv("MASTER_KEY", "test-master-key")
	t.Setenv("CORS_ORIGINS", "http://localhost:3000,http://localhost:5173")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Fatalf("Expected 2 CORS origins, got %d", len(cfg.CORSOrigins))
	}
	if cfg.CORSOrigins[0] != "http://localhost:3000" {
		t.Errorf("Expected first origin 'http://localhost:3000', got %q", cfg.CORSOrigins[0])
	}
	if cfg.CORSOrigins[1] != "http://localhost:5173" {
		t.Errorf("Expected second origin 'http://localhost:5173', got %q", cfg.CORSOrigins[1])
	}
}

// TestLoad_TRUSTED_PROXIES tests that TRUSTED_PROXIES is parsed correctly with multiple CIDR entries.
func TestLoad_TRUSTED_PROXIES(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	t.Setenv("MASTER_KEY", "test-master-key")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8,172.16.0.0/12")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("Expected 2 trusted proxy CIDRs, got %d", len(cfg.TrustedProxies))
	}
	// Verify the CIDRs are parsed correctly
	if !cfg.TrustedProxies[0].Contains(net.ParseIP("10.0.0.1")) {
		t.Error("First CIDR should contain 10.0.0.1")
	}
	if !cfg.TrustedProxies[1].Contains(net.ParseIP("172.16.0.1")) {
		t.Error("Second CIDR should contain 172.16.0.1")
	}
}

// TestValidateProviderURL_AllowedHosts tests that when ALLOWED_PROVIDER_HOSTS
// is set, URLs with non-allowed hosts are rejected.
func TestValidateProviderURL_AllowedHosts(t *testing.T) {
	cfg := &Config{
		AllowedProviderHosts: []string{"allowed.example.com", "another-allowed.com"},
	}

	// Test with a host not in the allowlist
	err := cfg.ValidateProviderURL("https://not-allowed.example.com/v1")
	if err == nil {
		t.Error("Expected error for host not in ALLOWED_PROVIDER_HOSTS")
	}
	if !strings.Contains(err.Error(), "is not in ALLOWED_PROVIDER_HOSTS allowlist") {
		t.Errorf("Error should mention allowlist, got: %v", err)
	}

	// Test with a host in the allowlist (should pass)
	err = cfg.ValidateProviderURL("https://allowed.example.com/v1")
	if err != nil {
		t.Errorf("Expected no error for allowed host, got: %v", err)
	}
}

// TestConfig_String_MasksFields tests that String() masks sensitive fields.
func TestConfig_String_MasksFields(t *testing.T) {
	cfg := &Config{
		Port:        ":8080",
		DatabaseURL: "postgres://user:secret-password@localhost:5432/mydb",
		MasterKey:   "super-secret-master-key",
		AdminToken:  "admin-token-value",
		DataDir:     "./data",
	}

	s := cfg.String()

	// Database password should not appear
	if strings.Contains(s, "secret-password") {
		t.Error("Config.String() must not leak the database password")
	}

	// Master key should not appear
	if strings.Contains(s, "super-secret-master-key") {
		t.Error("Config.String() must not leak the master key")
	}

	// Admin token should not appear
	if strings.Contains(s, "admin-token-value") {
		t.Error("Config.String() must not leak the admin token")
	}

	// Non-sensitive fields should appear
	if !strings.Contains(s, ":8080") {
		t.Error("Config.String() should show the port")
	}
	if !strings.Contains(s, "./data") {
		t.Error("Config.String() should show the data dir")
	}
}

// TestLoadTrustedProxies_InvalidCIDRGraceful tests that invalid CIDR entries are
// handled gracefully (logged/skipped).
func TestLoadTrustedProxies_InvalidCIDRGraceful(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "invalid-cidr,10.0.0.0/8,also-invalid,192.168.0.0/16")

	nets := LoadTrustedProxies()
	// Should have 2 valid CIDRs (invalid ones skipped)
	if len(nets) != 2 {
		t.Fatalf("Expected 2 valid CIDRs (invalid skipped), got %d", len(nets))
	}

	// Verify the valid CIDRs are present
	if !nets[0].Contains(net.ParseIP("10.0.0.1")) {
		t.Error("First valid CIDR should contain 10.0.0.1")
	}
	if !nets[1].Contains(net.ParseIP("192.168.1.1")) {
		t.Error("Second valid CIDR should contain 192.168.1.1")
	}
}
