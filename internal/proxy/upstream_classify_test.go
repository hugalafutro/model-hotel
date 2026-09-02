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
		model  string
		want   ErrorKind
	}{
		{
			name:   "google retired model, still in its own /models listing",
			status: 404,
			body:   `{"error":{"code":404,"message":"This model models/gemini-2.0-flash is no longer available. Please update your code to use a newer model."}}`,
			model:  "gemini-2.0-flash",
			want:   KindProviderModelGone,
		},
		{
			name:   "google generateContent on an unsupported model",
			status: 404,
			body:   `{"error":{"code":404,"message":"models/gemini-embedding-001 is not found for API version v1main, or is not supported for generateContent."}}`,
			model:  "gemini-embedding-001",
			want:   KindProviderModelGone,
		},
		{
			name:   "opencode zen unsupported model",
			status: 401,
			body:   `{"type":"error","error":{"type":"ModelError","message":"Model gemini-3-pro is not supported"}}`,
			model:  "gemini-3-pro",
			want:   KindProviderModelGone,
		},
		{
			name:   "opencode zen model off the full model list",
			status: 400,
			body:   `{"type":"invalid_request_error","message":"Error from provider (Console): Model claude-sonnet-4 is not supported on the full model list."}`,
			model:  "claude-sonnet-4",
			want:   KindProviderModelGone,
		},
		{
			name:   "xai model not found",
			status: 400,
			body:   `"Model not found: grok-imagine-image"`,
			model:  "grok-imagine-image",
			want:   KindProviderModelGone,
		},
		{
			name:   "zai coding plan, model outside the subscription",
			status: 429,
			body:   `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`,
			model:  "some-model",
			want:   KindProviderNotEntitled,
		},
		{
			name:   "gemini native route rejecting an openai-shaped body",
			status: 400,
			body:   `{"code":400,"message":"Error from provider (Console): Invalid JSON request body: Missing key\n  at [\"contents\"]","status":"INVALID_ARGUMENT"}`,
			model:  "some-model",
			want:   KindProviderBadRequest,
		},
		{
			name:   "transient aggregator backend fault stays the default",
			status: 400,
			body:   `{"message":"Error from provider (Console): Upstream request failed","type":"server_error"}`,
			model:  "some-model",
			want:   KindProviderError,
		},
		{
			name:   "plain 5xx stays the default",
			status: 503,
			body:   `service unavailable`,
			model:  "some-model",
			want:   KindProviderError,
		},
		{
			name:   "empty body stays the default",
			status: 502,
			body:   ``,
			model:  "some-model",
			want:   KindProviderError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, reason := classifyUpstreamError(tc.status, tc.body, tc.model)
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
		_, reason := classifyUpstreamError(400, body, "some-model")
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
		model  string
		want   ErrorKind
	}{
		{
			// A bare "billing" substring used to match here, so a transient
			// fault naming a billing subsystem was recorded as a permanent
			// entitlement failure.
			name:   "transient fault naming a billing subsystem is not an entitlement failure",
			status: 500,
			body:   `{"error":{"message":"billing_engine_timeout: upstream unavailable","type":"server_error"}}`,
			model:  "some-model",
			want:   KindProviderError,
		},
		{
			name:   "402 is entitlement regardless of body",
			status: 402,
			body:   `{"error":{"message":"payment required"}}`,
			model:  "some-model",
			want:   KindProviderNotEntitled,
		},
		{
			// "does not exist" is too generic alone: a provider erroring about
			// some other entity must not accrue gone-strikes against a live
			// model.
			name:   "does-not-exist about a non-model entity is not a dead model",
			status: 404,
			body:   `{"error":{"message":"the requested conversation does not exist"}}`,
			model:  "some-model",
			want:   KindProviderError,
		},
		{
			name:   "does-not-exist about a model still classifies as gone",
			status: 404,
			body:   "{\"error\":{\"message\":\"The model `gpt-4.5-preview` does not exist or you do not have access to it\"}}",
			model:  "gpt-4.5-preview",
			want:   KindProviderModelGone,
		},
		{
			name:   "genuine insufficient balance still classifies",
			status: 429,
			body:   `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`,
			model:  "some-model",
			want:   KindProviderNotEntitled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(tc.status, tc.body, tc.model); got != tc.want {
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
			got, _ := classifyUpstreamError(400, tc.body, modelInBody(tc.body))
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
			if got, _ := classifyUpstreamError(404, tc.body, modelInBody(tc.body)); got != KindProviderModelGone {
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
		{"response modalities", `{"error":{"message":"Model gemini-3.6-flash is not supported for the requested response modalities."}}`},
		{"endpoint", `{"error":{"message":"Model claude-opus-5 is not supported for this endpoint"}}`},
		{"method", `{"error":{"message":"Model gemini-3.6-flash is not supported for this method"}}`},
		{"api route", `{"error":{"message":"Model glm-5.2 is not supported on this route"}}`},
		{"request type", `{"error":{"message":"Model kimi-k3 is not supported for this request type"}}`},
		{"region", `{"error":{"message":"Model minimax-m3 is not supported in your region"}}`},
		{"plan", `{"error":{"message":"Model grok-4.5 is not supported on your plan"}}`},
		{"account tier", `{"error":{"message":"Model deepseek-v4-pro is not supported for your account"}}`},
		{"availability qualifier", `{"error":{"message":"Model qwen3.6-plus is no longer available for this endpoint"}}`},
		// Providers write the qualifier out in full as readily as they write the
		// bare noun. Matching only the bare forms let these through as
		// retirements and auto-disabled a model still served for other requests.
		//
		// The ids here must be ones modelInBody knows: an unrecognised id falls
		// back to a name that is absent from the body, and the classifier then
		// declines to attribute the phrase at all — so the case would pass
		// without the veto ever being consulted.
		{"adjective before the plan", `{"error":{"message":"Model gpt-5.6-sol is not supported on your current plan"}}`},
		{"adjective before the operation", `{"error":{"message":"Model glm-5.2 is not supported for this specific operation"}}`},
		{"two adjectives", `{"error":{"message":"Model kimi-k3 is not supported on the current pricing plan"}}`},
		{"no determiner", `{"error":{"message":"Model minimax-m3 is not supported for streaming requests"}}`},
		{"plural noun", `{"error":{"message":"Model deepseek-v4-pro is not supported for enterprise accounts"}}`},
	}

	for _, tc := range bodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// modelNamedInBody, not modelInBody: every case here names a model
			// on purpose, and a fallback id would let the case pass without the
			// veto being consulted at all.
			got, _ := classifyUpstreamError(400, tc.body, modelNamedInBody(t, tc.body))
			if got == KindProviderModelGone {
				t.Errorf("capability refusal classified as a dead model, which would auto-disable a live one: %q", tc.body)
			}
		})
	}
}

