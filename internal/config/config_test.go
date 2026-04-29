package config

import (
	"net/http"
	"net/http/httptest"
	"os"
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
	cfg := &Config{}
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
	os.Unsetenv("MASTER_KEY")

	_, err := Load()
	if err == nil {
		t.Error("expected error when DATABASE_URL is missing, got nil")
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
	if contains(s, "super-secret-master-key-12345") {
		t.Error("Config.String() should mask the master key")
	}
	if !contains(s, "***") {
		t.Error("Config.String() should contain masked key indicator")
	}
}

func TestConfig_String_MasksDatabaseURL(t *testing.T) {
	cfg := &Config{
		Port:        ":8080",
		DatabaseURL: "postgres://admin:MyS3cret!@localhost:5432/mydb",
		MasterKey:   "test-key",
	}
	s := cfg.String()
	if contains(s, "MyS3cret!") {
		t.Error("Config.String() should mask the database password")
	}
}

func TestConfig_String_ShortMasterKey(t *testing.T) {
	cfg := &Config{
		MasterKey: "abc",
	}
	s := cfg.String()
	if contains(s, "abc") {
		t.Error("Config.String() should not leak short master key")
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
	cfg := &Config{}
	err := cfg.ValidateProviderURL("https://my-proxy.example.com/v1")
	if err != nil {
		t.Errorf("non-localhost, known-provider-like URL should be allowed, got error: %v", err)
	}
}

func TestValidateProviderURL_HTTPSchemes(t *testing.T) {
	cfg := &Config{}
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
