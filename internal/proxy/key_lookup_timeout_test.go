package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// deadlineRepo records the deadline the key lookup ran under and answers
// with the error a wedged database produces once it expires.
type deadlineRepo struct {
	*mockVirtualKeyRepoWithFuncs
	deadline time.Duration
	err      error
}

func (d *deadlineRepo) FindByKeyHash(ctx context.Context, _ string) (*VirtualKeyInfo, error) {
	if dl, ok := ctx.Deadline(); ok {
		d.deadline = time.Until(dl)
	}
	return nil, d.err
}

// The key lookup runs under its own bound now that the timeout middleware
// sits behind it: a database that does not answer yields a 500 rather than
// a request parked until the client gives up.
func TestProxyKeyMiddleware_KeyLookupIsBounded(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"deadline exceeded is a 500", context.DeadlineExceeded, http.StatusInternalServerError},
		{"a cancelled lookup on a live request is a 500 too", context.Canceled, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)
			repo := &deadlineRepo{mockVirtualKeyRepoWithFuncs: &mockVirtualKeyRepoWithFuncs{}, err: tc.err}
			h.virtualKeyRepo = repo
			r := chi.NewRouter()
			h.Register(r)
			req := httptest.NewRequest(http.MethodPost, "/chat/completions", strings.NewReader(`{}`))
			req.Header.Set("Authorization", "Bearer sk-bounded-lookup-key-0123456789")
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rr.Code, tc.want, rr.Body.String())
			}
			if repo.deadline <= 0 || repo.deadline > keyLookupTimeout {
				t.Fatalf("lookup ran with deadline %v from now, want within %v", repo.deadline, keyLookupTimeout)
			}
		})
	}
}
