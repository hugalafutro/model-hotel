package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// CreateProvider creates a new provider.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req provider.CreateProviderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	trimmed, err := validateNameString("name", req.Name, 1, 100)
	if err != nil {
		respondBadRequest(w, "invalid name", err)
		return
	}
	req.Name = trimmed

	if req.BaseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}

	// The dashboard sends the type the operator picked. A client that omits it
	// gets the vendor-hostname derivation, which covers scripted adds of cloud
	// providers; self-hosted servers must name their type to be added.
	derivedType := req.ProviderType == ""
	if derivedType {
		req.ProviderType = provider.TypeFromHostname(req.BaseURL)
	}
	if !provider.IsKnownType(req.ProviderType) {
		http.Error(w, "unknown provider_type", http.StatusBadRequest)
		return
	}
	// Self-hosted servers serve their OpenAI-compatible API under /v1 and their
	// native endpoints at the root, so the /v1 half is a convenience the
	// operator should not have to get right.
	req.BaseURL = provider.NormalizeLocalBaseURL(req.ProviderType, req.BaseURL)

	// Measured after normalization, so the stored value is the one that has to
	// fit.
	if len(req.BaseURL) > 500 {
		http.Error(w, "base_url must be at most 500 characters", http.StatusBadRequest)
		return
	}

	// An address matching no vendor host and given no type is a generic OpenAI
	// endpoint. That is right for a gateway and wrong for a self-hosted server the
	// caller forgot to name, and the difference is invisible afterwards, so it is
	// logged once.
	if derivedType && req.ProviderType == "openai" {
		debuglog.Info("provider: no provider_type given, treating as a generic OpenAI-compatible endpoint",
			"name", req.Name, "hint", "self-hosted servers (ollama, lmstudio, koboldcpp) must name their type to get native discovery")
	}

	// Some providers (e.g. OpenCode Zen) support keyless access for free models, so
	// an empty API key is allowed only for those types.
	if req.APIKey == "" && !providerTypeAllowsEmptyKey(req.ProviderType) {
		http.Error(w, "api_key is required for this provider type", http.StatusBadRequest)
		return
	}

	if len(req.APIKey) > 500 {
		http.Error(w, "api_key must be less than 500 characters", http.StatusBadRequest)
		return
	}

	if !h.acceptProviderURLShape(w, req.BaseURL) {
		return
	}

	// Application-level duplicate name check.
	existing, err := h.providerRepo.GetByName(r.Context(), req.Name)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// A DB error here looks like "no duplicate" and bypasses the app-level
		// guard, leaving the DB unique constraint as the backstop. Surfaced so a
		// flaky DB does not quietly admit duplicates.
		debuglog.Warn("provider create: duplicate-name check failed, relying on DB constraint", "name", req.Name, "error", err)
	}
	if existing != nil {
		http.Error(w, "a provider with this name already exists", http.StatusConflict)
		return
	}

	if !h.rejectDuplicateLocalServer(w, r, req.ProviderType, req.BaseURL, uuid.Nil) {
		return
	}

	// Last, because it is the only check that waits on the network: a bad name
	// or URL should fail immediately rather than after a probe timeout.
	if !h.confirmLocalServerType(w, r, req.ProviderType, req.BaseURL, req.APIKey) {
		return
	}

	var encryptedKey *auth.KeyPair
	if req.APIKey != "" {
		var encErr error
		encryptedKey, encErr = auth.Encrypt(req.APIKey, h.cfg.MasterKey)
		if encErr != nil {
			respondError(w, fmt.Sprintf("failed to encrypt API key for provider %q", req.Name), encErr, http.StatusInternalServerError)
			return
		}
	}

	var encCiphertext, encNonce, encSalt []byte
	if encryptedKey != nil {
		encCiphertext = encryptedKey.Ciphertext
		encNonce = encryptedKey.Nonce
		encSalt = encryptedKey.Salt
	}

	p, err := h.providerRepo.Create(r.Context(), req, encCiphertext, encNonce, encSalt)
	if err != nil {
		if db.IsUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		respondError(w, fmt.Sprintf("failed to create provider %q", req.Name), err, http.StatusInternalServerError)
		return
	}

	// Skip key cache warming for keyless providers (nil encrypted key bytes).
	if len(p.EncryptedKey) > 0 {
		go auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.cfg.MasterKey)
	}
	// The plaintext is in hand: hold it for the credential mask now rather than
	// after the warm lands, so a relay quoting this key is masked from the first
	// request.
	util.HoldSecret(req.APIKey)

	response := provider.ToResponse(p)
	writeJSONCreated(w, response)
}