// modelInBody pulls the model id a test payload is talking about, so the
// classifier is exercised the way the proxy calls it: with the id of the model
// the request actually asked for. Falls back to a placeholder for bodies that
// name no model, which is exactly the "cannot be about this model" case.
// modelInBody returns the requested-model id for a table body, falling back to a
// name that is deliberately absent when the body names no model at all — several
// cases are about bodies with no model in them ("Streaming is not supported").
//
// Use modelNamedInBody instead wherever the case is meant to exercise the
// classifier's WORDING checks. An absent id short-circuits attribution, so those
// cases would pass without the wording ever being examined.
func modelInBody(body string) string {
	for _, id := range knownTestModelIDs {
		if strings.Contains(body, id) {
			return id
		}
	}
	return "some-model"
}

var knownTestModelIDs = []string{
	"gemini-2.0-flash", "gemini-embedding-001", "gemini-3-pro", "gemini-3.6-flash",
	"claude-sonnet-4", "claude-opus-5", "grok-imagine-image", "grok-4.5",
	"gpt-4.5-preview", "gpt-5.6-sol", "glm-5.2", "kimi-k3", "minimax-m3",
	"deepseek-v4-pro", "qwen3.6-plus",
}

// modelNamedInBody is modelInBody for tables whose every case must reach the
// wording checks, and it fails rather than falling back.
//
// The fallback is a silent trap in those tables: the classifier declines to
// attribute a phrase to a model whose id is absent from the body, so a case
// written with an unrecognised id passes for that reason alone. A whole table
// can look green while testing nothing — which is exactly what happened to the
// first version of the multi-word qualifier cases below.
func modelNamedInBody(t *testing.T, body string) string {
	t.Helper()
	for _, id := range knownTestModelIDs {
		if strings.Contains(body, id) {
			return id
		}
	}
	t.Fatalf("no known model id in %q: this case would pass without reaching the wording checks", body)
	return ""
}

// TestClassifyUpstreamError_MustBeAboutTheRequestedModel is the constraint that
// replaced four rounds of trying to get the wording right: a retirement verdict
// requires the provider to be talking about THIS model.
//
// Without it, any body that merely mentioned some other missing model, or
// echoed request content that happened to contain "unknown model", counted as
// proof the requested model was retired — and three of those disable it.
func TestClassifyUpstreamError_MustBeAboutTheRequestedModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "error naming a different model is not about ours",
			body:      `{"error":{"message":"The model ` + "`gpt-4.5-preview`" + ` does not exist"}}`,
			requested: "gpt-5.6-sol",
			want:      KindProviderError,
		},
		{
			name:      "fallback chain mentioning another dead model",
			body:      `{"error":{"message":"upstream failed; note: model gemini-1.0-pro is no longer available"}}`,
			requested: "gemini-3.6-flash",
			want:      KindProviderError,
		},
		{
			name:      "provider echoing request content containing the phrase",
			body:      `{"error":{"message":"invalid request","input":"please explain what an unknown model is"}}`,
			requested: "claude-opus-5",
			want:      KindProviderError,
		},
		{
			name:      "echoed prompt saying model not found",
			body:      `{"error":{"message":"bad request","echo":"the user wrote: model not found errors are annoying"}}`,
			requested: "glm-5.2",
			want:      KindProviderError,
		},
		{
			name:      "no model id available cannot substantiate a retirement",
			body:      `{"error":{"message":"The model ` + "`gpt-4.5-preview`" + ` does not exist"}}`,
			requested: "",
			want:      KindProviderError,
		},
		// The positive control: same phrasing, and this time it IS our model.
		{
			name:      "error about the requested model does classify",
			body:      `{"error":{"message":"The model ` + "`gpt-4.5-preview`" + ` does not exist"}}`,
			requested: "gpt-4.5-preview",
			want:      KindProviderModelGone,
		},
		{
			name:      "google prefixes the id, we store it bare",
			body:      `{"error":{"code":404,"message":"This model models/gemini-2.0-flash is no longer available."}}`,
			requested: "gemini-2.0-flash",
			want:      KindProviderModelGone,
		},
		{
			name:      "we hold the prefixed id, provider reports it bare",
			body:      `{"error":{"message":"Model gemini-2.0-flash is no longer available."}}`,
			requested: "models/gemini-2.0-flash",
			want:      KindProviderModelGone,
		},
	}

	runClassifyCases(t, tests)
}

