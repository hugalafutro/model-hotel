package main

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The body-peeking timeout middleware reaches the proxy through Register's
// after-auth slot, never through a plain r.Use ahead of the key check: the
// router carries no middleware of its own and Register receives exactly one.
func TestMountProxyRoutes_PeekIsHandedToRegister(t *testing.T) {
	r := chi.NewRouter()
	var got int
	var seen chi.Router
	mountProxyRoutes(r, func(rr chi.Router, afterAuth ...func(http.Handler) http.Handler) {
		seen = rr
		got = len(afterAuth)
	})
	if seen != r {
		t.Fatal("Register was not given the route's router")
	}
	if got != 1 {
		t.Fatalf("Register received %d after-auth middlewares, want the one peek", got)
	}
	if n := len(r.Middlewares()); n != 0 {
		t.Fatalf("the route mounted %d middlewares ahead of Register, want none", n)
	}
}
