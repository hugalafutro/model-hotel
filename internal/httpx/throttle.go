package httpx

import (
	"math"
	"net/http"
	"strconv"
	"time"
)

// SetRetryAfter writes the Retry-After header for a duration the caller must
// wait. The value is rounded up and never below 1, so a sub-second backoff
// still tells the client to pause rather than advertising an immediate retry.
func SetRetryAfter(w http.ResponseWriter, d time.Duration) {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}

// RespondTooManyAttempts answers a login-style endpoint that has run out of
// attempt budget: Retry-After plus the uniform 429 body every credential
// surface returns, so a caller cannot tell throttled endpoints apart by their
// wording.
func RespondTooManyAttempts(w http.ResponseWriter, retry time.Duration) {
	SetRetryAfter(w, retry)
	http.Error(w, "too many failed attempts, try again later", http.StatusTooManyRequests)
}
