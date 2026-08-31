package provider

import (
	"errors"
	"net/http"
	"testing"
)

// Test errorStatusCode function
func TestErrorStatusCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "httpError with status code",
			err:  &httpError{StatusCode: http.StatusForbidden},
			want: http.StatusForbidden,
		},
		{
			name: "httpError with 0 status",
			err:  &httpError{StatusCode: 0},
			want: 0,
		},
		{
			name: "httpError with 429 status",
			err:  &httpError{StatusCode: http.StatusTooManyRequests},
			want: http.StatusTooManyRequests,
		},
		{
			name: "httpError with 500 status",
			err:  &httpError{StatusCode: http.StatusInternalServerError},
			want: http.StatusInternalServerError,
		},
		{
			name: "regular error",
			err:  errors.New("some error"),
			want: 0,
		},
		{
			name: "nil error",
			err:  nil,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errorStatusCode(tt.err)
			if got != tt.want {
				t.Errorf("errorStatusCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
