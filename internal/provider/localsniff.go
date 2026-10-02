package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
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

// localProbeBodyCap bounds what getOnce reads: a fingerprint or a model card
// (TabbyAPI's carries the whole chat template) is well under it.
const localProbeBodyCap = 1 << 20

// IdentifyLocalServer asks the server behind baseURL which family it belongs
// to, using one identifying endpoint per family. It is used as a gate when a
// provider is added or its URL changed, never as a way to guess a type that
// was not chosen.
//
// The expected family's fingerprint is asked first, so adding a server as the
// type it really is touches only that product's own endpoint (Ollama and
// KoboldCPP also get their emulators' probes, see localServerEmulators). Asking another
// family's route first is not harmless: LM Studio logs every unknown route as
// an ERROR, so each LM Studio add left a KoboldCPP probe in its log. The other
// fingerprints still follow, in a fixed order, when the expected one does not
// match, which is how a mismatch names what did answer. An expected type that
// is no family here just keeps the fixed order.
//
// Each check fails closed on the body, not the status: LM Studio answers
// unknown routes with HTTP 200 and an {"error": ...} body, so a status-only
// check would identify it as whatever was asked first.
//
// A returned error means no probe reached the server, or an emulator check
// after the expected family's match could not be completed. A nil error with
// an empty Type means the server answered but matched no fingerprint.
func (d *DiscoveryService) IdentifyLocalServer(ctx context.Context, baseURL, apiKey, expected string) (LocalServerIdentity, error) {
	origin := localServerOrigin(baseURL)
	reached := false

	probes := localServerProbes()
	for i, p := range probes {
		if p.family == expected && i > 0 {
			// Move to the front; the rest keep their order.
			copy(probes[1:i+1], probes[:i])
			probes[0] = p
			break
		}
	}
	for _, p := range probes {
		body, status, err := d.probeLocal(ctx, origin+p.path, apiKey)
		if err != nil {
			// A status beside the error means the server answered and the
			// body did not arrive whole: the host is alive, the route is
			// not a match.
			reached = reached || status != 0
			continue
		}
		reached = true
		if status != http.StatusOK {
			continue
		}
		version, matched := p.match(body)
		if !matched {
			continue
		}
		// A matched expected family that others emulate is checked against
		// the emulators too: LocalAI and SGLang answer Ollama's /api/tags in
		// Ollama's shape, and TabbyAPI answers KoboldCPP's /api/extra/version
		// as KoboldCpp, so any of them added as the family it imitates would
		// pass as one and lose its own discovery. The extra GETs land on a
		// real Ollama as 404s it logs at its request level, and on a real
		// KoboldCPP's own serviceinfo, which names KoboldCpp and so fails the
		// TabbyAPI check.
		if p.family == expected {
			for _, q := range probes {
				if !slices.Contains(localServerEmulators[p.family], q.family) {
					continue
				}
				body, status, err := d.probeLocal(ctx, origin+q.path, apiKey)
				// The server answered a moment ago, so a transport fault or
				// a 5xx is a transient fault; saving it as the expected
				// family on an unanswered question could file an emulator
				// under the wrong type. A 4xx is the route not being there
				// (or a proxy refusing an unknown one), which is an answer.
				if err == nil && status >= http.StatusInternalServerError {
					err = fmt.Errorf("HTTP %d", status)
				}
				if err != nil {
					return LocalServerIdentity{}, fmt.Errorf("%s fingerprint could not be checked: %w", q.family, err)
				}
				if status != http.StatusOK {
					continue
				}
				if v, matched := q.match(body); matched {
					return LocalServerIdentity{Type: q.family, Version: v}, nil
				}
			}
		}
		return LocalServerIdentity{Type: p.family, Version: version}, nil
	}

	if !reached {
		return LocalServerIdentity{}, ErrLocalServerUnreachable
	}
	return LocalServerIdentity{}, nil
}

// localServerEmulators names, per family, the other families that answer its
// fingerprint too, so a server added as the emulated family is still told
// apart. LocalAI and SGLang both serve Ollama's tag listing in Ollama's shape;
// TabbyAPI impersonates KoboldCPP on its version route.
var localServerEmulators = map[string][]string{"ollama": {"localai", "sglang"}, "koboldcpp": {"tabbyapi"}}

