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

// Gemini speech-to-text: /v1/audio/transcriptions on a provider whose hearing
// models are served by generateContent alone. Google's OpenAI-compatibility
// layer has no transcription route (gemini-3.5-transcribe answers 404 there,
// and null through chat), so the upload is translated to the native call and
// the text it answers with is delivered as the json or text the client asked
// for. The twin of gemini_speech.go, one direction over.

// transcriptionEndpointPath is the pass-through endpoint the adapter serves.
const transcriptionEndpointPath = "/audio/transcriptions"

// transcriptionBodyCap bounds the generateContent answer read for a
// transcription: an hour of speech is well under a megabyte of text.
const transcriptionBodyCap = 4 << 20

// isGeminiTranscriptionAttempt reports a transcription request landing on a
// provider whose hearing models are served by Google's native route: Google
// AI Studio and Vertex AI express. A model whose discovered input modalities
// name no audio keeps the pass-through; a model discovery left without
// modalities is treated as hearing.
func isGeminiTranscriptionAttempt(st *requestState, providerType, inputModalities string) bool {
	if st.endpointPath != transcriptionEndpointPath || (providerType != "google" && providerType != "vertex-express") {
		return false
	}
	declared := declaredModalities(inputModalities)
	return len(declared) == 0 || slices.Contains(declared, "audio")
}

// transcriptionRequestFromParts lifts what the adapter needs off the parsed
// multipart form: the upload and the fields the translation reads. A model
// with a transcribe segment in its id (gemini-3.5-transcribe) is a dedicated
// transcriber and is sent the audio alone.
func transcriptionRequestFromParts(parts []multipartPart, modelID string) gemini.TranscriptionRequest {
	req := gemini.TranscriptionRequest{Dedicated: slices.Contains(util.ModelIDSegments(modelID), "transcribe")}
	for _, p := range parts {
		switch p.fieldName {
		case "file":
			req.Audio, req.FileName, req.ContentType = p.data, p.fileName, p.contentType
		case multipartPromptField:
			req.Prompt = string(p.data)
		case "response_format":
			req.ResponseFormat = string(p.data)
		case "temperature":
			req.Temperature = string(p.data)
		case "stream":
			req.Stream = strings.EqualFold(strings.TrimSpace(string(p.data)), "true")
		}
	}
	return req
}

// transcriptionRequestRefusal is the reason a Gemini transcription attempt
// cannot serve the request as asked, or empty: a response format the adapter
// does not produce (the timestamped ones), a streaming request, or an upload
// whose container cannot be named. Checked before the attempt is made, so the
// request can fail over to a candidate that can serve it. The check reads the
// form's fields only; the upload is encoded once, when the attempt is built.
func transcriptionRequestRefusal(st *requestState, candidate modelCandidate) string {
	if !isGeminiTranscriptionAttempt(st, provider.TypeOf(candidate.provider), candidate.model.InputModalities) {
		return ""
	}
	if _, _, err := gemini.ValidateTranscriptionRequest(transcriptionRequestFromParts(st.multipartParts, candidate.model.ModelID)); err != nil {
		return strings.TrimPrefix(err.Error(), "gemini: ")
	}
	return ""
}

// buildGeminiTranscriptionRequest builds the native generateContent request
// for a transcription attempt and records the format the answer is to take.
func (h *Handler) buildGeminiTranscriptionRequest(ctx context.Context, st *requestState, candidate modelCandidate, providerType string) (*http.Request, string, string, error) {
	body, format, err := gemini.TranslateTranscriptionRequest(transcriptionRequestFromParts(st.multipartParts, candidate.model.ModelID))
	if err != nil {
		return nil, providerType, "", err
	}
	st.transcriptionFormat = format
	endpoint := geminiEgressEndpoint(providerType, candidate.model.ModelID, false)
	baseURL := candidate.provider.BaseURL
	if providerType == "google" {
		baseURL = provider.GoogleNativeBaseURL(baseURL)
	}
	targetURL := util.BuildProviderTargetURL(baseURL, providerType, endpoint)
	debuglog.Info("proxy: routing transcription via gemini egress adapter", "target_url", targetURL, "model", candidate.model.ModelID, "provider", candidate.provider.Name, "format", format)
	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, providerType, targetURL, err
	}
	setGeminiEgressAuth(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	return proxyReq, providerType, targetURL, nil
}

// serveGeminiTranscriptionResponse delivers a transcription attempt's 2xx:
// the generateContent answer is read whole (bounded), its text becomes the
// json or text the client asked for, and the body goes out through the
// pass-through's buffered path, which owns the commit point, the breaker
// credit, the request log and the metering. The usage the answer reported
// rides along on the request state, since the re-shaped body carries none.
// An answer without text (a blocked prompt, an empty reply) is handed to the
// loop as an untranslatable body and fails over.
func (h *Handler) serveGeminiTranscriptionResponse(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, responseHeaderMs float64) candidateOutcome {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, transcriptionBodyCap+1))
	_ = resp.Body.Close()
	if readErr == nil && len(body) > transcriptionBodyCap {
		readErr = errTranscriptionBodyOversized
	}
	if readErr != nil {
		return h.rejectUntranslatableBody(st, candidate, st.logData, "gemini transcription", resp.StatusCode, readErr, attempt, r)
	}
	out, contentType, usage, err := gemini.BuildTranscriptionResponse(body, st.transcriptionFormat)
	if err != nil {
		return h.rejectUntranslatableBody(st, candidate, st.logData, "gemini transcription", resp.StatusCode, err, attempt, r)
	}
	st.passthroughUsage = &passthroughUsage{prompt: usage.PromptTokens, completion: usage.CompletionTokens}
	delivered := &http.Response{
		StatusCode: resp.StatusCode,
		Header:     http.Header{"Content-Type": {contentType}, "Content-Length": {strconv.Itoa(len(out))}},
		Body:       io.NopCloser(bytes.NewReader(out)),
	}
	h.servePassthroughResponse(w, r, st, candidate, delivered, attempt, responseHeaderMs)
	return outcomeServed
}

// errTranscriptionBodyOversized reports a generateContent answer past
// transcriptionBodyCap: this gateway's own limit, never a provider fault for
// the breaker.
var errTranscriptionBodyOversized = fmt.Errorf("gemini transcription response exceeds %d bytes", transcriptionBodyCap)
