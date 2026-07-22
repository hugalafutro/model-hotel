// Package pwned checks passwords against the Have I Been Pwned "Pwned
// Passwords" range API using the k-anonymity model: only the first five hex
// characters of the password's SHA-1 hash ever leave the process, and the
// endpoint returns every stored suffix sharing that prefix for the caller to
// match locally. The plaintext password and its full hash are never
// transmitted. The endpoint is a fixed, operator-configured URL (default
// api.pwnedpasswords.com, overridable for a self-hosted mirror), so there is
// no user-controlled URL and therefore no SSRF surface.
package pwned

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // SHA-1 is the HIBP range-API wire contract, not used as a security primitive
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRangeBody caps how much of a range response we read. A prefix returns on
// the order of a thousand 40-byte lines; 4 MiB is comfortably above that while
// bounding memory if a mirror misbehaves.
const maxRangeBody = 4 << 20

// Checker queries a HIBP-compatible Pwned Passwords range endpoint.
type Checker struct {
	baseURL string
	client  *http.Client
}

// New returns a Checker for the given base URL (e.g.
// "https://api.pwnedpasswords.com" or a self-hosted mirror). A nil client is
// replaced with one carrying a short timeout so a slow or unreachable endpoint
// cannot stall a password change; callers treat any error as "allow" (fail
// open), so the timeout bounds worst-case latency rather than blocking users.
func New(baseURL string, client *http.Client) *Checker {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &Checker{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  client,
	}
}

// Breached reports whether password appears in the breach corpus and, if so,
// how many times. It sends only the 5-character SHA-1 prefix and matches the
// returned suffixes locally. Any transport error or non-200 status is returned
// to the caller, which decides how to fail (the callers in this project fail
// open). Padding decoys (count 0) never count as a match.
func (c *Checker) Breached(ctx context.Context, password string) (bool, int, error) {
	// SHA-1 is the HIBP range API's wire contract for k-anonymity, not password
	// storage: only the 5-char prefix below is ever sent and the password is
	// never persisted as this hash. The gosec/CodeQL weak-hash warnings key on
	// the word "password" reaching sha1 and cannot see that distinction.
	sum := sha1.Sum([]byte(password)) //nolint:gosec // codeql[go/weak-sensitive-data-hashing] -- HIBP k-anonymity wire contract, not a security primitive
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hash[:5], hash[5:]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/range/"+prefix, http.NoBody)
	if err != nil {
		return false, 0, err
	}
	// Add-Padding pads the response with decoy suffixes (count 0) so the number
	// of returned lines cannot leak how many real matches a prefix has. Honored
	// by HIBP and compatible mirrors; harmlessly ignored otherwise.
	req.Header.Set("Add-Padding", "true")

	resp, err := c.client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, 0, fmt.Errorf("pwned: range endpoint returned status %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(io.LimitReader(resp.Body, maxRangeBody))
	for sc.Scan() {
		sfx, cnt, ok := strings.Cut(sc.Text(), ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(sfx), suffix) {
			continue
		}
		count, _ := strconv.Atoi(strings.TrimSpace(cnt))
		return count > 0, count, nil
	}
	if err := sc.Err(); err != nil {
		return false, 0, err
	}
	return false, 0, nil
}
