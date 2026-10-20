package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Error codes the dashboard branches on to phrase the failure in the
// operator's language. The English text sent alongside them is the fallback
// for API clients.
const (
	codeProviderTypeMismatch    = "provider_type_mismatch"
	codeProviderTypeUnconfirmed = "provider_type_unconfirmed"
	codeProviderUnreachable     = "provider_unreachable"
	// codeProviderURLRejected carries the reason ValidateProviderURL refused an
	// address, so the dashboard can explain it instead of saying "invalid".
	codeProviderURLRejected = "provider_url_rejected"
	// codeProviderDuplicateAddress refuses a second provider for a self-hosted
	// server that is already configured.
	codeProviderDuplicateAddress = "provider_duplicate_address"
	// codeProviderDuplicateKey refuses a second provider that would carry the
	// same key to the same address as one already configured.
	codeProviderDuplicateKey = "provider_duplicate_key"
)

// providerTypeGateResponse is the body behind the three codes above. The extra
// fields let the dashboard name what actually answered instead of repeating
// the request back at the operator.
type providerTypeGateResponse struct {
	Code            string `json:"code"`
	Error           string `json:"error"`
	Expected        string `json:"expected"`
	Detected        string `json:"detected,omitempty"`
	DetectedVersion string `json:"detected_version,omitempty"`
}

// rejectDuplicateLocalServer blocks a second provider for a self-hosted server
// that is already configured. Two providers on one address are legitimate for a
// hosted API, where each row carries its own key and therefore its own quota,
// but a self-hosted server has no such split: the duplicate only discovers the
// same models a second time and quietly forms an auto failover group with its
// twin. It writes the error and reports false when the address is taken.
func (h *Handler) rejectDuplicateLocalServer(w http.ResponseWriter, r *http.Request, providerType, baseURL string, excludeID uuid.UUID) bool {
	if !provider.IsLocalServerType(providerType) {
		return true
	}
	existing, err := h.providerRepo.List(r.Context())
	if err != nil {
		// A listing failure must not block a legitimate add; the duplicate is
		// a usability guard, not a correctness one.
		debuglog.Warn("provider: duplicate-address check failed", "error", err)
		return true
	}
	for _, p := range existing {
		if p.ID == excludeID || !provider.IsLocalServerType(provider.TypeOf(p)) {
			continue
		}
		if provider.SameLocalAddress(p.BaseURL, baseURL) {
			writeJSONStatus(w, http.StatusConflict, map[string]string{
				"code":     codeProviderDuplicateAddress,
				"error":    "a provider for this address already exists: " + p.Name,
				"existing": p.Name,
			})
			return false
		}
	}
	return true
}

// rejectDuplicateKey blocks a provider that would send the same key to the same
// address as one already configured. Two providers on one hosted address are
// legitimate when each carries its own key (two accounts, two quotas); with one
// key they are one account twice: the same models discovered again, an auto
// failover group of twins, and both rows draining one upstream quota. Keyless
// providers are left alone: with no key there is nothing to be the same. The
// existing rows are compared by their decrypted key, so the check is exact,
// read through the key cache so a busy provider costs no Argon2id round. Two
// creates racing each other can both pass: this is a usability guard like the
// address one, not a uniqueness constraint. It writes the error and reports
// false when the pair is taken.
func (h *Handler) rejectDuplicateKey(w http.ResponseWriter, r *http.Request, baseURL, apiKey string, excludeID uuid.UUID) bool {
	if apiKey == "" {
		return true
	}
	existing, err := h.providerRepo.List(r.Context())
	if err != nil {
		// As for the address guard: a usability check must not block an add
		// over an unrelated failure.
		debuglog.Warn("provider: duplicate-key check failed", "error", err)
		return true
	}
	for _, p := range existing {
		if p.ID == excludeID || len(p.EncryptedKey) == 0 || !sameHostedAddress(p.BaseURL, baseURL) {
			continue
		}
		stored, decErr := auth.DecryptCached(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.cfg.MasterKey)
		if decErr != nil {
			// A key this master key cannot read is not one the new provider can
			// duplicate in any way that matters here.
			debuglog.Warn("provider: duplicate-key check could not read a stored key", "provider_id", p.ID, "error", decErr)
			continue
		}
		if subtle.ConstantTimeCompare([]byte(stored), []byte(apiKey)) == 1 {
			writeJSONStatus(w, http.StatusConflict, map[string]string{
				"code":     codeProviderDuplicateKey,
				"error":    "a provider with this key for this address already exists: " + p.Name,
				"existing": p.Name,
			})
			return false
		}
	}
	return true
}

