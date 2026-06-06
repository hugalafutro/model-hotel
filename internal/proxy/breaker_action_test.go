package proxy

import (
	"testing"
)

func TestBreakerRecordAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		want       breakerAction
	}{
		// Failure actions — provider is unhealthy.
		{name: "500_internal_error", statusCode: 500, want: breakerActionFailure},
		{name: "502_bad_gateway", statusCode: 502, want: breakerActionFailure},
		{name: "503_service_unavailable", statusCode: 503, want: breakerActionFailure},
		{name: "504_gateway_timeout", statusCode: 504, want: breakerActionFailure},
		{name: "429_rate_limited", statusCode: 429, want: breakerActionFailure},
		{name: "401_unauthorized", statusCode: 401, want: breakerActionFailure},
		{name: "403_forbidden", statusCode: 403, want: breakerActionFailure},

		// No-op actions — model-specific client error; provider is alive.
		{name: "404_not_found", statusCode: 404, want: breakerActionNoOp},
		{name: "499_client_closed", statusCode: 499, want: breakerActionNoOp},

		// Success actions — provider responded normally.
		{name: "200_ok", statusCode: 200, want: breakerActionSuccess},
		{name: "201_created", statusCode: 201, want: breakerActionSuccess},
		{name: "400_bad_request", statusCode: 400, want: breakerActionSuccess},
		{name: "408_request_timeout", statusCode: 408, want: breakerActionSuccess},
		{name: "409_conflict", statusCode: 409, want: breakerActionSuccess},
		{name: "422_unprocessable", statusCode: 422, want: breakerActionSuccess},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := breakerRecordAction(tt.statusCode)
			if got != tt.want {
				t.Errorf("breakerRecordAction(%d) = %v, want %v", tt.statusCode, got, tt.want)
			}
		})
	}
}
