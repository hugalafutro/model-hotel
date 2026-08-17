package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ErrLocalServerUnreachable reports that none of the probes got an answer:
// nothing is listening, the host is wrong, or the network is blocking it.
var ErrLocalServerUnreachable = errors.New("local server unreachable")

// LocalServerIdentity is what a probe learned about the server behind a base
// URL. Type is "" when the server answered but is none of the families Model
// Hotel knows how to drive natively.
type LocalServerIdentity struct {
	Type    string
	Version string
}

// localProbeTimeout bounds a single fingerprint request. The operator is
// waiting on the add dialog, and a self-hosted server is on the LAN or the
// same box, so a slow answer is a wrong answer.
const localProbeTimeout = 5 * time.Second

// IdentifyLocalServer asks the server behind baseURL which family it belongs
// to, using one identifying endpoint per family. It is used as a gate when a
// provider is added or its URL changed, never as a way to guess a type that
// was not chosen.
//
// Each check fails closed on the body, not the status: LM Studio answers
// unknown routes with HTTP 200 and an {"error": ...} body, so a status-only
// check would identify it as whatever was asked first.
//
// A returned error means no probe reached the server. A nil error with an
// empty Type means the server answered but matched no fingerprint.
func (d *DiscoveryService) IdentifyLocalServer(ctx context.Context, baseURL, apiKey string) (LocalServerIdentity, error) {
	origin := localServerOrigin(baseURL)
	reached := false

	// KoboldCPP: /api/extra/version reports the product name outright.
	if body, ok, err := d.probeLocal(ctx, origin+"/api/extra/version", apiKey); err == nil {
		reached = true
		if ok {
			var v struct {
				Result  string `json:"result"`
				Version string `json:"version"`
			}
			if json.Unmarshal(body, &v) == nil && strings.EqualFold(v.Result, "koboldcpp") {
				return LocalServerIdentity{Type: "koboldcpp", Version: v.Version}, nil
			}
		}
	}

	// LM Studio: the native REST listing, which nothing else serves.
	if body, ok, err := d.probeLocal(ctx, origin+"/api/v0/models", apiKey); err == nil {
		reached = true
		if ok && isLMStudioModelListing(body) {
			return LocalServerIdentity{Type: "lmstudio"}, nil
		}
	}

	// Ollama: the native tag listing.
	if body, ok, err := d.probeLocal(ctx, origin+"/api/tags", apiKey); err == nil {
		reached = true
		if ok && isOllamaTagListing(body) {
			return LocalServerIdentity{Type: "ollama"}, nil
		}
	}

	if !reached {
		return LocalServerIdentity{}, ErrLocalServerUnreachable
	}
	return LocalServerIdentity{}, nil
}

// probeLocal performs one fingerprint GET. It reports the body, whether the
// response was a 200 worth inspecting, and an error only when the server could
// not be reached at all (so a 404 still counts as "the host is alive").
//
// The key is sent for the same reason discovery sends it: a self-hosted server
// can sit behind a password or an authenticating proxy, and an unauthenticated
// probe would see a 401 and conclude the server is not what it says it is.
func (d *DiscoveryService) probeLocal(ctx context.Context, endpoint, apiKey string) ([]byte, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, localProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, false, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Debug("provider: local server probe failed", "url", endpoint, "error", err)
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Fingerprint bodies are tiny; a large one is not one of ours.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, nil
	}
	return body, resp.StatusCode == http.StatusOK, nil
}

// isLMStudioModelListing reports whether body is LM Studio's /api/v0/models
// response. An LM Studio with nothing downloaded still answers with an empty
// data array, so an empty list counts as long as the body is not an error.
func isLMStudioModelListing(body []byte) bool {
	var listing struct {
		Error json.RawMessage `json:"error"`
		Data  []struct {
			CompatibilityType string `json:"compatibility_type"`
			MaxContextLength  *int   `json:"max_context_length"`
			Publisher         string `json:"publisher"`
			Arch              string `json:"arch"`
			Type              string `json:"type"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &listing) != nil || len(listing.Error) > 0 {
		return false
	}
	if !strings.Contains(string(body), `"data"`) {
		return false
	}
	if len(listing.Data) == 0 {
		return true
	}
	first := listing.Data[0]
	return first.CompatibilityType != "" || first.MaxContextLength != nil ||
		first.Publisher != "" || first.Arch != "" || first.Type != ""
}

// isOllamaTagListing reports whether body is Ollama's /api/tags response.
func isOllamaTagListing(body []byte) bool {
	var listing struct {
		Error  json.RawMessage `json:"error"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &listing) != nil || len(listing.Error) > 0 {
		return false
	}
	return strings.Contains(string(body), `"models"`)
}