// TestClassifyUpstreamError_NearbyClauseIsNotAttribution covers the difference
// between a model being MENTIONED near a retirement phrase and being its
// subject.
//
// Proximity was doing the work: any occurrence of the requested id inside an
// 80-character window counted, so a response that names the model we asked for
// in one clause and retires a different model in the next was read as retiring
// ours. Three of those disable a model that is serving fine — the failure this
// classifier exists to avoid causing, arrived at from a new direction.
func TestClassifyUpstreamError_NearbyClauseIsNotAttribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "our model named, a different model retired in the same sentence",
			body:      `{"error":{"message":"Request for healthy-model failed because retired-model is no longer available"}}`,
			requested: "healthy-model",
			want:      KindProviderError,
		},
		{
			name:      "clause boundary between our model and the phrase",
			body:      `{"error":{"message":"healthy-model was routed; retired-model does not exist"}}`,
			requested: "healthy-model",
			want:      KindProviderError,
		},
		{
			name:      "comma-separated claims are separate claims",
			body:      `{"error":{"message":"healthy-model was selected, but retired-model does not exist"}}`,
			requested: "healthy-model",
			want:      KindProviderError,
		},
		{
			name:      "our model precedes a noun-phrase verb naming another model",
			body:      `{"error":{"message":"routing healthy-model failed. unknown model retired-model"}}`,
			requested: "healthy-model",
			want:      KindProviderError,
		},
		{
			name:      "our model in a separate JSON field from the message",
			body:      `{"model":"healthy-model","error":{"message":"retired-model does not exist"}}`,
			requested: "healthy-model",
			want:      KindProviderError,
		},
		// Positive controls: attribution must still be found when the id really
		// is the subject, including the wordier phrasings providers use.
		{
			name:      "adjacent subject still classifies",
			body:      `{"error":{"message":"retired-model is no longer available"}}`,
			requested: "retired-model",
			want:      KindProviderModelGone,
		},
		{
			name:      "a few words between subject and phrase still classifies",
			body:      `{"error":{"message":"The model ` + "`gpt-4`" + ` has been deprecated and does not exist"}}`,
			requested: "gpt-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "noun-phrase verb with the id after it still classifies",
			body:      `{"error":{"message":"unknown model openai/gpt-4"}}`,
			requested: "gpt-4",
			want:      KindProviderModelGone,
		},
	}

	runClassifyCases(t, tests)
}

// TestClassifyUpstreamError_MixedMessages covers responses that say two things
// at once, which is where a body-wide rule gets it wrong in both directions.
//
// The capability veto has to cancel the phrase it qualifies and nothing else.
// Applied to the whole body it suppressed genuine retirements that happened to
// share a response with an unrelated capability refusal — the model stays
// enabled and routable while the provider is plainly saying it is gone. Applied
// too narrowly it would let a capability refusal through as a retirement.
func TestClassifyUpstreamError_MixedMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "retirement of ours plus a capability refusal of another model",
			body:      `{"error":{"message":"Model gemini-2.0-flash does not exist. Separately, tool-only-model is not supported for this endpoint."}}`,
			requested: "gemini-2.0-flash",
			want:      KindProviderModelGone,
		},
		{
			name:      "order reversed: the refusal comes first",
			body:      `{"error":{"message":"tool-only-model is not supported for this endpoint. Also, model gemini-2.0-flash is no longer available."}}`,
			requested: "gemini-2.0-flash",
			want:      KindProviderModelGone,
		},
		{
			name:      "our model refused a capability, another model retired",
			body:      `{"error":{"message":"Model gemini-2.0-flash is not supported for this operation. Separately, gpt-4.5-preview does not exist."}}`,
			requested: "gemini-2.0-flash",
			want:      KindProviderError,
		},
		{
			name:      "both phrases about our model: the retirement still wins",
			body:      `{"error":{"message":"Model gpt-4 is not supported for this operation, and gpt-4 does not exist."}}`,
			requested: "gpt-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "capability refusal alone is still vetoed",
			body:      `{"error":{"message":"Model gpt-4 is not supported for this operation"}}`,
			requested: "gpt-4",
			want:      KindProviderError,
		},
	}

	runClassifyCases(t, tests)
}

