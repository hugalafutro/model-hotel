package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
)

// A 200 whose body breaks off mid-read is reported by its read error, not
// mistaken for the oversized-body cause or passed on as an empty answer.
func TestTestModel_TruncatedUpstreamBody(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/chat/completions") {
			return
		}
		// Promise more than is sent: the server closes the connection after the
		// handler returns, and the client's read ends in an unexpected EOF.
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[`))
	}))
	defer mockServer.Close()

	origTransport := h.testModelTransport
	h.testModelTransport = &http.Transport{}
	defer func() { h.testModelTransport = origTransport }()

	providerData := fmt.Sprintf(`{"name": "truncated-body-provider-%s", "base_url": "%s", "api_key": "sk-test-key"}`, uuid.New().String()[:8], mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %d %s", rec.Code, rec.Body.String())
	}
	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider response: %v", err)
	}
	modelID := uuid.New().String()
	if _, err := h.Pool().Pool().Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true); err != nil {
		t.Fatalf("failed to insert model: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var testResp TestModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("failed to parse test response: %v", err)
	}
	if !strings.HasPrefix(testResp.Error, "failed to read upstream response: ") || !strings.Contains(testResp.Error, "unexpected EOF") {
		t.Errorf("error = %q, want the read failure with its cause", testResp.Error)
	}
	if testResp.Success {
		t.Error("an unread response must not be reported as a successful test")
	}
}

// The egress path reads a 200 itself to translate it, so it reports the same
// failures: an oversized body as the sentence the OpenAI-shaped path uses, a
// broken-off body by its read error, and a body the translation cannot read
// by the translation's error. Each leaves an empty body behind for the caller.
func TestDoTestModelEgressRequest_BodyFailures(t *testing.T) {
	h := newTestHandler(t)
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)

	for _, tc := range []struct {
		name     string
		respond  func(w http.ResponseWriter)
		checkErr func(t *testing.T, err error)
	}{
		{"oversized", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			chunk := bytes.Repeat([]byte("a"), 1<<20)
			for range 9 { // 9 MiB, past httpx.MaxUpstreamBody
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		}, func(t *testing.T, err error) {
			if err == nil || err.Error() != "upstream response too large" {
				t.Errorf("err = %v, want the oversized-body sentence", err)
			}
		}},
		{"truncated", func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"content":[`))
		}, func(t *testing.T, err error) {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("err = %v, want the unexpected EOF of the broken-off read", err)
			}
		}},
		{"untranslatable", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		}, func(t *testing.T, err error) {
			// The builder's own parse failure, which nothing else on this path
			// produces.
			if err == nil || !strings.HasPrefix(err.Error(), "anthropicegress: invalid upstream response: ") {
				t.Errorf("err = %v, want the translation's invalid-upstream-response error", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				tc.respond(w)
			}))
			defer srv.Close()

			resp, err := h.doTestModelEgressRequest(context.Background(), srv.Client(), srv.URL, "anthropic-messages", "claude-sonnet-4-5", "k", body,
				anthropicegress.TranslateRequest, anthropicegress.BuildChatCompletion)
			tc.checkErr(t, err)
			if resp == nil {
				t.Fatal("resp = nil, want the upstream response with its status")
			}
			if left, _ := io.ReadAll(resp.Body); len(left) != 0 || resp.StatusCode != http.StatusOK {
				t.Errorf("resp = %d with %d body bytes, want 200 and an empty body", resp.StatusCode, len(left))
			}
		})
	}
}
