package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestRespondLookupError(t *testing.T) {
	t.Run("not-found sentinel returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondLookupError(w, pgx.ErrNoRows, pgx.ErrNoRows, "thing not found", "failed to load thing")
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("wrapped not-found sentinel returns 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		wrapped := fmt.Errorf("query failed: %w", pgx.ErrNoRows)
		respondLookupError(w, wrapped, pgx.ErrNoRows, "thing not found", "failed to load thing")
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for wrapped sentinel, got %d", w.Code)
		}
	})

	t.Run("any other error returns a logged 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondLookupError(w, errors.New("db connection lost"), pgx.ErrNoRows, "thing not found", "failed to load thing")
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", w.Code)
		}
	})
}