// TestClassifyUpstreamError_OverlappingModelIDs covers the case where the error
// names a model whose id CONTAINS the requested one.
//
// Families are named by extension — gpt-4 inside gpt-4.1, gemini-3-flash inside
// gemini-3-flash-lite — so a plain substring test reads an error about the newer
// model as proof the older one is retired, and three of those disable a model
// that is serving perfectly well. The requested id has to match as a whole
// identifier, while still tolerating the path and quoting providers wrap it in.
func TestClassifyUpstreamError_OverlappingModelIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "longer sibling id is a different model",
			body:      `{"error":{"message":"The model ` + "`gpt-4.1`" + ` does not exist"}}`,
			requested: "gpt-4",
			want:      KindProviderError,
		},
		{
			name:      "suffixed variant is a different model",
			body:      `{"error":{"message":"Model gemini-3-flash-lite is no longer available."}}`,
			requested: "gemini-3-flash",
			want:      KindProviderError,
		},
		{
			name:      "tagged variant is a different model",
			body:      `{"error":{"message":"model llama3:8b does not exist"}}`,
			requested: "llama3",
			want:      KindProviderError,
		},
		{
			name:      "versioned publisher variant is a different model",
			body:      `{"error":{"message":"text-bison@001 does not exist"}}`,
			requested: "text-bison",
			want:      KindProviderError,
		},
		{
			name:      "a prefix of the requested id is not the requested id",
			body:      `{"error":{"message":"The model ` + "`gpt-4`" + ` does not exist"}}`,
			requested: "gpt-4.1",
			want:      KindProviderError,
		},
		// Positive controls: the boundary rule must not break the spellings
		// providers actually use around a genuine retirement.
		{
			name:      "backtick-quoted exact id still classifies",
			body:      `{"error":{"message":"The model ` + "`gpt-4`" + ` does not exist"}}`,
			requested: "gpt-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "path-qualified exact id still classifies",
			body:      `{"error":{"message":"publishers/google/models/gemini-2.0-flash is no longer available"}}`,
			requested: "gemini-2.0-flash",
			want:      KindProviderModelGone,
		},
		{
			name:      "vendor-prefixed exact id still classifies",
			body:      `{"error":{"message":"unknown model openai/gpt-4"}}`,
			requested: "gpt-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "the exact tagged id still classifies",
			body:      `{"error":{"message":"model llama3:8b does not exist"}}`,
			requested: "llama3:8b",
			want:      KindProviderModelGone,
		},
	}

	runClassifyCases(t, tests)
}

// runClassifyCases drives the shared table shape used by the model-identity
// tests, which assert on the kind alone.
func runClassifyCases(t *testing.T, tests []struct {
	name      string
	body      string
	requested string
	want      ErrorKind
},
) {
	t.Helper()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(400, tc.body, tc.requested); got != tc.want {
				t.Errorf("classifyUpstreamError(body=%q, requested=%q) = %q, want %q", tc.body, tc.requested, got, tc.want)
			}
		})
	}
}

