package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

func TestIsClientDisconnect(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"broken pipe", syscall.EPIPE, true},
		{"connection reset", syscall.ECONNRESET, true},
		{"closed conn", net.ErrClosed, true},
		{"context canceled is not a disconnect", context.Canceled, false},
		{"wrapped broken pipe", fmt.Errorf("write tcp: %w", syscall.EPIPE), true},
		{"unmarshalable value", errors.New("json: unsupported type: chan int"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClientDisconnect(tc.err); got != tc.want {
				t.Errorf("isClientDisconnect(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// levelCaptureHandler records the level of the last record it handled.
type levelCaptureHandler struct{ last slog.Level }

func (h *levelCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *levelCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.last = r.Level
	return nil
}
func (h *levelCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *levelCaptureHandler) WithGroup(string) slog.Handler      { return h }

func TestLogEncodeError_Level(t *testing.T) {
	// SetHandler swaps the process-wide slog default; restore it afterwards so
	// later tests in this package aren't silently swallowed by the capture handler.
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })

	capt := &levelCaptureHandler{}
	debuglog.SetHandler(capt)

	t.Run("client disconnect logs at debug", func(t *testing.T) {
		capt.last = slog.LevelError + 1 // sentinel
		logEncodeError(fmt.Errorf("write tcp 1.2.3.4:8080->5.6.7.8:9: write: %w", syscall.EPIPE))
		if capt.last != slog.LevelDebug {
			t.Errorf("expected debug level, got %v", capt.last)
		}
	})

	t.Run("genuine encode error logs at error", func(t *testing.T) {
		capt.last = slog.LevelDebug - 1 // sentinel
		logEncodeError(errors.New("json: unsupported type: chan int"))
		if capt.last != slog.LevelError {
			t.Errorf("expected error level, got %v", capt.last)
		}
	})
}
