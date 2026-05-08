package provider

import (
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// isNoAccessError
// ---------------------------------------------------------------------------

func TestIsNoAccessError_Nil(t *testing.T) {
	if isNoAccessError(nil) {
		t.Error("isNoAccessError(nil) = true, want false")
	}
}

func TestIsNoAccessError_Forbidden(t *testing.T) {
	err := &httpError{StatusCode: http.StatusForbidden}
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(403) = false, want true")
	}
}

func TestIsNoAccessError_TooManyRequests(t *testing.T) {
	err := &httpError{StatusCode: http.StatusTooManyRequests}
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(429) = false, want true")
	}
}

func TestIsNoAccessError_Unauthorized(t *testing.T) {
	err := &httpError{StatusCode: http.StatusUnauthorized}
	if isNoAccessError(err) {
		t.Error("isNoAccessError(401) = true, want false")
	}
}

func TestIsNoAccessError_InternalServerError(t *testing.T) {
	err := &httpError{StatusCode: http.StatusInternalServerError}
	if isNoAccessError(err) {
		t.Error("isNoAccessError(500) = true, want false")
	}
}

func TestIsNoAccessError_OtherErrorType(t *testing.T) {
	err := http.ErrAbortHandler
	if isNoAccessError(err) {
		t.Error("isNoAccessError(non-httpError) = true, want false")
	}
}

func TestIsNoAccessError_Pointer(t *testing.T) {
	err := &httpError{StatusCode: http.StatusForbidden}
	if !isNoAccessError(err) {
		t.Error("isNoAccessError(&httpError{403}) = false, want true")
	}
}

func TestHTTPError_Error(t *testing.T) {
	err := &httpError{StatusCode: 418, Body: "I'm a teapot"}
	msg := err.Error()
	if msg != "unexpected status 418" {
		t.Errorf("Error() = %q, want %q", msg, "unexpected status 418")
	}
}