// ListProviders returns all configured providers.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", err, http.StatusInternalServerError)
		return
	}

	rows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, COUNT(*) FROM models GROUP BY provider_id")
	if err != nil {
		respondError(w, "failed to query model counts", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	modelCounts := make(map[string]int)
	for rows.Next() {
		var providerID string
		var count int
		if err := rows.Scan(&providerID, &count); err != nil {
			respondError(w, "failed to scan model count row", err, http.StatusInternalServerError)
			return
		}
		modelCounts[providerID] = count
	}

	// Non-admins only see their own traffic in these totals: the same owner
	// predicate the logs and stats surfaces apply, so a usage-granted user cannot
	// read other tenants' aggregate volume off the provider list.
	ownerFrag, ownerArgs := ownerFilterFragment(ownerScopeFromIdentity(r), 1)
	tokenRows, err := h.dbPool.Pool().Query(r.Context(), "SELECT rl.provider_id, SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)), MIN(rl.created_at) FROM request_logs rl WHERE rl.provider_id IS NOT NULL"+ownerFrag+" GROUP BY rl.provider_id", ownerArgs...)
	if err != nil {
		respondError(w, "failed to query token counts", err, http.StatusInternalServerError)
		return
	}
	defer tokenRows.Close()

	tokenCounts := make(map[string]int)
	tokensSince := make(map[string]time.Time)
	for tokenRows.Next() {
		var providerID string
		var total int
		var since *time.Time
		if err := tokenRows.Scan(&providerID, &total, &since); err != nil {
			respondError(w, "failed to scan token count row", err, http.StatusInternalServerError)
			return
		}
		tokenCounts[providerID] = total
		if since != nil {
			tokensSince[providerID] = *since
		}
	}
	if err := tokenRows.Err(); err != nil {
		respondError(w, "failed to read token count rows", err, http.StatusInternalServerError)
		return
	}

	responses := make([]provider.ProviderResponse, len(providers))
	for i, p := range providers {
		responses[i] = provider.ToResponse(p)
		responses[i].ModelCount = modelCounts[p.ID.String()]
		responses[i].TotalTokens = tokenCounts[p.ID.String()]
		if since, ok := tokensSince[p.ID.String()]; ok {
			responses[i].TokensSince = &since
		}
		responses[i].LastCap = h.lastCapFor(p.ID)
	}

	writeJSON(w, responses)
}

// GetProvider returns a single provider by ID.
func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	p, err := h.providerRepo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		respondError(w, fmt.Sprintf("failed to get provider %s", id), err, http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)
	response.LastCap = h.lastCapFor(p.ID)

	var modelCount int
	if err := h.dbPool.Pool().QueryRow(r.Context(), "SELECT COUNT(*) FROM models WHERE provider_id = $1", p.ID).Scan(&modelCount); err == nil {
		response.ModelCount = modelCount
	}

	writeJSON(w, response)
}

