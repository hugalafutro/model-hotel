package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/auth"
)

type CreateProxyKeyRequest struct {
	Name string `json:"name"`
}

type ProxyKeyResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Key     string `json:"key,omitempty"`
	Created string `json:"created_at"`
}

func (h *Handler) RegisterProxyKeys(r chi.Router) {
	r.Route("/api/keys", func(r chi.Router) {
		r.Use(h.AuthMiddleware)
		r.Post("/", h.CreateProxyKey)
		r.Get("/", h.ListProxyKeys)
		r.Delete("/{id}", h.RevokeProxyKey)
	})
}

func (h *Handler) CreateProxyKey(w http.ResponseWriter, r *http.Request) {
	var req CreateProxyKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	key, err := auth.GenerateProxyKey()
	if err != nil {
		http.Error(w, "failed to generate proxy key", http.StatusInternalServerError)
		return
	}

	keyHash := auth.HashProxyKey(key)

	id := uuid.New().String()
	created := time.Now().Format(time.RFC3339)

	query := `INSERT INTO proxy_keys (id, key_hash, name) VALUES ($1, $2, $3)`
	if _, err := h.dbPool.Pool().Exec(r.Context(), query, id, keyHash, req.Name); err != nil {
		http.Error(w, "failed to create proxy key", http.StatusInternalServerError)
		return
	}

	response := ProxyKeyResponse{
		ID:      id,
		Name:    req.Name,
		Key:     key,
		Created: created,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

func (h *Handler) ListProxyKeys(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT id, name, created_at
		FROM proxy_keys
		ORDER BY created_at DESC
	`

	rows, err := h.dbPool.Pool().Query(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var keys []ProxyKeyResponse
	for rows.Next() {
		var id, name string
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &createdAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		keys = append(keys, ProxyKeyResponse{
			ID:      id,
			Name:    name,
			Created: createdAt.Format(time.RFC3339),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func (h *Handler) RevokeProxyKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	query := `DELETE FROM proxy_keys WHERE id = $1`
	result, err := h.dbPool.Pool().Exec(r.Context(), query, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected() == 0 {
		http.Error(w, "proxy key not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ValidateProxyKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}

		token := ""
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			token = authHeader[7:]
		} else {
			http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
			return
		}

		if len(token) < 5 || token[:5] != "llmp_" {
			http.Error(w, "Invalid proxy key format", http.StatusUnauthorized)
			return
		}

		keyHash := auth.HashProxyKey(token)

		query := `SELECT id FROM proxy_keys WHERE key_hash = $1`
		var keyID string
		err := h.dbPool.Pool().QueryRow(r.Context(), query, keyHash).Scan(&keyID)
		if err != nil {
			http.Error(w, "Invalid proxy key", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
