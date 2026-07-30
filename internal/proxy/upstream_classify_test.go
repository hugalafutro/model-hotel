package proxy

import (
	"strings"
	"testing"
)

// TestClassifyUpstreamError_RealProviderBodies pins the classifier against the
// exact upstream payloads that motivated it. Every body here was captured from a
// real provider response during the 2026-07-30 catalog audit, where all of them
// reached the caller as an indistinguishable "upstream provider returned HTTP
// 400" and had to be told apart by hand from request_logs.error_message.
func TestClassifyUpstreamError_RealProviderBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   ErrorKind
	}{
		{
			name:   "google retired model, still in its own /models listing",
			status: 404,
			body:   `{"error":{"code":404,"message":"This model models/gemini-2.0-flash is no longer available. Please update your code to use a newer model."}}`,
			want:   KindProviderModelGone,
		},
		{
			name:   "google generateContent on an unsupported model",
			status: 404,
			body:   `{"error":{"code":404,"message":"models/gemini-embedding-001 is not found for API version v1main, or is not supported for generateContent."}}`,
			want:   KindProviderModelGone,
		},
		{
			name:   "opencode zen unsupported model",
			status: 401,
			body:   `{"type":"error","error":{"type":"ModelError","message":"Model gemini-3-pro is not supported"}}`,
			want:   KindProviderModelGone,
		},
		{
			name:   "opencode zen model off the full model list",
			status: 400,
			body:   `{"type":"invalid_request_error","message":"Error from provider (Console): Model claude-sonnet-4 is not supported on the full model list."}`,
			want:   KindProviderModelGone,
		},
		{
			name:   "xai model not found",
			status: 400,
			body:   `"Model not found: grok-imagine-image"`,
			want:   KindProviderModelGone,
		},
		{
			name:   "zai coding plan, model outside the subscription",
			status: 429,
			body:   `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`,
			want:   KindProviderNotEntitled,
		},
		{
			name:   "gemini native route rejecting an openai-shaped body",
			status: 400,
			body:   `{"code":400,"message":"Error from provider (Console): Invalid JSON request body: Missing key\n  at [\"contents\"]","status":"INVALID_ARGUMENT"}`,
			want:   KindProviderBadRequest,
		},
		{
			name:   "transient aggregator backend fault stays the default",
			status: 400,
			body:   `{"message":"Error from provider (Console): Upstream request failed","type":"server_error"}`,
			want:   KindProviderError,
		},
		{
			name:   "plain 5xx stays the default",
			status: 503,
			body:   `service unavailable`,
			want:   KindProviderError,
		},
		{
			name:   "empty body stays the default",
			status: 502,
			body:   ``,
			want:   KindProviderError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, reason := classifyUpstreamError(tc.status, tc.body)
			if got != tc.want {
				t.Errorf("classifyUpstreamError(%d, %q) kind = %q, want %q", tc.status, tc.body, got, tc.want)
			}
			if reason == "" {
				t.Error("reason must never be empty: it is the only thing the caller is told")
			}
		})
	}
}

// TestClassifyUpstreamError_ReasonNeverEchoesBody is the privacy guarantee: the
// reason handed to a caller is gateway-authored static text, so a provider that
// quotes our request back inside an error can never leak it onward.
func TestClassifyUpstreamError_ReasonNeverEchoesBody(t *testing.T) {
	t.Parallel()

	secret := "what-is-the-capital-of-france"
	bodies := []string{
		`{"error":{"message":"content rejected: ` + secret + `"}}`,
		`{"error":{"message":"Invalid JSON request body: Missing key at [\"contents\"] near ` + secret + `"}}`,
		`{"error":{"message":"Model ` + secret + ` is not supported"}}`,
		`{"error":{"message":"Insufficient balance. Prompt was: ` + secret + `"}}`,
	}

	for _, body := range bodies {
		_, reason := classifyUpstreamError(400, body)
		if strings.Contains(reason, secret) {
			t.Errorf("reason leaked the upstream body: %q", reason)
		}
		msg := upstreamClientMessage("Some Provider", 400, reason)
		if strings.Contains(msg, secret) {
			t.Errorf("client message leaked the upstream body: %q", msg)
		}
	}
}

func TestUpstreamClientMessage(t *testing.T) {
	t.Parallel()

	got := upstreamClientMessage("Z.ai Coding Plan", 429, "the provider rejected this request for billing or plan reasons")
	for _, want := range []string{"Z.ai Coding Plan", "upstream HTTP 429", "billing or plan"} {
		if !strings.Contains(got, want) {
			t.Errorf("upstreamClientMessage() = %q, want it to contain %q", got, want)
		}
	}

	// An unattributed failure must still say something useful.
	if got := upstreamClientMessage("", 502, "the provider failed to serve this request"); got != "the provider failed to serve this request" {
		t.Errorf("unattributed message = %q", got)
	}
}