// TestClassifyUpstreamError_StructuredRetirementSignals covers the payload shape
// that prose matching cannot reach at all: the retirement is a JSON field, and
// the model id is in a different one.
//
// The captured body is from issue #595 — OpenCode Zen forwarding Anthropic's own
// error verbatim for claude-sonnet-4, a model Zen lists and refuses. It accrued
// no strike and was never retired, because modelGoneAbout requires the id
// adjacent to a verb with no clause break between them, and here the two are
// separated by the comma between two JSON fields.
//
// The negatives are the point of this test rather than the positives. Each of
// them is a body that says something is missing, or names a model, or both, and
// none of them is this model being retired.
func TestClassifyUpstreamError_StructuredRetirementSignals(t *testing.T) {
	t.Parallel()

	// The exact body captured on dev, whitespace and all.
	zenBody := `{"type":"error","error":{"type":"not_found_error",` +
		`"message":"Error from provider (Anthropic): model: claude-sonnet-4-20250514"},` +
		`"request_id":"req_011CdaLiCduCzdJHbasS6cWF"}`

	tests := []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "the captured Zen body, asked for by its alias",
			body:      zenBody,
			requested: "claude-sonnet-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "the same body, asked for by the snapshot it names",
			body:      zenBody,
			requested: "claude-sonnet-4-20250514",
			want:      KindProviderModelGone,
		},
		{
			// A model-scoped code names its own subject, so no identity check
			// applies and none is wanted.
			name:      "a model-scoped code needs no prose",
			body:      `{"error":{"code":"model_not_found","message":"no such model"}}`,
			requested: "hy3-preview",
			want:      KindProviderModelGone,
		},
		{
			name:      "a model-scoped code at the top level",
			body:      `{"code":"model_not_supported","message":"unavailable"}`,
			requested: "hy3-preview",
			want:      KindProviderModelGone,
		},

		// --- negatives: something is missing, but not this model ---
		{
			// The whole reason not_found_error needs identity: it is used for
			// any absent entity.
			name:      "a not-found type naming a different model",
			body:      `{"error":{"type":"not_found_error","message":"model: some-other-model-20250101"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			name:      "a not-found type about something that is not a model",
			body:      `{"error":{"type":"not_found_error","message":"conversation conv_0192 not found"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// The model is named, but in the request echo rather than in the
			// error. Matching anywhere in the body would retire it.
			name:      "a not-found type with the model named only outside the error",
			body:      `{"model":"claude-sonnet-4","error":{"type":"not_found_error","message":"file file_01 not found"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// The same hole through a different door: the error object supplies
			// the type and says nothing else, and a top-level message mentions
			// the model. Reading a field from each level pairs two unrelated
			// statements, and the identity check meant to bound the type then
			// answers about the wrong text.
			name:      "a not-found type paired with a top-level message",
			body:      `{"message":"please retry claude-sonnet-4 later","error":{"type":"not_found_error"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// And the truncated form of it, which takes the scan rather than
			// the parse.
			name:      "a truncated not-found type paired with a top-level message",
			body:      `{"message":"claude-sonnet-4 was requested","error":{"type":"not_found_error"`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// A provider that reports at the top level still works, because
			// then the error object said nothing to prefer.
			name:      "a top-level not-found type naming the model",
			body:      `{"type":"not_found_error","message":"model: claude-sonnet-4-20250514"}`,
			requested: "claude-sonnet-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "an invalid-request type naming the model",
			body:      `{"error":{"type":"invalid_request_error","message":"model: claude-sonnet-4-20250514 rejected"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			name:      "a server-error type naming the model",
			body:      `{"error":{"type":"server_error","message":"model: claude-sonnet-4-20250514 failed"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// Providers invent codes. An unknown model-ish one is a transient
			// fault about a model that plainly still exists.
			name:      "an unknown model-ish code is not an allowlisted one",
			body:      `{"error":{"code":"model_overloaded","message":"try again"}}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// SanitizeLogBody truncates at 10 000 bytes and will cut JSON
			// mid-structure. The parse must degrade to "no structured signal"
			// rather than matching on a fragment.
			name:      "a truncated body carrying the words but no parseable structure",
			body:      `{"error":{"type":"not_found_erro`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _ := classifyUpstreamError(404, tc.body, tc.requested)
			if got != tc.want {
				t.Errorf("classifyUpstreamError(404, %q, %q) = %q, want %q", tc.body, tc.requested, got, tc.want)
			}
		})
	}
}

// TestClassifyUpstreamError_TruncatedStructuredBodyStillClassifies pins the
// fallback the parse degrades to when SanitizeLogBody has cut the document.
//
// A truncated body is the normal case for a large error, not an exotic one, and
// the signal is usually near the front while the truncation is at the end. The
// key scan is looser than the parse, which is why what it finds only counts
// against an allowlisted code or beside the model's own id.
//
// The fixture is the REAL captured body, envelope and all, because the envelope
// is what makes this hard: `{"type":"error","error":{"type":"not_found_error"`
// puts a decoy in front of the signal, and a scan of the whole document reads
// the outer one and finds "error", which retires nothing. An earlier version of
// this test dropped the outer field and passed against a scan that could not
// handle the payload it was written for.
func TestClassifyUpstreamError_TruncatedStructuredBodyStillClassifies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "the captured body, cut mid-tail",
			body: `{"type":"error","error":{"type":"not_found_error",` +
				`"message":"error from provider (anthropic): model: claude-sonnet-4-20250514"},"request_id":"req_011Cda`,
		},
		{
			name: "an error object with no envelope around it",
			body: `{"error":{"type":"not_found_error","message":"model: claude-sonnet-4-20250514"},"usage":{"input_tok`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(404, tc.body, "claude-sonnet-4"); got != KindProviderModelGone {
				t.Errorf("kind = %q, want %q: the signal was in the part that survived truncation", got, KindProviderModelGone)
			}
		})
	}
}

// TestScanStructuredError_ReadsOneObject pins the invariant structuredError
// exists to carry: the three fields come from ONE object.
//
// It has been broken three ways while this was written — a message read from
// anywhere in the body, then from another level of the same document, then from
// a SIBLING of the right object — and each time the identity check that bounds a
// generic not-found type ended up reading text no provider had attached to it.
// These are the sibling cases, which only the scan can reach: the parse rejects
// them by construction, and the scan is what runs on a truncated body, which is
// the normal case for a large error.
func TestScanStructuredError_ReadsOneObject(t *testing.T) {
	t.Parallel()

	runClassifyCases(t, []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "a sibling object's message is not this error's",
			body:      `{"error":{"type":"not_found_error"},"hint":{"message":"try claude-sonnet-4 instead"}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			name:      "nor is a later echo of the request",
			body:      `{"error":{"type":"not_found_error"},"request":{"message":"claude-sonnet-4 hello"}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// The object closes, so the walker stops there even though the id
			// appears afterwards.
			name:      "a closed error object does not reach past its brace",
			body:      `{"error":{"type":"not_found_error","message":"nothing \"here\""},"z":{"message":"claude-sonnet-4-20250514"}`,
			requested: "claude-sonnet-4",
			want:      KindProviderError,
		},
		{
			// An object nested INSIDE the error object must not end the region
			// early, or the fields after it are lost.
			name:      "a nested object does not end the region",
			body:      `{"error":{"details":{"foo":"bar"},"type":"not_found_error","message":"model: claude-sonnet-4-20250514"},"x":1`,
			requested: "claude-sonnet-4",
			want:      KindProviderModelGone,
		},
		{
			// A brace inside a message is text, not structure. A CLOSING one is
			// what proves it: counted as structure it ends the region mid-value,
			// the message loses its closing quote, and the field reader finds
			// nothing. An opening brace would not show this, since the region
			// merely runs long and the right message is still the first one in
			// it.
			name:      "a closing brace inside a message is not a delimiter",
			body:      `{"error":{"type":"not_found_error","message":"unexpected } in model claude-sonnet-4-20250514"},"y":{"message":"nope"}`,
			requested: "claude-sonnet-4",
			want:      KindProviderModelGone,
		},
		{
			// Providers quote the model name inside a message that is itself
			// JSON, so a field reader that stopped at the first quote lost the
			// id it was looking for.
			name:      "escaped quotes around the id still name it",
			body:      `{"error":{"type":"not_found_error","message":"model \"claude-sonnet-4-20250514\" not found"},"z":{"message":"nope"}`,
			requested: "claude-sonnet-4",
			want:      KindProviderModelGone,
		},
	})
}