// acceptProviderIdentity validates the two fields that decide where a provider
// points and how it is driven: its base URL and its type. Either change has to
// be confirmed against the server that answers, so they are handled together.
// It writes the error response and reports false when the change must not be
// saved.
func (h *Handler) acceptProviderIdentity(w http.ResponseWriter, r *http.Request, id uuid.UUID, req *provider.UpdateProviderRequest) bool {
	if req.BaseURL == nil && req.ProviderType == nil {
		return true
	}
	if req.ProviderType != nil && !provider.IsKnownType(*req.ProviderType) {
		http.Error(w, "unknown provider_type", http.StatusBadRequest)
		return false
	}
	// Checked before the provider is loaded, so a refused address fails fast and
	// for the right reason.
	if req.BaseURL != nil && !h.acceptProviderURLShape(w, *req.BaseURL) {
		return false
	}

	current, err := h.providerRepo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return false
		}
		respondError(w, fmt.Sprintf("failed to load provider %s", id), err, http.StatusInternalServerError)
		return false
	}

	// The type the provider has once this update lands drives everything below: a
	// new address must answer as it, and a corrected type must match the address
	// already stored.
	effectiveType := provider.TypeOf(current)
	typeChanged := false
	if req.ProviderType != nil && *req.ProviderType != effectiveType {
		effectiveType = *req.ProviderType
		typeChanged = true
	}

	address := current.BaseURL
	if req.BaseURL != nil {
		address = *req.BaseURL
	}
	normalized := provider.NormalizeLocalBaseURL(effectiveType, address)
	if req.BaseURL != nil {
		req.BaseURL = &normalized
	}

	// A type-only change probes the address already stored. It is re-checked
	// because ALLOWED_PROVIDER_HOSTS may have narrowed since, and every address
	// this handler probes must be one the SSRF rules accept now.
	if req.BaseURL == nil && !h.acceptProviderURLShape(w, normalized) {
		return false
	}

	// Nothing that decides where requests land is changing: an update that only
	// renames the provider must not fail because the server is down. Both sides
	// are normalized first, so a client echoing back a stored URL in a
	// non-canonical form does not trip it.
	if !typeChanged && normalized == provider.NormalizeLocalBaseURL(effectiveType, current.BaseURL) {
		return true
	}

	if !h.rejectDuplicateLocalServer(w, r, effectiveType, normalized, id) {
		return false
	}

	// The probe needs a key for a password-protected server: the update's own
	// key when it carries one, otherwise the stored key.
	apiKey := ""
	if req.APIKey != nil {
		apiKey = *req.APIKey
	} else if len(current.EncryptedKey) > 0 {
		plain, decErr := auth.Decrypt(current.EncryptedKey, current.KeyNonce, current.KeySalt, h.cfg.MasterKey)
		if decErr != nil {
			// Not fatal: the probe goes out unauthenticated, and an unreadable key
			// is the update's problem to report, not this check's.
			debuglog.Warn("provider: could not decrypt key for the type probe", "provider_id", id, "error", decErr)
		} else {
			apiKey = plain
		}
	}
	return h.confirmLocalServerType(w, r, effectiveType, normalized, apiKey)
}

// acceptProviderURLShape applies the scheme and SSRF rules a base URL must
// satisfy before anything is done with it.
func (h *Handler) acceptProviderURLShape(w http.ResponseWriter, baseURL string) bool {
	if !h.cfg.AllowHTTPProviders {
		parsed, err := url.Parse(strings.TrimSpace(baseURL))
		if err != nil || parsed.Scheme != "https" {
			http.Error(w, "base_url must use HTTPS (set ALLOW_HTTP_PROVIDERS=true for HTTP)", http.StatusBadRequest)
			return false
		}
	}
	if err := h.cfg.ValidateProviderURL(baseURL); err != nil {
		// The reason matters to the operator: "not in ALLOWED_PROVIDER_HOSTS" and
		// "resolves to a private address" call for different fixes, and a
		// containerised Model Hotel cannot reach the operator's localhost at all.
		// This endpoint is admin-only, so echoing the reason leaks nothing.
		debuglog.Info("provider: base URL rejected", "error", err)
		writeCodedError(w, http.StatusBadRequest, codeProviderURLRejected, err.Error())
		return false
	}
	return true
}

