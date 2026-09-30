package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// An images answer whose picture is empty is no image, so it fails over to a
// sibling that draws one. KoboldCpp answers a failed generation this way:
// {"data":[{"b64_json":""}]} under HTTP 200.
func TestImageGenerations_EmptyImageFailsOver(t *testing.T) {
	var emptyCalls, drawnCalls atomic.Int32
	envEmpty := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		emptyCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":""}]}`)
	}))
	drawn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		drawnCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"aW1hZ2U="}]}`)
	}))
	t.Cleanup(drawn.Close)
	_, _, drawnModelUUID, _ := createMultimodalProvider(t, drawn.URL)

	group := envEmpty.modelName
	if _, err := failover.NewRepository(testDB.Pool()).UpsertWithConfig(context.Background(), group,
		[]uuid.UUID{envEmpty.modelUUID, drawnModelUUID},
		map[string]bool{envEmpty.modelUUID.String(): true, drawnModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("create failover group: %v", err)
	}

	w := httptest.NewRecorder()
	envEmpty.handler.ImageGenerations(w, envEmpty.request("/v1/images/generations", "application/json",
		strings.NewReader(fmt.Sprintf(`{"model":"hotel/%s","prompt":"a circle"}`, group))))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "aW1hZ2U=") {
		t.Fatalf("status = %d body = %s, want the sibling's image", w.Code, w.Body.String())
	}
	if emptyCalls.Load() != 1 || drawnCalls.Load() != 1 {
		t.Errorf("calls empty=%d drawn=%d, want 1 each", emptyCalls.Load(), drawnCalls.Load())
	}
}
