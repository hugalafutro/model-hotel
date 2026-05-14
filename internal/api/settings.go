package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// RegisterSettings mounts settings API routes.
func (h *Handler) RegisterSettings(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Get("/", h.GetSettings)
		r.Put("/", h.UpdateSettings)
	})
}

// GetSettings returns all settings as a key-value map.
func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := h.settingsRepo.GetAll(r.Context())
	if err != nil {
		respondError(w, "failed to load settings", err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// allowedSettings defines which keys can be set and their validation rules.
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
	"dashboard_refresh":            {typeName: "string"}, // predefined option
	"quota_refresh":                {typeName: "string"}, // predefined option
	"history_limit":                {typeName: "string"}, // predefined option
	"log_retention":                {typeName: "string"}, // predefined option
	"stale_request_timeout":        {typeName: "string"}, // predefined option
	"toast_duration":               {typeName: "int", min: 1000, max: 15000},
	"theme":                        {typeName: "string"}, // light/dark
	"ui_style":                     {typeName: "string"}, // e.g. "default"
	"accent_color":                 {typeName: "string"}, // hex color
	"key_cache_ttl":                {typeName: "string"}, // duration (e.g. "10m0s")
}

const maxSettingValueLen = 500

// UpdateSettings updates user settings in the database.
func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req) == 0 {
		http.Error(w, "no settings provided", http.StatusBadRequest)
		return
	}

	if len(req) > 50 {
		http.Error(w, "too many settings in one request", http.StatusBadRequest)
		return
	}

	// Validate all keys and values before writing
	for key, value := range req {
		rule, ok := allowedSettings[key]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown setting: %s", key), http.StatusBadRequest)
			return
		}

		if len(value) > maxSettingValueLen {
			http.Error(w, fmt.Sprintf("value for %s too long (max %d characters)", key, maxSettingValueLen), http.StatusBadRequest)
			return
		}

		switch rule.typeName {
		case "int":
			v, err := strconv.Atoi(value)
			if err != nil {
				http.Error(w, fmt.Sprintf("%s must be a number", key), http.StatusBadRequest)
				return
			}
			if float64(v) < rule.min || float64(v) > rule.max {
				http.Error(w, fmt.Sprintf("%s must be between %d and %d", key, int(rule.min), int(rule.max)), http.StatusBadRequest)
				return
			}
		case "float":
			v, err := strconv.ParseFloat(value, 64)
			if err != nil {
				http.Error(w, fmt.Sprintf("%s must be a number", key), http.StatusBadRequest)
				return
			}
			if v < rule.min || v > rule.max {
				http.Error(w, fmt.Sprintf("%s must be between %g and %g", key, rule.min, rule.max), http.StatusBadRequest)
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}
