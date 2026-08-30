package api

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// The url and url_public branches of validateSyncedSetting are the standing
// SSRF guard on the import path: a compromised primary must not be able to
// write through config sync a URL the interactive settings endpoint would
// refuse. No syncable setting is url-typed today (every one is instance-local
// and skipped earlier), so this is the only thing holding the guard in place
// for the day one becomes syncable.
//
// The two types are deliberately different, and that difference is the point.
// A "url" is fetched by the server, so a literal metadata/link-local address is
// refused. A "url_public" is only reflected into a redirect URI and never
// dialled, so it is checked for shape alone: an operator whose external origin
// happens to be a link-local address must still be able to set it.
func TestValidateSyncedSetting_URLTypes(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		// url: the server fetches these, so blocked literals are refused.
		{name: "apprise metadata address", key: "alert_apprise_api_url", value: "http://169.254.169.254/latest/meta-data/", wantErr: true},
		{name: "apprise unspecified address", key: "alert_apprise_api_url", value: "http://0.0.0.0:8000", wantErr: true},
		{name: "oidc issuer link-local", key: "oidc_issuer_url", value: "https://169.254.169.254/", wantErr: true},
		{name: "oidc issuer non-http scheme", key: "oidc_issuer_url", value: "file:///etc/passwd", wantErr: true},
		{name: "oidc issuer hostless", key: "oidc_issuer_url", value: "https://", wantErr: true},
		{name: "apprise ordinary host", key: "alert_apprise_api_url", value: "http://apprise:8000"},
		{name: "oidc issuer ordinary origin", key: "oidc_issuer_url", value: "https://idp.example.com/realms/mh"},
		{name: "empty clears the url setting", key: "alert_apprise_api_url", value: ""},

		// url_public: never dialled, so only the shape is enforced.
		{name: "public base non-http scheme", key: "oidc_public_base_url", value: "gopher://example.com", wantErr: true},
		{name: "public base hostless", key: "github_public_base_url", value: "https:///callback", wantErr: true},
		{name: "public base ordinary origin", key: "oidc_public_base_url", value: "https://hotel.example.com"},
		{name: "public base link-local origin is the operator's business", key: "github_public_base_url", value: "http://169.254.169.254:8080"},
		{name: "empty clears the public base url", key: "oidc_public_base_url", value: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSyncedSetting(tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSyncedSetting(%q, %q) error = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
			}
			if err == nil {
				return
			}
			if !errors.Is(err, errInvalidSyncedURL) {
				t.Fatalf("error %v does not wrap errInvalidSyncedURL", err)
			}
			// The import response quotes this error to the operator, so it has
			// to name the offending key: "some URL was invalid" is not enough
			// to find it among a whole envelope of settings. The key is matched
			// WITH its quotes, so a short key cannot pass by appearing as a
			// prefix of a longer one inside the netguard message.
			if !strings.Contains(err.Error(), strconv.Quote(tt.key)) {
				t.Errorf("error %q does not name the key %q", err.Error(), tt.key)
			}
		})
	}
}