// localServerProbe is one family's fingerprint: the endpoint that identifies
// it and the check its answer has to pass.
type localServerProbe struct {
	family string
	path   string
	match  func(body []byte) (version string, ok bool)
}

// localServerProbes lists the fingerprints in the order they are asked when no
// expected family moves one to the front. Built per call, since the caller
// reorders it.
func localServerProbes() []localServerProbe {
	return []localServerProbe{
		// TabbyAPI: its service info names the software, with or without a
		// model loaded and without a key. Asked before KoboldCPP's: TabbyAPI
		// answers /api/extra/version as KoboldCpp for Kobold clients, so the
		// KoboldCPP fingerprint alone would claim it.
		{"tabbyapi", "/.well-known/serviceinfo", func(body []byte) (string, bool) {
			return "", isTabbyAPIServiceInfo(body)
		}},
		// KoboldCPP: /api/extra/version reports the product name outright.
		{"koboldcpp", "/api/extra/version", func(body []byte) (string, bool) {
			var v KoboldCPPVersionResponse
			if json.Unmarshal(body, &v) == nil && isKoboldCPPVersion(v) {
				return v.Version, true
			}
			return "", false
		}},
		// LM Studio: the native REST listing, which nothing else serves.
		{"lmstudio", "/api/v0/models", func(body []byte) (string, bool) {
			return "", isLMStudioModelListing(body)
		}},
		// LocalAI: its capabilities listing, which nothing else serves. Asked
		// before Ollama's: LocalAI also answers /api/tags in Ollama's shape,
		// so the Ollama fingerprint alone would claim it.
		{"localai", "/v1/models/capabilities", func(body []byte) (string, bool) {
			return "", isLocalAICapabilitiesListing(body)
		}},
		// SGLang: its model info, which nothing else serves on that route.
		{"sglang", "/get_model_info", func(body []byte) (string, bool) {
			return "", isSGLangModelInfo(body)
		}},
		// Ollama: the native tag listing.
		{"ollama", "/api/tags", func(body []byte) (string, bool) {
			return "", isOllamaTagListing(body)
		}},
	}
}

// probeLocal performs one fingerprint GET. It reports the body, the status
// (only a 200 is worth inspecting), and an error only when the server could
// not be reached at all (so a 404 still counts as "the host is alive").
//
// The key is sent for the same reason discovery sends it: a self-hosted server
// can sit behind a password or an authenticating proxy, and an unauthenticated
// probe would see a 401 and conclude the server is not what it says it is.
func (d *DiscoveryService) probeLocal(ctx context.Context, endpoint, apiKey string) ([]byte, int, error) {
	return d.getOnce(ctx, endpoint, apiKey, localProbeTimeout)
}

// getOnce is one GET with no retry: the body, the status, and an error for
// a request that got no response (status 0) or whose body did not arrive
// whole or within the size cap (status set). A fingerprint probe bounds it
// with localProbeTimeout; a discovery read that must not retry (TabbyAPI's
// card routes, whose 503 is an answer) passes 0 and keeps the client's own
// deadline.
func (d *DiscoveryService) getOnce(ctx context.Context, endpoint, apiKey string, timeout time.Duration) ([]byte, int, error) {
	reqCtx, cancel := ctx, func() {}
	if timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		// A parse error quotes the raw endpoint, userinfo included.
		return nil, 0, &maskedError{text: maskRawURLText(rawURLSecrets(endpoint), err.Error()), cause: err}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		// Host only, as the rest of discovery logs: the endpoint keeps any
		// user:password the operator put in the base URL. The error is masked
		// off the request for an upstream or proxy that quotes the key back.
		err = maskedRequestError(req, err)
		debuglog.Debug("provider: local server probe failed", "host", req.URL.Host, "error", err.Error())
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Fingerprints and model cards are small; a body over the cap is not
	// one of ours, and is reported rather than cut short and misread.
	body, err := io.ReadAll(io.LimitReader(resp.Body, localProbeBodyCap+1))
	if err != nil {
		return nil, resp.StatusCode, maskedRequestError(req, err)
	}
	if len(body) > localProbeBodyCap {
		return nil, resp.StatusCode, fmt.Errorf("body over %d bytes", localProbeBodyCap)
	}
	return body, resp.StatusCode, nil
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
