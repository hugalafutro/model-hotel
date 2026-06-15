package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/otelexport"
)

// RegisterSettings mounts settings API routes.
func (h *Handler) RegisterSettings(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Get("/", h.GetSettings)
		r.Put("/", h.UpdateSettings)
		r.Delete("/", h.ResetSettings)
	})
}

// injectReadOnlyStatus adds server-derived, read-only fields to a settings map
// before it is returned to the client. These keys are deliberately excluded
// from allowedSettings so they cannot be written via PUT /api/settings:
//   - app_version: the running build (set via ldflags).
//   - log_export_json/metrics/otel: which log-export integrations are active,
//     derived from process environment (LOG_FORMAT, METRICS_TOKEN, OTLP endpoint),
//     for the Observability settings section to reflect.
//
// All three response handlers call this so a mutation response can't drop the
// status keys from the client's settings cache. Returns a non-nil map.
func (h *Handler) injectReadOnlyStatus(all map[string]string) map[string]string {
	if all == nil {
		all = make(map[string]string)
	}
	all["app_version"] = h.appVersion
	all["log_export_json"] = strconv.FormatBool(debuglog.JSONFormat())
	all["log_export_metrics"] = strconv.FormatBool(h.cfg != nil && h.cfg.MetricsToken != "")
	all["log_export_otel"] = strconv.FormatBool(otelexport.LogsEnabled())
	return all
}

// GetSettings returns all settings as a key-value map.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := h.settingsRepo.GetAll(r.Context())
	if err != nil {
		respondError(w, "failed to load settings", err, http.StatusInternalServerError)
		return
	}

	all = h.injectReadOnlyStatus(all)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// allowedSettings defines which keys can be set and their validation rules.
// The key set MUST be kept in sync with settings.AllowedSettings — add a
// key to both or neither. TestAllowedSettingsSync enforces this at CI time.
var allowedSettings = map[string]struct {
	typeName string // "string", "int", "float"
	min      float64
	max      float64
}{
	"rate_limit_enabled":           {typeName: "string"}, // bool as string
	"rate_limit_ip_enabled":        {typeName: "string"}, // bool as string
	"rate_limit_ip_rps":            {typeName: "float", min: 0, max: 10000},
	"rate_limit_ip_burst":          {typeName: "int", min: 1, max: 10000},
	"rate_limit_max_wait_ms":       {typeName: "int", min: 0, max: 10000},
	"rate_limit_rps":               {typeName: "float", min: 0, max: 10000},
	"rate_limit_burst":             {typeName: "int", min: 1, max: 10000},
	"request_timeout":              {typeName: "string"}, // duration (e.g. "1m0s")
	"failover_on_rate_limit":       {typeName: "string"}, // bool as string
	"circuit_breaker_enabled":      {typeName: "string"}, // bool as string
	"circuit_breaker_threshold":    {typeName: "int", min: 1, max: 100},
	"circuit_breaker_cooldown":     {typeName: "string"}, // duration (e.g. "1m0s")
	"discovery_interval":           {typeName: "string"}, // predefined option
	"discovery_on_startup":         {typeName: "string"}, // bool as string
	"discovery_on_provider_create": {typeName: "string"}, // bool as string
	"log_retention":                {typeName: "string"}, // predefined option
	"stale_request_timeout":        {typeName: "string"}, // predefined option
	"key_cache_ttl":                {typeName: "string"}, // duration (e.g. "10m0s")
	"ttft_timeout":                 {typeName: "string"}, // duration (e.g. "1m0s", "0s" = disabled)
	"stream_stall_timeout":         {typeName: "string"}, // duration (e.g. "30s", "0s" = disabled)
	"backup_enabled":               {typeName: "string"}, // bool as string
	"backup_interval":              {typeName: "string"}, // duration (e.g. "24h")
	"backup_son_retention":         {typeName: "int", min: 1, max: 365},
	"backup_father_retention":      {typeName: "int", min: 0, max: 52},
	"backup_grandfather_retention": {typeName: "int", min: 0, max: 120},
}

const maxSettingValueLen = 500