// sameHostedAddress reports whether two base URLs name the same endpoint:
// scheme and host compared without regard to case, the path with its case and
// without a trailing slash. The path is kept: on OpenCode the path decides
// which balance a key is billed to, so one key on two paths is two providers
// on purpose. An address that does not parse is compared as written.
func sameHostedAddress(a, b string) bool {
	return hostedAddressKey(a) == hostedAddressKey(b)
}

func hostedAddressKey(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.TrimRight(strings.TrimSpace(raw), "/")
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	// An explicit default port names the same endpoint as none.
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		host += ":" + port
	}
	// The escaped path, so an encoded slash stays distinct from a real one.
	key := scheme + "://" + host + strings.TrimRight(u.EscapedPath(), "/")
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key
}

func writeProviderTypeGateError(w http.ResponseWriter, body providerTypeGateResponse) {
	writeJSONStatus(w, http.StatusBadRequest, body)
}

// confirmLocalServerType blocks a create or a URL change whose address does not
// answer as the type the operator picked. It runs only for self-hosted server
// families: those are the ones the operator points at an arbitrary address, so
// they are the ones where a typo silently produces a provider that discovers
// nothing. Every other type is identified by its vendor hostname and is not
// probed.
//
// Confirmation is required rather than advisory: a mismatch means the stored
// type would drive the wrong native endpoints, and an unreachable server means
// nothing can be confirmed either way. Both leave the provider unsaved, so the
// server has to be running when it is added or its address changed.
//
// The probe goes through the same guarded discovery client as model discovery,
// so it inherits the SSRF protections (scheme, host allowlist, redirect and
// dial guards) rather than opening a second, unguarded egress path. It carries
// the provider's key for the same reason discovery does: a server behind a
// password would otherwise answer 401 and look like the wrong kind of server.
func (h *Handler) confirmLocalServerType(w http.ResponseWriter, r *http.Request, providerType, baseURL, apiKey string) bool {
	if !provider.IsLocalServerType(providerType) {
		return true
	}

	identity, err := h.discoveryService().IdentifyLocalServer(r.Context(), baseURL, apiKey, providerType)
	if err != nil {
		if !errors.Is(err, provider.ErrLocalServerUnreachable) && r.Context().Err() == nil {
			debuglog.Warn("provider: local server probe failed", "type", providerType, "error", err)
		}
		// Any other error is a check that could not be completed on a server
		// that did answer (an emulator probe failing after the expected
		// family matched): nothing is saved, and a retry clears it.
		resp := providerTypeGateResponse{
			Code:     codeProviderUnreachable,
			Error:    "could not reach a server at this address",
			Expected: providerType,
		}
		if !errors.Is(err, provider.ErrLocalServerUnreachable) {
			resp.Code = codeProviderTypeUnconfirmed
			resp.Error = "the server at this address could not be confirmed as " + providerType + "; try again"
		}
		writeProviderTypeGateError(w, resp)
		return false
	}

	switch identity.Type {
	case providerType:
		return true
	case "":
		writeProviderTypeGateError(w, providerTypeGateResponse{
			Code:     codeProviderTypeUnconfirmed,
			Error:    "the server at this address did not answer as " + providerType,
			Expected: providerType,
		})
	default:
		writeProviderTypeGateError(w, providerTypeGateResponse{
			Code:            codeProviderTypeMismatch,
			Error:           "the server at this address reports " + identity.Type + ", not " + providerType,
			Expected:        providerType,
			Detected:        identity.Type,
			DetectedVersion: identity.Version,
		})
	}
	return false
}