// TestClassifyUpstreamError_ProseNamingADatedSnapshot pins that the alias rule
// reaches the prose path too.
//
// A provider that resolves the alias and says so in a sentence is making the
// same claim as one that says so in a JSON field, and identity is the thing that
// was failing in both. Handling only the structured half would have left the
// other silently unfixed, which is the shape of the bug this whole change came
// from.
//
// The negatives are the ones that matter: extending an occurrence over a dated
// tail must not extend it over anything else.
func TestClassifyUpstreamError_ProseNamingADatedSnapshot(t *testing.T) {
	t.Parallel()

	runClassifyCases(t, []struct {
		name      string
		body      string
		requested string
		want      ErrorKind
	}{
		{
			name:      "prose naming a dated snapshot of the requested alias",
			body:      "the model `gpt-4-0613` does not exist",
			requested: "gpt-4",
			want:      KindProviderModelGone,
		},
		{
			name:      "prose naming a segmented-date snapshot",
			body:      "model gpt-4-turbo-2024-04-09 is no longer available",
			requested: "gpt-4-turbo",
			want:      KindProviderModelGone,
		},
		{
			name:      "prose naming a sibling family member is still not us",
			body:      "the model `gpt-4.1` does not exist",
			requested: "gpt-4",
			want:      KindProviderError,
		},
		{
			name:      "prose naming a named variant is still not us",
			body:      "the model `gemini-3-flash-lite` does not exist",
			requested: "gemini-3-flash",
			want:      KindProviderError,
		},
		{
			name:      "prose naming a size variant is still not us",
			body:      "the model `gpt-4-32k` does not exist",
			requested: "gpt-4",
			want:      KindProviderError,
		},
	})
}

// TestVersionSuffixIdentity is the table from the plan, and the negatives in it
// are the regression that five rounds of review produced.
//
// Widening identity is the dangerous half of this change: every one of those
// rounds was about a false retirement, and the whole-identifier rule is what
// stops an error about gpt-4.1 from disabling gpt-4. The suffix rule is additive
// to that rule rather than a relaxation of it, and any doubt resolves toward
// rejecting.
func TestVersionSuffixIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested string
		body      string
		want      bool
	}{
		{"dated snapshot", "claude-sonnet-4", "model: claude-sonnet-4-20250514", true},
		{"short dated snapshot", "gpt-4", "model: gpt-4-0613", true},
		{"segmented date", "gpt-4-turbo", "model: gpt-4-turbo-2024-04-09", true},
		{"exact id", "gpt-4", "model: gpt-4", true},
		{"id at the very end", "gpt-4", "gpt-4", true},

		{"a sibling family member is not an alias", "gpt-4", "model: gpt-4.1", false},
		{"a named variant is not a date", "gemini-3-flash", "model: gemini-3-flash-lite", false},
		{"a size suffix is not a date", "gpt-4", "model: gpt-4-32k", false},
		{"too few digits to be a date", "llama-3", "model: llama-3-70", false},
		{"a shorter id is never an alias of a longer one", "gpt-4.1", "model: gpt-4", false},
		{"a variant of a snapshot is not the model", "gpt-4", "model: gpt-4-0613-preview", false},
		{"a longer id that merely starts the same", "gpt-4", "model: gpt-4o", false},
		{"the id inside a longer word", "gpt-4", "not-gpt-4", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := namesModelAllowingVersion(tc.body, tc.requested); got != tc.want {
				t.Errorf("namesModelAllowingVersion(%q, %q) = %v, want %v", tc.body, tc.requested, got, tc.want)
			}
		})
	}
}

// TestClassifyUpstreamError_ModelNamedAfterThePhrase covers the attribution
// direction real providers use in their terser messages: the verb first and the
// model id after it, as a colon-delimited tail.
//
// phraseIsAbout measures the gap on whichever side the id falls, and the two
// sides are separate branches. Only the id-before-phrase order was exercised, so
// the "does not exist: <model>" shape rested on an untested branch — a shape
// that classifies today and would silently stop if the gap measurement for it
// regressed, taking every retirement behind it with it. Failing safe is why it
// would be silent: the refusals would simply stop counting.
func TestClassifyUpstreamError_ModelNamedAfterThePhrase(t *testing.T) {
	t.Parallel()

	const modelID = "gpt-4o-mini"

	cases := []struct {
		name string
		body string
		want ErrorKind
	}{
		{
			name: "colon tail",
			body: `{"error":{"message":"does not exist: gpt-4o-mini"}}`,
			want: KindProviderModelGone,
		},
		{
			name: "phrase then id",
			body: `{"error":{"message":"model not found: gpt-4o-mini"}}`,
			want: KindProviderModelGone,
		},
		{
			// The clause break is the whole point of the gap rule: two claims,
			// and the retirement is not about the model named in the second.
			name: "clause break breaks attribution",
			body: `{"error":{"message":"does not exist. Try gpt-4o-mini instead"}}`,
			want: KindProviderError,
		},
		{
			// Far enough away that proximity is not attribution.
			name: "distant id is not the subject",
			body: `{"error":{"message":"does not exist - the upstream account has no entitlement for any model in this region so gpt-4o-mini cannot be served"}}`,
			want: KindProviderError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(404, tc.body, modelID); got != tc.want {
				t.Errorf("classify(%s) = %s, want %s", tc.body, got, tc.want)
			}
		})
	}
}

