package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// Gemini text-to-speech: /v1/audio/speech on a provider whose TTS models are
// served by generateContent alone. Google's OpenAI-compatibility layer has no
// speech route, so the request is translated to the native call and the audio
// part it answers with is delivered as the wav or pcm the client asked for.

// speechEndpointPath is the pass-through endpoint the adapter serves.
const speechEndpointPath = "/audio/speech"

// speechBodyCap bounds the generateContent answer read for a speech request:
// base64 of 16-bit 24 kHz mono is 64 KB per second of speech, so the cap holds
// several minutes.
const speechBodyCap = 32 << 20

// isGeminiSpeechAttempt reports a speech request landing on a provider whose
// TTS models are served by Google's native route: Google AI Studio and Vertex
// AI express. A model whose discovered output modalities name no audio keeps
// the pass-through; a model discovery left without modalities is treated as
// audio-capable.
func isGeminiSpeechAttempt(st *requestState, providerType, outputModalities string) bool {
	if st.endpointPath != speechEndpointPath || (providerType != "google" && providerType != "vertex-express") {
		return false
	}
	declared := declaredModalities(outputModalities)
	return len(declared) == 0 || slices.Contains(declared, "audio")
}

// speechRequestRefusal is the reason a Gemini speech attempt cannot serve the
// request as asked, or empty: a response format the model does not produce
// (the compressed ones need an encoder the gateway does not carry), or a
// request the translation cannot read. Checked before the attempt is made, so
// the request can fail over to a candidate that can serve it.
func speechRequestRefusal(st *requestState, candidate modelCandidate) string {
	if !isGeminiSpeechAttempt(st, provider.TypeOf(candidate.provider), candidate.model.OutputModalities) {
		return ""
	}
	if _, _, _, err := gemini.TranslateSpeechRequest(st.bodyBytes); err != nil {
		return strings.TrimPrefix(err.Error(), "gemini: ")
	}
	return ""
}

// geminiRequestRefusal is the reason a Gemini speech or transcription
// attempt cannot serve the request as asked, or empty.
func geminiRequestRefusal(st *requestState, candidate modelCandidate) string {
	if reason := speechRequestRefusal(st, candidate); reason != "" {
		return reason
	}
	return transcriptionRequestRefusal(st, candidate)
}

// refuseGeminiRequest answers a speech or transcription request with a 400
// before any attempt is made when every candidate refuses it. Reports whether
// the request was answered.
func (h *Handler) refuseGeminiRequest(w http.ResponseWriter, st *requestState, candidates []modelCandidate) bool {
	if (st.endpointPath != speechEndpointPath && st.endpointPath != transcriptionEndpointPath) || len(candidates) == 0 {
		return false
	}
	reason := ""
	for _, c := range candidates {
		reason = geminiRequestRefusal(st, c)
		if reason == "" {
			return false
		}
	}
	h.failRequest(st.logData, http.StatusBadRequest, KindValidation, reason, 0, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, reason, http.StatusBadRequest)
	return true
}

// buildGeminiSpeechRequest builds the native generateContent request for a
// speech attempt and records the format the answer is to take.
func (h *Handler) buildGeminiSpeechRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	body, _, format, err := gemini.TranslateSpeechRequest(st.bodyBytes)
	if err != nil {
		return nil, providerType, "", err
	}
	st.speechFormat = format
	endpoint := geminiEgressEndpoint(providerType, candidate.model.ModelID, false)
	baseURL := candidate.provider.BaseURL
	if providerType == "google" {
		baseURL = provider.GoogleNativeBaseURL(baseURL)
	}
	targetURL := util.BuildProviderTargetURL(baseURL, providerType, endpoint)
	debuglog.Info("proxy: routing speech via gemini egress adapter", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name, "format", format)
	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	setGeminiEgressAuth(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}

// serveGeminiSpeechResponse delivers a speech attempt's 2xx: the
// generateContent answer is read whole (bounded), its audio part becomes the
// wav or pcm the client asked for, and the bytes go out through the
// pass-through's binary path, which owns the commit point, the breaker credit,
// the request log and the metering. The usage the answer reported rides along
// on the request state, since a binary body carries none. An answer without
// audio (a blocked prompt, a text reply) is handed to the loop as an
// untranslatable body and fails over.
func (h *Handler) serveGeminiSpeechResponse(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, responseHeaderMs float64) candidateOutcome {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, speechBodyCap+1))
	_ = resp.Body.Close()
	if readErr == nil && len(body) > speechBodyCap {
		readErr = errSpeechBodyOversized
	}
	if readErr != nil {
		return h.rejectUntranslatableBody(st, candidate, st.logData, "gemini speech", resp.StatusCode, readErr, attempt, r)
	}
	audio, contentType, usage, err := gemini.BuildSpeechResponse(body, st.speechFormat)
	if err != nil {
		return h.rejectUntranslatableBody(st, candidate, st.logData, "gemini speech", resp.StatusCode, err, attempt, r)
	}
	st.passthroughUsage = &passthroughUsage{prompt: usage.PromptTokens, completion: usage.CompletionTokens}
	delivered := &http.Response{
		StatusCode: resp.StatusCode,
		Header:     http.Header{"Content-Type": {contentType}, "Content-Length": {strconv.Itoa(len(audio))}},
		Body:       io.NopCloser(bytes.NewReader(audio)),
	}
	h.servePassthroughResponse(w, r, st, candidate, delivered, attempt, responseHeaderMs)
	return outcomeServed
}

// errSpeechBodyOversized reports a generateContent answer past speechBodyCap:
// this gateway's own limit, never a provider fault for the breaker.
var errSpeechBodyOversized = fmt.Errorf("gemini speech response exceeds %d bytes", speechBodyCap)

// passthroughUsage is the usage a translating adapter read off a provider's
// answer before re-shaping it into a body that carries none. The binary
// pass-through path meters from it in place of the estimate.
type passthroughUsage struct {
	prompt, completion int
}
