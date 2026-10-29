package provider

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// A URL that fails to parse is quoted whole, in %q form, by the parse error.
// rawURLSecrets lists its userinfo and credential query values both raw and in
// that rendering, so the masked error carries neither, including when the %q
// form differs from the raw one (a quote, a control byte) and when the
// userinfo holds a space the URL-userinfo pattern stops at.
func TestRawURLSecrets_MasksUnparseableURLCredentials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    string
		secret string // the raw credential as it sits in the URL
	}{
		{"userinfo with a space", "http://admin:open sesame99@example.invalid/v1", "admin:open sesame99"},
		{"userinfo with a quote and newline", "http://admin:hunter\"2\nopensesame@example.invalid/v1", "admin:hunter\"2\nopensesame"},
		{"query key with a DEL byte", "http://example.invalid/v1?key=opensesame\x7fpassword", "opensesame\x7fpassword"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := url.Parse(tc.raw)
			if err == nil {
				t.Fatalf("url.Parse(%q) succeeded, want a parse error that quotes the URL", tc.raw)
			}
			quoted := strconv.Quote(tc.secret)
			quoted = quoted[1 : len(quoted)-1]
			if !strings.Contains(err.Error(), quoted) {
				t.Fatalf("parse error %q does not quote the credential as %q; the case no longer exercises the masking", err.Error(), quoted)
			}

			got := maskRawURLText(rawURLSecrets(tc.raw), err.Error())
			for _, form := range []string{tc.secret, quoted, "opensesame"} {
				if strings.Contains(got, form) {
					t.Errorf("masked text %q still carries %q", got, form)
				}
			}
		})
	}
}