// TestClassifyUpstreamError_PunctuationDoesNotBlockAttribution pins the fix for
// a false negative that switched retirement off for whole providers.
//
// gapBindsPhrase refuses to bind when a COMPETING model id sits between the
// phrase and its subject, and looksLikeAModelID took a bare "-" for one, on the
// strength of the hyphen alone. So every provider that punctuates a refusal
// ("unknown model - gpt-4o-mini", an arrow, an em dash, a bullet) had that
// refusal read as provider_error. It failed safe, which is why it went unnoticed:
// nothing was retired, the strikes simply never accumulated.
//
// The guard cases are the other half and matter more than the fix, because the
// change can only make MORE bodies classify as gone: a gap holding a real second
// id must still block, in both word orders.
func TestClassifyUpstreamError_PunctuationDoesNotBlockAttribution(t *testing.T) {
	t.Parallel()

	const modelID = "gpt-4o-mini"

	cases := []struct {
		name string
		body string
		want ErrorKind
	}{
		{
			name: "hyphen between phrase and id",
			body: `{"error":{"message":"unknown model - gpt-4o-mini"}}`,
			want: KindProviderModelGone,
		},
		{
			name: "arrow between phrase and id",
			body: `{"error":{"message":"model not found -> gpt-4o-mini"}}`,
			want: KindProviderModelGone,
		},
		{
			name: "em dash between phrase and id",
			body: `{"error":{"message":"unknown model — gpt-4o-mini"}}`,
			want: KindProviderModelGone,
		},
		{
			name: "id before the phrase, hyphen after it",
			body: `{"error":{"message":"gpt-4o-mini - does not exist"}}`,
			want: KindProviderModelGone,
		},
		{
			// The guard: a real id in the gap is still a competing subject, and
			// binding here would retire the model named in the OTHER clause.
			name: "competing id in the gap still blocks, id last",
			body: `{"error":{"message":"does not exist for other-model-7 gpt-4o-mini"}}`,
			want: KindProviderError,
		},
		{
			name: "competing id in the gap still blocks, id first",
			body: `{"error":{"message":"healthy-model was routed but retired-x9 does not exist"}}`,
			want: KindProviderError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(404, tc.body, modelID); got != tc.want {
				t.Errorf("classify(%s) = %s, want %s", tc.body, got, tc.want)
			}
		})
	}
}

