//go:build live

// These tests hit real upstream provider APIs and need real credentials, so
// they are excluded from the default build entirely (rather than skipped at
// runtime, which pollutes the suite with permanent skips). Run them with:
//
//	go test -tags live ./internal/provider/ \
//	    -run 'LiveAPI|ZAICodingQuota'
//
// with ANTHROPIC_API_KEY / OPENAI_API_KEY / ZAI_CODING_API_KEY set as needed.
// A missing key is a hard failure here: you asked for the live run.

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestAnthropicDiscoveryLiveAPI(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Fatal("ANTHROPIC_API_KEY environment variable is required for live API tests")
	}

	svc := NewDiscoveryService(nil, nil)
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.anthropic.com",
	}

	ctx := context.Background()
	models, err := svc.discoverAnthropic(ctx, prov, apiKey)
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	t.Logf("Discovered %d models from Anthropic", len(models))

	pricingMatched := 0
	for _, m := range models {
		if m.InputPricePerMillion != nil {
			pricingMatched++
		}
	}
	t.Logf("  Pricing-matched: %d, No pricing: %d", pricingMatched, len(models)-pricingMatched)

	if pricingMatched == 0 {
		t.Error("expected at least some pricing-matched models")
	}

	for _, m := range models {
		var caps model.Capability
		//nolint:gosec // test-only
		json.Unmarshal([]byte(m.Capabilities), &caps)
		ctxLen := "<nil>"
		if m.ContextLength != nil {
			ctxLen = fmt.Sprintf("%d", *m.ContextLength)
		}
		maxOut := "<nil>"
		if m.MaxOutputTokens != nil {
			maxOut = fmt.Sprintf("%d", *m.MaxOutputTokens)
		}
		inPrice := "<nil>"
		if m.InputPricePerMillion != nil {
			inPrice = fmt.Sprintf("$%.2f", *m.InputPricePerMillion)
		}
		outPrice := "<nil>"
		if m.OutputPricePerMillion != nil {
			outPrice = fmt.Sprintf("$%.2f", *m.OutputPricePerMillion)
		}
		cachePrice := "<nil>"
		if m.InputPricePerMillionCacheHit != nil {
			cachePrice = fmt.Sprintf("$%.2f", *m.InputPricePerMillionCacheHit)
		}
		t.Logf("  %s display=%s ctx=%s max_out=%s in=%s out=%s cache=%s vision=%v struct=%v pdf=%v",
			m.ModelID, m.DisplayName, ctxLen, maxOut,
			inPrice, outPrice, cachePrice,
			caps.Vision, caps.StructuredOutput, caps.PDFUpload)
	}

	for _, m := range models {
		if m.ModelID == "claude-opus-4-7" || m.ModelID == "claude-opus-4-6" {
			if m.InputPricePerMillion == nil {
				t.Errorf("%s should have pricing", m.ModelID)
			}
			if m.ContextLength == nil {
				t.Errorf("%s should have context length from API", m.ModelID)
			}
			if m.DisplayName == "" {
				t.Errorf("%s should have display name from API", m.ModelID)
			}
		}
		if m.ModelID == "claude-opus-4-5-20251101" {
			if m.InputPricePerMillion == nil {
				t.Error("claude-opus-4-5-20251101 should have pricing from catalog strip")
			}
			if *m.InputPricePerMillion != 5.00 {
				t.Errorf("claude-opus-4-5-20251101 input price: got %.2f, want 5.00", *m.InputPricePerMillion)
			}
			if m.DisplayName != "Claude Opus 4.5" {
				t.Errorf("claude-opus-4-5-20251101 display name: got %s, want 'Claude Opus 4.5'", m.DisplayName)
			}
		}
	}
}

