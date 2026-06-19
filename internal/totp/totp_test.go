package totp

import (
	"context"
	"log"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// ---------------------------------------------------------------------------
// TestMain - integration test database setup (mirrors internal/failover).
// ---------------------------------------------------------------------------

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("totp")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("totp")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// newTestRepo returns a Repository backed by the per-package test DB, with a
// cleanup that truncates the TOTP tables between tests so state never leaks.
func newTestRepo(t *testing.T, masterKey string) *Repository {
	t.Helper()
	repo := NewRepository(testDB.Pool(), masterKey)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testDB.Pool().Exec(ctx, `DELETE FROM admin_totp_recovery`)
		_, _ = testDB.Pool().Exec(ctx, `DELETE FROM admin_totp`)
	})
	return repo
}

var recoveryCodeRe = regexp.MustCompile(`^[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}$`)

func TestEnrollAndVerify(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	uri, secret, err := repo.Enroll(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, secret)
	assert.True(t, len(uri) > len("otpauth://totp/"), "uri should be an otpauth URL")
	assert.Equal(t, "otpauth://totp/", uri[:len("otpauth://totp/")])

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	ok, err := repo.Verify(ctx, code)
	require.NoError(t, err)
	assert.True(t, ok, "valid code must verify")
}

func TestVerifyWrongCode(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	_, _, err := repo.Enroll(ctx)
	require.NoError(t, err)

	// "000000" is a 1/1e6 collision candidate; if it validates, regenerate
	// once and retry so the test is deterministic in practice.
	for attempt := 0; attempt < 2; attempt++ {
		ok, err := repo.Verify(ctx, "000000")
		require.NoError(t, err)
		if !ok {
			return // expected: wrong code rejected
		}
		if attempt == 0 {
			_, _, _ = repo.Enroll(ctx) // rotate secret, retry
		}
	}
	t.Fatal("wrong code validated against a fresh enrollment (1/1e6 collision twice)")
}

func TestVerifySkewWindow(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	_, secret, err := repo.Enroll(ctx)
	require.NoError(t, err)

	// Skew=1 covers the prior 30s window (-30s) and the next one (+30s).
	code1, err := totp.GenerateCode(secret, time.Now().Add(-30*time.Second))
	require.NoError(t, err)
	ok, err := repo.Verify(ctx, code1)
	require.NoError(t, err)
	assert.True(t, ok, "code from -30s window must verify (skew=1)")

	code2, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	require.NoError(t, err)
	ok, err = repo.Verify(ctx, code2)
	require.NoError(t, err)
	assert.True(t, ok, "code from +30s window must verify (skew=1)")

	// -90s is two windows before the current -> outside skew=1, must fail.
	code3, err := totp.GenerateCode(secret, time.Now().Add(-90*time.Second))
	require.NoError(t, err)
	ok, err = repo.Verify(ctx, code3)
	require.NoError(t, err)
	assert.False(t, ok, "code from -90s window must NOT verify (outside skew)")
}

func TestVerifyNoEnrollment(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	// No Enroll call: Verify must return (false, nil) - not an error.
	ok, err := repo.Verify(ctx, "123456")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRecoveryCodesSingleUse(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	codes, err := repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	require.Len(t, codes, 10)
	for _, c := range codes {
		assert.True(t, recoveryCodeRe.MatchString(c), "code %q must match XXXX-XXXX-XXXX-XXXX base32", c)
	}

	// First use of codes[0] succeeds.
	ok, err := repo.ConsumeRecoveryCode(ctx, codes[0])
	require.NoError(t, err)
	assert.True(t, ok, "first use of a recovery code must succeed")

	// Second use of the same code fails (single-use enforced).
	ok, err = repo.ConsumeRecoveryCode(ctx, codes[0])
	require.NoError(t, err)
	assert.False(t, ok, "reuse of a recovery code must fail")

	// A different unused code from the set still works.
	ok, err = repo.ConsumeRecoveryCode(ctx, codes[1])
	require.NoError(t, err)
	assert.True(t, ok, "second distinct code must succeed")

	// A bogus code that was never issued fails.
	ok, err = repo.ConsumeRecoveryCode(ctx, "BOGUS-CODE-XXXX-YYYY")
	require.NoError(t, err)
	assert.False(t, ok, "bogus code must fail")
}

func TestGenerateRecoveryCodesReplacesExisting(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	setA, err := repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)

	setB, err := repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, setA, setB, "regenerating must produce a new set")

	// An old code from set A is now invalid (replaced, not just consumed).
	ok, err := repo.ConsumeRecoveryCode(ctx, setA[0])
	require.NoError(t, err)
	assert.False(t, ok, "old recovery code must be invalid after regeneration")

	// A fresh code from set B still works.
	ok, err = repo.ConsumeRecoveryCode(ctx, setB[0])
	require.NoError(t, err)
	assert.True(t, ok, "new recovery code from set B must succeed")
}