// UpdateSettings updates user settings in the database.
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	if len(req) == 0 {
		respondBadRequest(w, "no settings provided", nil)
		return
	}

	if len(req) > 50 {
		respondBadRequest(w, "too many settings in one request", nil)
		return
	}

	// Validate all keys and values before writing
	for key, value := range req {
		rule, ok := allowedSettings[key]
		if !ok {
			respondBadRequest(w, fmt.Sprintf("unknown setting: %s", key), nil)
			return
		}

		if len(value) > maxSettingValueLen {
			respondBadRequest(w, fmt.Sprintf("value for %s too long (max %d characters)", key, maxSettingValueLen), nil)
			return
		}

		switch rule.typeName {
		case "int":
			v, err := strconv.Atoi(value)
			if err != nil {
				respondBadRequest(w, fmt.Sprintf("%s must be a number", key), err)
				return
			}
			if float64(v) < rule.min || float64(v) > rule.max {
				respondBadRequest(w, fmt.Sprintf("%s must be between %d and %d", key, int(rule.min), int(rule.max)), nil)
				return
			}
		case "float":
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				respondBadRequest(w, fmt.Sprintf("%s must be a number", key), err)
				return
			}
			if v < rule.min || v > rule.max {
				respondBadRequest(w, fmt.Sprintf("%s must be between %g and %g", key, rule.min, rule.max), nil)
				return
			}
		}
	}

	tx, err := h.dbPool.Begin(r.Context())
	if err != nil {
		debuglog.Error("settings: failed to begin transaction", "error", err)
		respondError(w, "failed to begin transaction", err, http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	for key, value := range req {
		if err := h.settingsRepo.SetTx(r.Context(), tx, key, value); err != nil {
			debuglog.Error("settings: failed to save setting", "key", key, "error", err)
			respondError(w, fmt.Sprintf("failed to save setting %q", key), err, http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		debuglog.Error("settings: failed to commit transaction", "error", err)
		respondError(w, "failed to commit transaction", err, http.StatusInternalServerError)
		return
	}

	// Invalidate cache for updated keys after successful commit
	for key := range req {
		h.settingsRepo.InvalidateCache(key)
	}
	keys := make([]string, 0, len(req))
	for key := range req {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	debuglog.Info("settings: updated", "keys", keys)

	all, _ := h.settingsRepo.GetAll(r.Context())
	all = h.injectReadOnlyStatus(all)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// ResetSettings deletes specified settings keys from the database so they
// fall through to their Go-side defaults. An empty keys list resets all
// settings. Returns the full updated settings map.
func (h *Handler) ResetSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	// Empty keys list = reset all known settings.
	keys := req.Keys
	if len(keys) == 0 {
		for k := range allowedSettings {
			keys = append(keys, k)
		}
	} else if len(keys) > 50 {
		// Guard only user-supplied lists against unbounded input.
		// The internally-expanded "reset all" list is bounded by allowedSettings.
		respondBadRequest(w, "too many keys in one request", nil)
		return
	}

	// Validate all keys before deleting.
	for _, key := range keys {
		if _, ok := allowedSettings[key]; !ok {
			respondBadRequest(w, fmt.Sprintf("unknown setting: %s", key), nil)
			return
		}
	}

	tx, err := h.dbPool.Begin(r.Context())
	if err != nil {
		debuglog.Error("settings: failed to begin transaction", "error", err)
		respondError(w, "failed to begin transaction", err, http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	if err := h.settingsRepo.DeleteKeysTx(r.Context(), tx, keys); err != nil {
		debuglog.Error("settings: failed to reset", "error", err)
		respondError(w, "failed to reset settings", err, http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		debuglog.Error("settings: failed to commit reset", "error", err)
		respondError(w, "failed to commit reset", err, http.StatusInternalServerError)
		return
	}

	// Invalidate cache for deleted keys after successful commit.
	// Use NotifyDeleted (not InvalidateCache) to avoid a redundant DB
	// query — we already know the keys were deleted.
	for _, key := range keys {
		h.settingsRepo.NotifyDeleted(key)
	}

	sort.Strings(keys)
	debuglog.Info("settings: reset to defaults", "keys", keys)

	all, _ := h.settingsRepo.GetAll(r.Context())
	all = h.injectReadOnlyStatus(all)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}
