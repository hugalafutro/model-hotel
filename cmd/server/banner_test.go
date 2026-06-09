package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hugalafutro/model-hotel/internal/config"
)

func TestPrintStartupBanner(t *testing.T) {
	cfg := &config.Config{
		Port:       ":8080",
		DataDir:    "/data",
		AdminToken: "test-token",
		DebugLog:   true,
	}

	var buf bytes.Buffer
	printStartupBanner(&buf, cfg)

	output := buf.String()

	if !strings.Contains(output, "███╗") {
		t.Error("banner should contain ASCII art")
	}
	if !strings.Contains(output, "Port") {
		t.Error("banner should contain config summary")
	}
	if !strings.HasPrefix(output, "\n") {
		t.Error("banner should start with a blank line")
	}
}

func TestPrintAdminTokenBox(t *testing.T) {
	var buf bytes.Buffer
	printAdminTokenBox(&buf, "abc123")

	output := buf.String()

	if !strings.Contains(output, "ADMIN TOKEN") {
		t.Error("token box should contain 'ADMIN TOKEN' header")
	}
	if !strings.Contains(output, "abc123") {
		t.Error("token box should contain the token value")
	}
	if !strings.Contains(output, "will NOT be shown again") {
		t.Error("token box should contain the warning text")
	}
	if !strings.Contains(output, "╔") || !strings.Contains(output, "╚") {
		t.Error("token box should have box-drawing borders")
	}
}

func TestPrintAdminTokenBoxAlignment(t *testing.T) {
	// Verify that every line in the box has the same width and that
	// content lines end with ║, regardless of token length.
	tests := []struct {
		name  string
		token string
	}{
		{"short token", "abc"},
		{"32-char hex token", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"},
		{"64-char hex token", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			printAdminTokenBox(&buf, tt.token)

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			if len(lines) < 3 {
				t.Fatalf("expected at least 3 lines, got %d", len(lines))
			}

			// All lines must have the same rune count.
			expectedWidth := utf8.RuneCountInString(lines[0])
			for i, line := range lines {
				width := utf8.RuneCountInString(line)
				if width != expectedWidth {
					t.Errorf("line %d has width %d, expected %d: %q", i, width, expectedWidth, line)
				}
			}

			// Content lines (not top/bottom borders) must end with ║.
			for i, line := range lines {
				if strings.HasPrefix(line, "╔") || strings.HasPrefix(line, "╚") {
					continue
				}
				if !strings.HasSuffix(line, "║") {
					t.Errorf("line %d does not end with ║: %q", i, line)
				}
			}
		})
	}
}

func TestPrintReadyMessage(t *testing.T) {
	var buf bytes.Buffer
	printReadyMessage(&buf, "v0.3.2")

	output := buf.String()

	if !strings.Contains(output, "Model Hotel (v0.3.2) instance is up and running.") {
		t.Errorf("ready message should contain version, got: %q", output)
	}
	if !strings.HasSuffix(output, "\n") {
		t.Error("ready message should end with newline")
	}
}

func TestPrintReadyMessageDevVersion(t *testing.T) {
	var buf bytes.Buffer
	printReadyMessage(&buf, "dev")

	output := strings.TrimSpace(buf.String())
	if output != "Model Hotel (dev) instance is up and running." {
		t.Errorf("unexpected output: %q", output)
	}
}

func TestPrintStartupBannerContainsConfigFields(t *testing.T) {
	cfg := &config.Config{
		Port:               ":9090",
		DataDir:            "/tmp/data",
		AdminToken:         "mytoken",
		AllowHTTPProviders: true,
		RateLimitEnabled:   true,
		DebugLog:           false,
	}

	var buf bytes.Buffer
	printStartupBanner(&buf, cfg)

	output := buf.String()

	// The config box is rendered by cfg.String(), so we just verify
	// it's present and contains key fields.
	if !strings.Contains(output, ":9090") {
		t.Error("banner should contain the configured port")
	}
	if !strings.Contains(output, "/tmp/data") {
		t.Error("banner should contain the configured data dir")
	}
}

// TestPrintStdoutWrappers exercises the thin os.Stdout wrappers. The core
// formatting is verified above against a buffer; here we just ensure the
// wrappers invoke the core functions without panicking.
func TestPrintStdoutWrappers(t *testing.T) {
	cfg := &config.Config{
		Port:       ":8080",
		DataDir:    "/data",
		AdminToken: "test-token",
	}
	printStartupBannerStdout(cfg)
	printAdminTokenBoxStdout("abc123")
	printReadyMessageStdout("v1.2.3")
}