func TestEnableDisableLifecycle(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	// No row -> disabled (not an error).
	enabled, err := repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)

	_, _, err = repo.Enroll(ctx)
	require.NoError(t, err)

	// Provisional enrollment -> still disabled until Enable.
	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)

	require.NoError(t, repo.Enable(ctx))
	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)

	require.NoError(t, repo.Disable(ctx))
	// Disable deletes the row -> back to disabled.
	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestEnableWithoutEnroll(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	// No provisional row -> Enable must error (rows affected = 0).
	err := repo.Enable(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no provisional enrollment")
}

func TestDecryptWrongMasterKey(t *testing.T) {
	ctx := context.Background()

	repo1 := newTestRepo(t, "correct-master-key-very-long-32+")
	_, secret, err := repo1.Enroll(ctx)
	require.NoError(t, err)

	// A valid code generated from the real secret.
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	// A repo with the WRONG master key cannot decrypt: Verify must return
	// (false, err), not silently pass.
	repo2 := NewRepository(testDB.Pool(), "wrong-master-key-also-longish!")
	// repo2 shares the DB with repo1 (same row), so clean up via the same cleanup.
	ok, err := repo2.Verify(ctx, code)
	require.Error(t, err, "decrypt with wrong master key must error")
	assert.False(t, ok)
}

func TestReEnrollResets(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	_, secret1, err := repo.Enroll(ctx)
	require.NoError(t, err)

	require.NoError(t, repo.Enable(ctx))
	enabled, err := repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)

	// Re-enroll overwrites the secret and resets enabled=false + confirmed_at=NULL.
	_, secret2, err := repo.Enroll(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, secret1, secret2, "re-enroll must rotate the secret")

	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled, "re-enroll must reset enabled to false")

	// A code generated from secret1 (the old secret) must no longer verify:
	// the stored secret is now secret2, even though the row still exists.
	code1, err := totp.GenerateCode(secret1, time.Now())
	require.NoError(t, err)
	ok, err := repo.Verify(ctx, code1)
	require.NoError(t, err)
	assert.False(t, ok, "old secret must not validate after re-enroll")
}

func TestVerifyRejectsReplay(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	_, secret, err := repo.Enroll(ctx)
	require.NoError(t, err)

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	ok, err := repo.Verify(ctx, code)
	require.NoError(t, err)
	assert.True(t, ok, "first use of a valid code must verify")

	// RFC 6238 §5.2: the same code (same step) must not be accepted twice.
	ok, err = repo.Verify(ctx, code)
	require.NoError(t, err)
	assert.False(t, ok, "replay of an already-used code must be rejected")
}

func TestNormalizeRecoveryCode(t *testing.T) {
	const canonical = "ABCD-EFGH-IJKL-MNOP"
	cases := []struct{ in, want string }{
		{"abcd-efgh-ijkl-mnop", canonical},             // lowercase
		{"ABCDEFGHIJKLMNOP", canonical},                // missing dashes
		{"abcd efgh ijkl mnop", canonical},             // spaces instead of dashes
		{"  ABCD-EFGH-IJKL-MNOP  ", canonical},         // surrounding whitespace
		{"ABCD-EFGH", "ABCDEFGH"},                      // wrong length -> cleaned, ungrouped
		{"2345-6723-4567-2345", "2345-6723-4567-2345"}, // base32 digits 2-7 kept
		{"AB01-89CD", "ABCD"},                          // non-base32 digits 0/1/8/9 dropped
	}
	for _, c := range cases {
		if got := normalizeRecoveryCode(c.in); got != c.want {
			t.Errorf("normalizeRecoveryCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDisableWithCode(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	// 1) A valid TOTP code authorizes the disable.
	_, secret, err := repo.Enroll(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	ok, err := repo.DisableWithCode(ctx, code)
	require.NoError(t, err)
	assert.True(t, ok, "valid TOTP code must authorize disable")
	en, err := repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, en, "TOTP must be disabled after a valid code")

	// 2) A valid (unused) recovery code authorizes the disable.
	_, _, err = repo.Enroll(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))
	codes, err := repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	ok, err = repo.DisableWithCode(ctx, codes[0])
	require.NoError(t, err)
	assert.True(t, ok, "valid recovery code must authorize disable")

	// 3) An invalid code changes nothing (TOTP stays enabled).
	_, _, err = repo.Enroll(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))
	ok, err = repo.DisableWithCode(ctx, "000000")
	require.NoError(t, err)
	assert.False(t, ok, "invalid code must not authorize disable")
	en, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, en, "TOTP must remain enabled after a failed disable")
}

func TestDisableWithCode_NotEnrolled(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ok, err := repo.DisableWithCode(context.Background(), "123456")
	require.NoError(t, err)
	assert.False(t, ok, "disable on a non-enrolled instance is a no-op")
}

func TestDisableWithCode_RecoveryWorksWithoutDecryptableSecret(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	_, _, err := repo.Enroll(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))
	codes, err := repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)

	// A repo with the WRONG master key cannot decrypt the secret, so the TOTP
	// auth path is skipped -- but a valid recovery code must still disable.
	repo2 := NewRepository(testDB.Pool(), "wrong-master-key-also-longish!")
	ok, err := repo2.DisableWithCode(ctx, codes[0])
	require.NoError(t, err)
	assert.True(t, ok, "recovery code disables even when the secret cannot be decrypted")
}

func TestDisableWithCode_RejectsReusedTotpStep(t *testing.T) {
	repo := newTestRepo(t, "test-master-key-very-long-32b+")
	ctx := context.Background()

	_, secret, err := repo.Enroll(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)

	// Consume the code's step via Verify (as a login would).
	ok, err := repo.Verify(ctx, code)
	require.NoError(t, err)
	require.True(t, ok)

	// The same code must NOT authorize disable: a used step is single-use even
	// for the disable confirmation.
	ok, err = repo.DisableWithCode(ctx, code)
	require.NoError(t, err)
	assert.False(t, ok, "a TOTP code already used for login must not authorize disable")
	en, err := repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, en, "TOTP must stay enabled when the replayed code is rejected")
}