// TestClassifyUpstreamError_NarrowPatterns pins the three misclassification
// risks raised in review. Each one only ever affected the recorded error_kind,
// the Prometheus label and the client message — routing stays status-driven —
// but a wrong kind sends an operator to the wrong conclusion, and
// provider_model_gone additionally accrues a strike towards auto-disabling a
// live model.
func TestClassifyUpstreamError_NarrowPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   ErrorKind
	}{
		{
			// A bare "billing" substring used to match here, so a transient
			// fault naming a billing subsystem was recorded as a permanent
			// entitlement failure.
			name:   "transient fault naming a billing subsystem is not an entitlement failure",
			status: 500,
			body:   `{"error":{"message":"billing_engine_timeout: upstream unavailable","type":"server_error"}}`,
			want:   KindProviderError,
		},
		{
			name:   "402 is entitlement regardless of body",
			status: 402,
			body:   `{"error":{"message":"payment required"}}`,
			want:   KindProviderNotEntitled,
		},
		{
			// "does not exist" is too generic alone: a provider erroring about
			// some other entity must not accrue gone-strikes against a live
			// model.
			name:   "does-not-exist about a non-model entity is not a dead model",
			status: 404,
			body:   `{"error":{"message":"the requested conversation does not exist"}}`,
			want:   KindProviderError,
		},
		{
			name:   "does-not-exist about a model still classifies as gone",
			status: 404,
			body:   "{\"error\":{\"message\":\"The model `gpt-4.5-preview` does not exist or you do not have access to it\"}}",
			want:   KindProviderModelGone,
		},
		{
			name:   "genuine insufficient balance still classifies",
			status: 429,
			body:   `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`,
			want:   KindProviderNotEntitled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(tc.status, tc.body); got != tc.want {
				t.Errorf("classifyUpstreamError(%d, %q) = %q, want %q", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// TestClassifyUpstreamError_CapabilityErrorsAreNotDeadModels is the negative
// half of the model-gone patterns, added after review found that a bare
// "is not supported" match would retire a healthy model.
//
// These bodies all describe a capability the provider refused — a parameter, an
// operation, a region, an account feature — on a model that is very much alive.
// Classifying any of them as provider_model_gone would accrue strikes and, on
// the third, call SetEnabled(false) and pull a working model out of routing.
func TestClassifyUpstreamError_CapabilityErrorsAreNotDeadModels(t *testing.T) {
	t.Parallel()

	bodies := []struct {
		name string
		body string
	}{
		{"rejected parameter", `{"error":{"message":"Parameter 'temperature' is not supported with this model","type":"invalid_request_error"}}`},
		{"rejected parameter, model trailing", `{"error":{"message":"top_p is not supported for this model"}}`},
		{"unsupported operation", `{"error":{"message":"This operation is not supported","type":"invalid_request_error"}}`},
		{"region restriction", `{"error":{"message":"This feature is not supported in your region"}}`},
		{"account capability", `{"error":{"message":"Streaming is not supported on your current plan"}}`},
		{"tooling capability", `{"error":{"message":"Structured outputs is not supported"}}`},
		{"other entity missing", `{"error":{"message":"the requested conversation does not exist"}}`},
		{"file missing", `{"error":{"message":"The uploaded file does not exist"}}`},
		{"endpoint retired", `{"error":{"message":"This API version is no longer available"}}`},
	}

	for _, tc := range bodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := classifyUpstreamError(400, tc.body)
			if got == KindProviderModelGone {
				t.Errorf("capability error classified as a dead model, which would auto-disable it: %q", tc.body)
			}
		})
	}
}

// TestClassifyUpstreamError_ModelGoneStillMatches guards the other direction:
// tightening the patterns must not stop the real retired-model payloads from
// classifying. Each of these was captured from a live provider.
func TestClassifyUpstreamError_ModelGoneStillMatches(t *testing.T) {
	t.Parallel()

	bodies := []struct {
		name string
		body string
	}{
		{"google retired", `{"error":{"code":404,"message":"This model models/gemini-2.0-flash is no longer available. Please update your code to use a newer model."}}`},
		{"google generateContent", `{"error":{"code":404,"message":"models/gemini-embedding-001 is not found for API version v1main, or is not supported for generateContent."}}`},
		{"zen model error", `{"type":"error","error":{"type":"ModelError","message":"Model gemini-3-pro is not supported"}}`},
		{"zen full model list", `{"type":"invalid_request_error","message":"Error from provider (Console): Model claude-sonnet-4 is not supported on the full model list."}`},
		{"xai not found", `"Model not found: grok-imagine-image"`},
		{"openai does not exist", "{\"error\":{\"message\":\"The model `gpt-4.5-preview` does not exist or you do not have access to it\"}}"},
	}

	for _, tc := range bodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(404, tc.body); got != KindProviderModelGone {
				t.Errorf("real retired-model payload no longer classifies: got %q for %q", got, tc.body)
			}
		})
	}
}

// TestClassifyUpstreamError_OperationRefusalsAreNotDeadModels covers the shape
// that survived the previous tightening: the model IS named before the phrase,
// so the trailing-model guard does not catch it, but the provider is only
// refusing one capability. Three of these would call SetEnabled(false) on a
// model that is serving every other request perfectly.
func TestClassifyUpstreamError_OperationRefusalsAreNotDeadModels(t *testing.T) {
	t.Parallel()

	bodies := []struct {
		name string
		body string
	}{
		{"operation", `{"error":{"message":"Model gpt-5.6-sol is not supported for this operation"}}`},
		{"endpoint", `{"error":{"message":"Model claude-opus-5 is not supported for this endpoint"}}`},
		{"method", `{"error":{"message":"Model gemini-3.6-flash is not supported for this method"}}`},
		{"api route", `{"error":{"message":"Model glm-5.2 is not supported on this route"}}`},
		{"request type", `{"error":{"message":"Model kimi-k3 is not supported for this request type"}}`},
		{"region", `{"error":{"message":"Model minimax-m3 is not supported in your region"}}`},
		{"plan", `{"error":{"message":"Model grok-4.5 is not supported on your plan"}}`},
		{"account tier", `{"error":{"message":"Model deepseek-v4-pro is not supported for your account"}}`},
		{"availability qualifier", `{"error":{"message":"Model qwen3.6-plus is no longer available for this endpoint"}}`},
	}

	for _, tc := range bodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := classifyUpstreamError(400, tc.body)
			if got == KindProviderModelGone {
				t.Errorf("capability refusal classified as a dead model, which would auto-disable a live one: %q", tc.body)
			}
		})
	}
}
