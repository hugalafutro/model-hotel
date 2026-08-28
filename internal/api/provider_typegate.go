package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

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

	identity, err := h.discoveryService().IdentifyLocalServer(r.Context(), baseURL, apiKey)
	if err != nil {
		if !errors.Is(err, provider.ErrLocalServerUnreachable) {
			debuglog.Warn("provider: local server probe failed", "type", providerType, "error", err)
		}
		writeProviderTypeGateError(w, providerTypeGateResponse{
			Code:     codeProviderUnreachable,
			Error:    "could not reach a server at this address",
			Expected: providerType,
		})
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