func TestOpenAIDiscoveryLiveAPI(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Fatal("OPENAI_API_KEY environment variable is required for live API tests")
	}

	svc := NewDiscoveryService(nil, nil)
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.openai.com/v1",
	}

	ctx := context.Background()
	models, err := svc.discoverOpenAI(ctx, prov, apiKey)
	if err != nil {
		t.Fatalf("discoverOpenAI failed: %v", err)
	}

	t.Logf("Discovered %d models from OpenAI", len(models))

	catalogMatches := 0
	minimalEntries := 0
	for _, m := range models {
		if m.InputPricePerMillion != nil {
			catalogMatches++
		} else {
			minimalEntries++
		}
	}

	t.Logf("  Catalog-matched: %d, Minimal entries: %d", catalogMatches, minimalEntries)

	if catalogMatches == 0 {
		t.Error("expected at least some catalog-matched models")
	}

	// Verify specific catalog-matched model
	for _, m := range models {
		if m.ModelID == "gpt-5.5" {
			if m.DisplayName != "GPT 5.5" {
				t.Errorf("gpt-5.5 display name: got %s", m.DisplayName)
			}
			if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 5.00 {
				t.Errorf("gpt-5.5 input price: got %v", m.InputPricePerMillion)
			}
			if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 30.00 {
				t.Errorf("gpt-5.5 output price: got %v", m.OutputPricePerMillion)
			}
			if m.ContextLength == nil || *m.ContextLength != 272000 {
				t.Errorf("gpt-5.5 context: got %v", m.ContextLength)
			}
			if m.InputPricePerMillionCacheHit == nil || *m.InputPricePerMillionCacheHit != 0.50 {
				t.Errorf("gpt-5.5 cache hit: got %v", m.InputPricePerMillionCacheHit)
			}

			var caps model.Capability
			json.Unmarshal([]byte(m.Capabilities), &caps)
			if !caps.Reasoning {
				t.Error("gpt-5.5 should have Reasoning=true")
			}
			if !caps.ToolCalling {
				t.Error("gpt-5.5 should have ToolCalling=true")
			}
			if !caps.StructuredOutput {
				t.Error("gpt-5.5 should have StructuredOutput=true")
			}

			t.Logf("OK: gpt-5.5 -> display=%s, ctx=%d, in=$%.2f, out=$%.2f, cache=$%.2f",
				m.DisplayName, *m.ContextLength, *m.InputPricePerMillion, *m.OutputPricePerMillion, *m.InputPricePerMillionCacheHit)
			break
		}
	}

	// Spot-check an unknown model
	for _, m := range models {
		if m.ModelID == "text-embedding-3-small" {
			if m.InputPricePerMillion != nil {
				t.Errorf("embedding model should have nil pricing, got %v", m.InputPricePerMillion)
			}
			if m.DisplayName != "text-embedding-3-small" {
				t.Errorf("unknown model should use ID as DisplayName, got %s", m.DisplayName)
			}
			t.Logf("OK: unknown model %s -> display=%s, pricing=nil (minimal entry)", m.ModelID, m.DisplayName)
			break
		}
	}
}

func TestGetZAICodingQuota(t *testing.T) {
	apiKey := os.Getenv("ZAI_CODING_API_KEY")
	if apiKey == "" {
		t.Fatal("ZAI_CODING_API_KEY environment variable is required for live API tests")
	}

	svc := &DiscoveryService{httpClient: http.DefaultClient}

	// Create properly encrypted key for testing
	masterKey := "test-master-key"

	keyPair, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://api.z.ai",
		EncryptedKey: keyPair.Ciphertext,
		KeyNonce:     keyPair.Nonce,
		KeySalt:      keyPair.Salt,
	}

	ctx := context.Background()
	quota, err := svc.GetZAICodingQuota(ctx, provider, masterKey)
	if err != nil {
		t.Fatalf("GetZAICodingQuota failed: %v", err)
	}

	if quota == nil {
		t.Fatal("expected non-nil quota")
	}

	if len(quota.Data.Limits) == 0 {
		t.Error("expected at least one limit in quota response")
	}

	t.Logf("ZAI Coding quota test passed - %d limits found", len(quota.Data.Limits))
}
