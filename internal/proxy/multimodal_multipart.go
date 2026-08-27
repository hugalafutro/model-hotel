package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// multipartPart is one parsed part of a multipart/form-data body, retained so
// the form can be rebuilt per failover candidate with the model substituted.
type multipartPart struct {
	fieldName   string
	fileName    string
	contentType string
	data        []byte
}

// parseMultipartParts decomposes a multipart body into its parts and returns
// them together with the value of the `model` form field (empty if absent).
func parseMultipartParts(body []byte, boundary string) ([]multipartPart, string, error) {
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var parts []multipartPart
	model := ""
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		data, err := io.ReadAll(p)
		_ = p.Close()
		if err != nil {
			return nil, "", err
		}
		part := multipartPart{
			fieldName:   p.FormName(),
			fileName:    p.FileName(),
			contentType: p.Header.Get("Content-Type"),
			data:        data,
		}
		if part.fieldName == "model" && part.fileName == "" {
			model = strings.TrimSpace(string(data))
		}
		parts = append(parts, part)
	}
	return parts, model, nil
}

// multipartQuoteEscaper escapes quotes and backslashes in multipart header
// values, matching the escaping used by mime/multipart.Writer.
var multipartQuoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// rebuildMultipartBody reassembles a multipart/form-data body from parsed
// parts with the `model` field replaced by the resolved upstream model ID.
// A fresh boundary is generated; the returned content type carries it.
func rebuildMultipartBody(parts []multipartPart, resolvedModelID string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, part := range parts {
		data := part.data
		if part.fieldName == "model" && part.fileName == "" {
			data = []byte(resolvedModelID)
		}
		if part.fileName == "" && part.contentType == "" {
			if err := mw.WriteField(part.fieldName, string(data)); err != nil {
				return nil, "", err
			}
			continue
		}
		hdr := make(textproto.MIMEHeader)
		disposition := fmt.Sprintf(`form-data; name="%s"`, multipartQuoteEscaper.Replace(part.fieldName))
		if part.fileName != "" {
			disposition += fmt.Sprintf(`; filename="%s"`, multipartQuoteEscaper.Replace(part.fileName))
		}
		hdr.Set("Content-Disposition", disposition)
		if part.contentType != "" {
			hdr.Set("Content-Type", part.contentType)
		}
		pw, err := mw.CreatePart(hdr)
		if err != nil {
			return nil, "", err
		}
		if _, err := pw.Write(data); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// newMultipartBodyBuilder returns a makeUpstreamBody fn that rebuilds the
// multipart form with the resolved model ID, memoizing the last build:
// failover-group candidates frequently resolve to the same upstream model ID
// (the same model offered by different providers), so the expensive full
// re-serialization of the upload happens once per distinct model instead of
// once per attempt.
func newMultipartBodyBuilder(parts []multipartPart) func(string) ([]byte, string, error) {
	var lastModelID, lastContentType string
	var lastBody []byte
	return func(resolvedModelID string) ([]byte, string, error) {
		if lastBody != nil && resolvedModelID == lastModelID {
			return lastBody, lastContentType, nil
		}
		body, contentType, err := rebuildMultipartBody(parts, resolvedModelID)
		if err != nil {
			return nil, "", err
		}
		lastModelID, lastBody, lastContentType = resolvedModelID, body, contentType
		return body, contentType, nil
	}
}

// multipartPromptTextBytes sizes the text a multipart request carries: the form
// fields, never the uploaded file.
//
// A part with a filename is the payload (audio to transcribe, an image to edit)
// and is orders of magnitude larger than the tokens it costs, so measuring it
// would invent a colossal charge -- the same reason promptTextBytes skips
// image_url parts and passthroughPromptTextBytes refuses to size an upload. The
// text fields are the prompt: an image edit's "prompt" is the same string the
// JSON image endpoint sends, and it was being counted as zero purely because it
// arrived as form data.
//
// The "model" field is excluded: it is routing metadata, not prompt text, and
// it is already recorded separately on the log row.
func multipartPromptTextBytes(parts []multipartPart) int {
	n := 0
	for _, p := range parts {
		if p.fileName != "" || p.fieldName == "model" {
			continue
		}
		n += len(p.data)
	}
	return n
}

// ingestMultipartRequest performs phase A for multipart endpoints: read the
// (middleware-cached) body, parse the multipart form, extract the `model`
// field, create the early "pending" request-log entry, publish the
// request.started event, and run the early-failure guards.
//
// On success it returns the request state plus the parsed parts (for the
// per-candidate rebuild). On any guard failure it records the failure, writes
// the OpenAI error response, and returns (nil, nil, false).
func (h *Handler) ingestMultipartRequest(w http.ResponseWriter, r *http.Request, endpointType string) (*requestState, []multipartPart, bool) {
	startTime := time.Now()

	// Create the log entry early so early-return paths can record failures.
	// modelID gets updated after the multipart form is parsed.
	logData, vkHash := h.newPendingRequestLog(r, endpointType, "", false)

	// Multipart bodies are never buffered by streamingAwareTimeout (the
	// middleware passes them through unread, post-auth memory only), so the
	// body is always read here.
	parseStart := time.Now()
	bodyBytes, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		debuglog.Warn("proxy: failed to read multipart request body", "error", err)
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, KindValidation, "failed to read request body", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
		return nil, nil, false
	}

	mediaType, ctParams, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") || ctParams["boundary"] == "" {
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, KindValidation, "Content-Type must be multipart/form-data with a boundary", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "Content-Type must be multipart/form-data with a boundary", http.StatusBadRequest)
		return nil, nil, false
	}

	parts, reqModel, err := parseMultipartParts(bodyBytes, ctParams["boundary"])
	if err != nil {
		debuglog.Warn("proxy: failed to parse multipart form", "error", err)
		publishRequestStartedEvent(logData)
		h.failRequest(logData, 400, KindValidation, "invalid multipart form", 0, startTime, 0, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "invalid multipart form", http.StatusBadRequest)
		return nil, nil, false
	}
	parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

	logData.modelID = reqModel
	publishRequestStartedEvent(logData)

	if reqModel == "" {
		h.failRequest(logData, 400, KindValidation, "model is required", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return nil, nil, false
	}

	debuglog.Info("proxy: multipart request start", "client_ip", clientip.From(r), "endpoint", endpointType, "model", reqModel, "key", logData.virtualKeyName, "parts", len(parts))

	// bodyBytes stays nil: the parsed parts are the upstream-body source for
	// multipart requests (via makeUpstreamBody), so retaining the raw body
	// would pin a redundant full copy of the upload for the request lifetime.
	// Size the text form fields, skipping the upload itself. Without this
	// logData.promptTextBytes stays at its zero value for every multipart
	// request, so the metering estimate downstream silently charges nothing --
	// the same no-op that the chat-only sizer produced for the JSON families.
	logData.promptTextBytes = multipartPromptTextBytes(parts)

	return &requestState{
		startTime: startTime,
		reqModel:  reqModel,
		vkHash:    vkHash,
		parseMs:   parseMs,
		logData:   logData,
	}, parts, true
}