// UpdateProvider updates an existing provider by ID.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	var req provider.UpdateProviderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Validate field lengths.
	if req.Name != nil {
		trimmed, err := validateNamePtr("name", req.Name, 1, 100)
		if err != nil {
			respondBadRequest(w, "invalid name", err)
			return
		}
		req.Name = trimmed
	}

	if req.BaseURL != nil {
		trimmed := trimString(*req.BaseURL)
		req.BaseURL = &trimmed
		if err := validateStringPtrLength("base_url", req.BaseURL, 1, 500); err != nil {
			respondBadRequest(w, "invalid base URL", err)
			return
		}
	}

	if req.APIKey != nil {
		if len(*req.APIKey) > 500 {
			http.Error(w, "api_key must be at most 500 characters", http.StatusBadRequest)
			return
		}
	}

	if req.ScheduledDisableOn.Set && req.ScheduledDisableOn.Value != nil {
		v := *req.ScheduledDisableOn.Value
		if _, err := time.Parse("2006-01-02", v); err != nil {
			respondBadRequest(w, "invalid scheduled_disable_on", err)
			return
		}
		// ISO dates compare correctly as strings. The client's calendar floors at
		// browser-tomorrow, but the server accepts its own today, so a browser
		// lagging the server clock does not get a 400 on its earliest selectable
		// day. A server-today schedule is due immediately and the sweep fires it on
		// its next tick.
		if v < time.Now().Format("2006-01-02") {
			http.Error(w, "scheduled_disable_on must not be in the past", http.StatusBadRequest)
			return
		}
	}

	// The rule the config import and the column's CHECK constraint share
	// (provider.ValidateMaxInFlight): a ceiling of zero does not admit nothing, it
	// reads as no ceiling at all.
	if req.MaxInFlight.Set {
		if err := provider.ValidateMaxInFlight(req.MaxInFlight.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	// Application-level duplicate name check when renaming.
	if req.Name != nil {
		existing, _ := h.providerRepo.GetByName(r.Context(), *req.Name)
		if existing != nil && existing.ID != id {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
	}

	// Validate the address and the type together: either one changing has to
	// be confirmed against the server that answers.
	if !h.acceptProviderIdentity(w, r, id, &req) {
		return
	}

	var encryptedKey []byte
	var keyNonce []byte
	var keySalt []byte

	if req.APIKey != nil {
		enc, encErr := auth.Encrypt(*req.APIKey, h.cfg.MasterKey)
		if encErr != nil {
			respondError(w, "failed to encrypt API key", encErr, http.StatusInternalServerError)
			return
		}
		encryptedKey = enc.Ciphertext
		keyNonce = enc.Nonce
		keySalt = enc.Salt
		// Held for the credential mask as on create; the old key stays held.
		util.HoldSecret(*req.APIKey)
	}

	prior := h.priorProvider(r.Context(), id, req)

	p, err := h.providerRepo.Update(r.Context(), id, req, encryptedKey, keyNonce, keySalt)
	if err != nil {
		if db.IsUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		respondError(w, fmt.Sprintf("failed to update provider %s", id), err, http.StatusInternalServerError)
		return
	}

	h.settleProviderUpdate(context.WithoutCancel(r.Context()), prior, p, req)

	response := provider.ToResponse(p)
	writeJSON(w, response)
}

// priorProvider reads the row a save is about to replace, but only when the
// save touches something whose before-and-after matters: the enabled flag or
// the provider's identity. A rename never needs it.
func (h *Handler) priorProvider(ctx context.Context, id uuid.UUID, req provider.UpdateProviderRequest) *provider.Provider {
	if req.Enabled == nil && req.AutodiscoveryEnabled == nil && req.BaseURL == nil && req.ProviderType == nil && req.APIKey == nil {
		return nil
	}
	prior, err := h.providerRepo.Get(ctx, id)
	if err != nil {
		return nil
	}
	return prior
}

// settleProviderUpdate runs what a committed save owes the rest of the system:
// the enabled-flag settlement, then a background rediscovery when the save
// changed something that can change the catalogue.
func (h *Handler) settleProviderUpdate(ctx context.Context, prior, p *provider.Provider, req provider.UpdateProviderRequest) {
	disabling := req.Enabled != nil && !*req.Enabled
	enabling := prior != nil && !prior.Enabled && p.Enabled
	h.settleProviderToggle(ctx, p, disabling, enabling)
	if shouldRediscover(prior, p, req.APIKey != nil) {
		h.rediscoverInBackground(ctx, p)
	}
}

// shouldRediscover reports whether a save changed something that can change
// the provider's catalogue: it or its autodiscovery was switched on, or its
// address, type or key moved. A config-sync import rediscovers on every fleet
// member after such a change, so the node the save landed on scans too rather
// than keeping the catalogue from before the edit until the next sweep.
func shouldRediscover(prior, updated *provider.Provider, keyChanged bool) bool {
	if prior == nil || !updated.Enabled || !updated.AutodiscoveryEnabled {
		return false
	}
	return !prior.Enabled || !prior.AutodiscoveryEnabled || keyChanged ||
		prior.BaseURL != updated.BaseURL ||
		provider.TypeOf(prior) != provider.TypeOf(updated)
}

// rediscoverInBackground scans one provider without holding the response:
// discovery can take minutes on a slow upstream. The dashboard follows the
// scan through the discovery events (lists re-read on completion, failures
// toasted) rather than the diff modal a manual Discover click shows; the scan
// itself bounds its own upstream time.
func (h *Handler) rediscoverInBackground(ctx context.Context, prov *provider.Provider) {
	if h.dbPool == nil {
		return
	}
	go h.discoverOne(ctx, h.discoveryService(), newModelRepo(h.dbPool.Pool()), newFailoverRepo(h.dbPool.Pool()), prov, false)
}

// settleProviderToggle runs what an enabled-flag change owes the rest of the
// system. A disable syncs failover groups to remove stale entries from
// auto-created groups, and an enable syncs them to put its models back; routing
// already follows the flag, but the UI and group membership must reflect the
// new state. An enable also refreshes the quota snapshot inline (bounded by the
// poll timeout): the background poller skips disabled providers, so the stored
// snapshot is as old as the disable, and the badge the dashboard reads straight
// after this response must show the current balance rather than that one.
func (h *Handler) settleProviderToggle(ctx context.Context, p *provider.Provider, disabling, enabling bool) {
	// A handler without a pool has none of the repositories this needs.
	if h.dbPool == nil {
		return
	}
	if disabling || enabling {
		failoverRepo := failover.NewRepository(h.dbPool.Pool())
		if _, err := failoverRepo.SyncAllModels(ctx); err != nil {
			debuglog.Info("admin: failed to sync failover groups after provider enable/disable", "error", err)
		}
	}
	if !enabling {
		return
	}
	if kind, ok := quotaKindFor(provider.TypeOf(p)); ok {
		// Tighter than the poll's own budget: this runs on an interactive save,
		// and the background poller is the backstop for a slow upstream.
		pollCtx, cancel := context.WithTimeout(ctx, enableQuotaRefreshTimeout)
		defer cancel()
		h.pollQuotaForProvider(pollCtx, h.discoveryService(), p, kind)
		h.RefreshQuotaAdvice(ctx)
	}
}

// enableQuotaRefreshTimeout bounds the upstream quota call an enable makes
// before its response is written.
const enableQuotaRefreshTimeout = 10 * time.Second

// DeleteProvider removes a provider by ID and cleans up associated data.
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	if err := h.providerRepo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		respondError(w, fmt.Sprintf("failed to delete provider %s", id), err, http.StatusInternalServerError)
		return
	}

	// The quota drift watch keeps a per-provider schema baseline in the settings
	// K/V and nothing else removes it, so it would outlive the provider. Detached
	// from the request context like the sync below: the row is already deleted, and
	// a client that hangs up must not leave the orphan behind.
	h.forgetQuotaSchema(context.WithoutCancel(r.Context()), id)

	// Sync failover groups, since the cascade-deleted models may leave groups with
	// stale entries or zero candidates. Guarded because dbPool can be nil.
	if h.dbPool != nil {
		failoverRepo := failover.NewRepository(h.dbPool.Pool())
		if _, err := failoverRepo.SyncAllModels(context.WithoutCancel(r.Context())); err != nil {
			// Logged but not fatal: the provider is already gone.
			debuglog.Info("admin: failed to sync failover groups after provider delete", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// providerTypeAllowsEmptyKey returns true for provider types that support keyless
// access (e.g. OpenCode Zen, and self-hosted servers, which serve their models
// without an API key).
func providerTypeAllowsEmptyKey(providerType string) bool {
	switch providerType {
	case "opencode-zen", "ollama", "koboldcpp", "lmstudio", "custom":
		return true
	default:
		return false
	}
}

// isForeignKeyViolation reports whether err is a PostgreSQL foreign key violation (error code 23503).
func isForeignKeyViolation(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23503"
	}
	return false
}

// lastCapFor is the provider's last exhausted 429 from the proxy's ledger, nil
// when there is none or no ledger is wired.
func (h *Handler) lastCapFor(id uuid.UUID) *provider.CapNote {
	n, ok := h.capLedger.Get(id)
	if !ok {
		return nil
	}
	return &n
}