// TestLooksLikeAModelID_RequiresAnAlphanumeric pins the rule directly, because
// the classifier cases above can only reach it through a gap and would not
// distinguish "punctuation is not an id" from "the gap was short enough".
func TestLooksLikeAModelID_RequiresAnAlphanumeric(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		// Punctuation alone is never an identifier, whatever it is made of.
		" - ":  false,
		" -- ": false,
		" -> ": false,
		"-":    false,
		" ":    false,
		"":     false,
		// The tell still has to be there: a plain word is not an id.
		" model ": false,
		// Unchanged: a letter or digit plus the digit-or-dash tell.
		"gpt-4":         true,
		"llama3":        true,
		"retired-model": true,
		" 4 ":           true,
	}

	for in, want := range cases {
		if got := looksLikeAModelID(in); got != want {
			t.Errorf("looksLikeAModelID(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestClassifyUpstreamError_VendorPrefixIsNotARivalID pins the fix for a false
// negative that hid behind the vendor prefix.
//
// modelGoneAbout searches for the NORMALIZED id — the part after the last slash
// — while the body carries the id whole. So "model not found: ai21/jamba-1.7"
// matched at "jamba-1.7" and left "ai21/" sitting in the gap between the phrase
// and its subject, where looksLikeAModelID read it as a rival id and refused to
// bind.
//
// It only bit prefixes carrying a digit or a hyphen, which is what made it look
// arbitrary rather than systematic: openai/ and google/ classified, ai21/,
// meta-llama/, LLM360/ and aion-labs/ did not. Swept against the dev catalogue,
// 125 of 1141 real model ids could not be retired by either phrase-first refusal
// shape, and nothing complained because the failure is silent: no attribution,
// no strike, no retirement.
func TestClassifyUpstreamError_VendorPrefixIsNotARivalID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		modelID string
		body    string
		want    ErrorKind
	}{
		{
			name:    "digit in the vendor prefix",
			modelID: "ai21/jamba-large-1.7",
			body:    `{"error":{"message":"model not found: ai21/jamba-large-1.7"}}`,
			want:    KindProviderModelGone,
		},
		{
			name:    "hyphen in the vendor prefix",
			modelID: "meta-llama/llama-3-8b",
			body:    `{"error":{"message":"unknown model - meta-llama/llama-3-8b"}}`,
			want:    KindProviderModelGone,
		},
		{
			name:    "uppercase and digits in the vendor prefix",
			modelID: "LLM360/K2-Think",
			body:    `{"error":{"message":"model not found: LLM360/K2-Think"}}`,
			want:    KindProviderModelGone,
		},
		{
			// The control: this shape always worked, because "openai" carries
			// neither of the tells looksLikeAModelID looks for.
			name:    "plain vendor prefix still classifies",
			modelID: "openai/gpt-4o",
			body:    `{"error":{"message":"model not found: openai/gpt-4o"}}`,
			want:    KindProviderModelGone,
		},
		{
			// The guard that matters: only the prefix ATTACHED to this id is
			// absorbed. A genuine second id in the gap is still a rival subject.
			name:    "rival id before the vendor prefix still blocks",
			modelID: "ai21/jamba-large-1.7",
			body:    `{"error":{"message":"model not found for other-model-3 ai21/jamba-large-1.7"}}`,
			want:    KindProviderError,
		},
		{
			// The same guard, with the rival butted against the vendor instead
			// of spaced off it. The walk crosses characters, and every character
			// it crosses is one gapBindsPhrase never gets to see — so it may not
			// cross a period, which is a CLAUSE BREAK. Absorbing this whole run
			// as "the vendor" would delete both the boundary and the rival id
			// from the gap and bind to the wrong subject.
			name:    "rival joined to the vendor by a period still blocks",
			modelID: "ai21/jamba-large-1.7",
			body:    `{"error":{"message":"model not found for other-model-3.ai21/jamba-large-1.7"}}`,
			want:    KindProviderError,
		},
		{
			// Same, for a separator that is not a clause break. A colon belongs
			// to a model TAG ("llama3:8b"), never to a publisher, so the walk
			// stops there too and the rival stays in the gap.
			name:    "rival joined to the vendor by a colon still blocks",
			modelID: "ai21/jamba-large-1.7",
			body:    `{"error":{"message":"model not found for other-model-3:ai21/jamba-large-1.7"}}`,
			want:    KindProviderError,
		},
		{
			// A model id absent from the body cannot be attributed at all: the
			// occurrence search fails before any gap is measured. Kept as the
			// floor, NOT as evidence about vendors — the case that actually
			// exercises the vendor comparison is the one below it.
			name:    "a model we did not ask for is not ours",
			modelID: "ai21/jamba-large-1.7",
			body:    `{"error":{"message":"model not found: other-vendor/some-model-9"}}`,
			want:    KindProviderError,
		},
		{
			// ACCEPTED, and pinned here so that changing it has to be deliberate:
			// normalizeModelID reduces the request to its tail, so a refusal
			// naming a DIFFERENT vendor's copy of the same model classifies as
			// ours. Providers echo the bare name as readily as the prefixed one,
			// and this needs a provider to name a model the caller never asked
			// for. It is not new here — "azure/gpt-4o" already bound for a
			// request for "openai/gpt-4o", because a vendor with no digit and no
			// hyphen never looked like a rival id. Absorbing the prefix only
			// removes the accident that made ai21/ and meta-llama/ behave
			// differently from openai/. A classification still only nominates:
			// the probe that follows asks the live model.
			name:    "another vendor's copy of the same model still classifies",
			modelID: "ai21/jamba-large-1.7",
			body:    `{"error":{"message":"model not found: other-vendor/jamba-large-1.7"}}`,
			want:    KindProviderModelGone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, _ := classifyUpstreamError(404, tc.body, tc.modelID); got != tc.want {
				t.Errorf("classify(%s) for %s = %s, want %s", tc.body, tc.modelID, got, tc.want)
			}
		})
	}
}

// TestVendorPrefixStart pins the walk directly, including the two ways it must
// decline: no slash at all, and a slash that is punctuation rather than the end
// of a vendor segment.
func TestVendorPrefixStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		pos  int
		want int
	}{
		{name: "no prefix", body: "gpt-4o", pos: 0, want: 0},
		{name: "no slash before the match", body: "gpt-4o", pos: 4, want: 4},
		{name: "vendor prefix absorbed", body: "ai21/jamba", pos: 5, want: 0},
		{name: "prefix mid-body", body: "x: ai21/jamba", pos: 8, want: 3},
		// Only one segment: a preceding word that happens to end in a slash is
		// not part of the identifier.
		{name: "one segment only", body: "see docs/ai21/jamba", pos: 14, want: 9},
		// A bare slash is punctuation, not a vendor.
		{name: "bare slash is punctuation", body: "try /jamba", pos: 5, want: 5},
		{name: "slash at body start", body: "/jamba", pos: 1, want: 1},
		// The walk stops at anything a publisher name cannot contain, so a
		// preceding id butted straight against the vendor is not swallowed.
		// A period matters most: it is a clause break, and crossing it would
		// hide the boundary from gapBindsPhrase.
		{name: "period stops the walk", body: "other-model-3.ai21/jamba", pos: 19, want: 14},
		{name: "colon stops the walk", body: "x:ai21/jamba", pos: 7, want: 2},
		{name: "at-sign stops the walk", body: "x@ai21/jamba", pos: 7, want: 2},
		// An underscore IS a publisher character, so it is crossed: registries
		// name organisations that way and there is no id boundary there.
		{name: "underscore is part of the vendor", body: "meta_llama/x", pos: 11, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := vendorPrefixStart(tc.body, tc.pos); got != tc.want {
				t.Errorf("vendorPrefixStart(%q, %d) = %d, want %d", tc.body, tc.pos, got, tc.want)
			}
		})
	}
}
