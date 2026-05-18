package config

import (
	"net"
	"strings"
	"testing"
)

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
