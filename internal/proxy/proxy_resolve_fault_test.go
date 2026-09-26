package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// resolveRow is the closed request-log row a resolve failure leaves behind.
type resolveRow struct {
	status                  int
	errorKind, errorMessage string
}

// closedRowFor polls for the failed row whose model_id is exactly modelID. The
// pending INSERT and the terminal UPDATE are both asynchronous.
func closedRowFor(t *testing.T, h *Handler, modelID string) resolveRow {
	t.Helper()
	return closedRowWhere(t, h, "model_id", modelID)
}

// closedRowWhere is closedRowFor on any text column; column is a literal from
// the calling test, never input.
func closedRowWhere(t *testing.T, h *Handler, column, value string) resolveRow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var row resolveRow
		var state string
		err := h.dbPool.QueryRow(context.Background(),
			`SELECT COALESCE(status_code, 0), state, COALESCE(error_kind, ''), COALESCE(error_message, '')
			 FROM request_logs WHERE `+column+` = $1 ORDER BY created_at DESC LIMIT 1`, value,
		).Scan(&row.status, &state, &row.errorKind, &row.errorMessage)
		if err == nil && state == "failed" {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("no failed request_logs row with %s %q (last err: %v, last state: %q)", column, value, err, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// unreachablePool is a pool whose every query fails to connect, the database
// outage a resolve must not report as an unknown model.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://invalid:invalid@localhost:59999/testdb?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// chatRequestFor builds an authenticated chat request naming reqModel, on ctx.
func chatRequestFor(ctx context.Context, keyHash, reqModel string) *http.Request {
	body := `{"model": "` + reqModel + `", "messages": [{"role": "user", "content": "hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx = context.WithValue(ctx, virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, keyHash)
	return req.WithContext(ctx)
}

// wireMessage is the OpenAI error envelope's message.
func wireMessage(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not the OpenAI error shape: %v; body: %s", err, w.Body.String())
	}
	return resp.Error.Message
}

// A hotel/ group lookup the caller abandoned is their disconnect: a 499 row
// with the client_disconnect kind, never the 404 "unknown model" validation
// row, and nothing of the database's error text on the wire.
func TestResolveHotelModel_CancelledResolveIsAClientDisconnect(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()

	reqModel := "hotel/cancelled-" + uuid.NewString()[:8]
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(cancelledContext(), env.KeyHash, reqModel))

	if w.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want 499; body: %s", w.Code, w.Body.String())
	}
	if got := wireMessage(t, w); got != "client disconnected" {
		t.Errorf("wire message = %q, want the fixed disconnect message", got)
	}
	row := closedRowFor(t, env.Handler, reqModel)
	if row.status != statusClientClosedRequest || row.errorKind != string(KindClientDisconnect) {
		t.Errorf("row = %+v, want 499 client_disconnect", row)
	}
}

// A group lookup the database failed is a fault on this side: 500 with the
// fixed message on the wire and the row, never the pg text and never a 404.
func TestResolveHotelModel_DatabaseFaultIsA500WithoutItsText(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()
	env.Handler.failoverRepo = failover.NewRepository(unreachablePool(t))

	reqModel := "hotel/dbfault-" + uuid.NewString()[:8]
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(context.Background(), env.KeyHash, reqModel))

	assertResolveFault(t, env.Handler, w, reqModel)
}

// The specific-provider path draws the same three lines on its provider
// lookup: a cancel is the caller's, a fault is ours, and only a genuine miss
// is "provider not found".
func TestResolveSpecificProvider_CancelledResolveIsAClientDisconnect(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()

	reqModel := "cancelled-" + uuid.NewString()[:8] + "/some-model"
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(cancelledContext(), env.KeyHash, reqModel))

	if w.Code != statusClientClosedRequest {
		t.Fatalf("status = %d, want 499; body: %s", w.Code, w.Body.String())
	}
	row := closedRowFor(t, env.Handler, reqModel)
	if row.status != statusClientClosedRequest || row.errorKind != string(KindClientDisconnect) {
		t.Errorf("row = %+v, want 499 client_disconnect", row)
	}
}

func TestResolveSpecificProvider_ProviderDatabaseFaultIsA500WithoutItsText(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()
	env.Handler.providerRepo = provider.NewRepository(unreachablePool(t))

	reqModel := "dbfault-" + uuid.NewString()[:8] + "/some-model"
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(context.Background(), env.KeyHash, reqModel))

	assertResolveFault(t, env.Handler, w, reqModel)
}

func TestResolveSpecificProvider_ModelDatabaseFaultIsA500WithoutItsText(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()
	// The provider resolves from the live database; the model read fails.
	env.Handler.modelRepo = model.NewRepository(unreachablePool(t))

	reqModel := env.ProviderName + "/" + env.ModelName
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(context.Background(), env.KeyHash, reqModel))

	assertResolveFault(t, env.Handler, w, reqModel)
}

// A genuine miss on the provider stays the 404 validation answer, with the
// resolver's own clean text.
func TestResolveSpecificProvider_UnknownProviderIsStillA404(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()

	name := "no-such-provider-" + uuid.NewString()[:8]
	reqModel := name + "/some-model"
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(context.Background(), env.KeyHash, reqModel))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if got := wireMessage(t, w); got != "provider not found: "+name {
		t.Errorf("wire message = %q", got)
	}
	if row := closedRowFor(t, env.Handler, reqModel); row.errorKind != string(KindValidation) {
		t.Errorf("row = %+v, want the validation kind", row)
	}
}

func TestResolveSpecificProvider_UnknownModelIsStillA404(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()

	reqModel := env.ProviderName + "/no-such-model-" + uuid.NewString()[:8]
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(context.Background(), env.KeyHash, reqModel))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if got := wireMessage(t, w); !strings.HasPrefix(got, "model not found: ") {
		t.Errorf("wire message = %q, want the clean model-not-found text", got)
	}
}

// assertResolveFault checks a resolve fault's whole outcome: 500, the fixed
// message on the wire and the row, the internal kind, and nothing of the
// connection error anywhere the caller or the dashboard can read.
func assertResolveFault(t *testing.T, h *Handler, w *httptest.ResponseRecorder, reqModel string) {
	t.Helper()
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", w.Code, w.Body.String())
	}
	if got := wireMessage(t, w); got != resolveFailedMessage {
		t.Errorf("wire message = %q, want %q", got, resolveFailedMessage)
	}
	row := closedRowFor(t, h, reqModel)
	if row.status != http.StatusInternalServerError || row.errorKind != string(KindInternal) || row.errorMessage != resolveFailedMessage {
		t.Errorf("row = %+v, want 500 internal %q", row, resolveFailedMessage)
	}
}

// A disabled model named directly is still the 404 validation answer.
func TestResolveSpecificProvider_DisabledModelIsStillA404(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Handler.Close()
	if _, err := testDB.Pool().Exec(context.Background(),
		"UPDATE models SET enabled = false, disabled_manually = true WHERE id = $1", env.ModelID); err != nil {
		t.Fatalf("disable model: %v", err)
	}
	model.InvalidateModelCache()

	reqModel := env.ProviderName + "/" + env.ModelName
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, chatRequestFor(context.Background(), env.KeyHash, reqModel))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if got := wireMessage(t, w); got != "model or provider disabled" {
		t.Errorf("wire message = %q", got)
	}
}
