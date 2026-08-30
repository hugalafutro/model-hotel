package frontdesk

import (
	"context"
	"errors"
	"testing"
	"time"

	totppkg "github.com/hugalafutro/model-hotel/internal/totp"
)

// enrolTOTP puts a provisional enrollment in place and returns the store under
// test. The secret bytes are opaque to the store: it only round-trips them.
func enrolTOTP(t *testing.T, st *TOTPStore) {
	t.Helper()
	if err := st.UpsertEnrollment(context.Background(), []byte("cipher"), []byte("nonce"), []byte("salt")); err != nil {
		t.Fatalf("UpsertEnrollment: %v", err)
	}
}

// A Front Desk with no TOTP enrollment must read as "not enrolled" on every
// accessor rather than as an error. sql.ErrNoRows reaching the caller here
// would turn the login and settings surfaces into 500s for the common case of
// an instance that never enrolled a second factor.
func TestTOTPStoreReadsWhenNotEnrolled(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	ctx := context.Background()

	sec, ok, err := st.LoadSecret(ctx)
	if err != nil {
		t.Fatalf("LoadSecret: %v", err)
	}
	if ok {
		t.Error("LoadSecret reports an enrollment on an empty store")
	}
	if len(sec.Cipher) != 0 || len(sec.Nonce) != 0 || len(sec.Salt) != 0 {
		t.Errorf("LoadSecret returned a non-zero secret: %+v", sec)
	}

	enabled, err := st.IsEnabled(ctx)
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if enabled {
		t.Error("IsEnabled true with no enrollment")
	}

	at, ok, err := st.EnabledAt(ctx)
	if err != nil {
		t.Fatalf("EnabledAt: %v", err)
	}
	if ok || !at.IsZero() {
		t.Errorf("EnabledAt = (%v, %v), want (zero, false)", at, ok)
	}

	step, ok, err := st.LastUsedStep(ctx)
	if err != nil {
		t.Fatalf("LastUsedStep: %v", err)
	}
	if ok || step != nil {
		t.Errorf("LastUsedStep = (%v, %v), want (nil, false)", step, ok)
	}
}

// EnabledAt is what the settings screen stamps "protected since"; it must stay
// silent until the enrollment is both enabled and confirmed. A provisional
// enrollment (secret stored, code not yet verified) is not protection.
func TestTOTPStoreEnabledAtNeedsAConfirmedEnrollment(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	ctx := context.Background()
	enrolTOTP(t, st)

	if _, ok, err := st.EnabledAt(ctx); err != nil || ok {
		t.Fatalf("EnabledAt on a provisional enrollment = ok:%v err:%v, want ok:false", ok, err)
	}
	// The enrollment row exists now, so LastUsedStep answers ok with a NULL step.
	step, ok, err := st.LastUsedStep(ctx)
	if err != nil || !ok || step != nil {
		t.Fatalf("LastUsedStep after enroll = (%v, %v, %v), want (nil, true, nil)", step, ok, err)
	}

	before := time.Now().Add(-time.Second)
	if flipped, err := st.Enable(ctx); err != nil || !flipped {
		t.Fatalf("Enable = %v, %v", flipped, err)
	}
	at, ok, err := st.EnabledAt(ctx)
	if err != nil || !ok {
		t.Fatalf("EnabledAt after Enable = ok:%v err:%v, want ok:true", ok, err)
	}
	if at.Before(before) || at.After(time.Now().Add(time.Second)) {
		t.Errorf("confirmed_at %v is not the moment of Enable", at)
	}
}

// Disable is the "turn it off" path: it must take the recovery codes with it.
// Leaving them behind would let a stale code authorize a later disable of a
// re-enrollment, so the deletion is part of the contract, not a tidy-up.
func TestTOTPStoreDisableClearsSecretAndRecoveryCodes(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	ctx := context.Background()
	enrolTOTP(t, st)
	if err := st.ReplaceRecoveryCodes(ctx, []string{"hash-a", "hash-b"}); err != nil {
		t.Fatalf("ReplaceRecoveryCodes: %v", err)
	}

	if err := st.Disable(ctx); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, ok, err := st.LoadSecret(ctx); err != nil || ok {
		t.Errorf("secret survived Disable: ok=%v err=%v", ok, err)
	}
	remaining, total, err := st.RecoveryCounts(ctx)
	if err != nil {
		t.Fatalf("RecoveryCounts: %v", err)
	}
	if remaining != 0 || total != 0 {
		t.Errorf("recovery codes survived Disable: remaining=%d total=%d", remaining, total)
	}
}

// With nothing enrolled there is no secret to authorize against, so the
// authorizer must not run at all and the answer is a plain "not authorized",
// not an error the caller would have to special-case.
func TestTOTPStoreDisableIfAuthorizedSkipsAuthorizerWhenNotEnrolled(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	called := false
	authorized, err := st.DisableIfAuthorized(context.Background(),
		func(totppkg.EncryptedSecret, *int64, func(string) (bool, error)) (bool, error) {
			called = true
			return true, nil
		})
	if err != nil {
		t.Fatalf("DisableIfAuthorized: %v", err)
	}
	if authorized {
		t.Error("disable authorized with no enrollment")
	}
	if called {
		t.Error("authorizer ran with no enrollment to authorize against")
	}
}

// A refused code must leave the enrollment exactly as it was: the transaction
// commits, but nothing inside it deleted anything.
func TestTOTPStoreDisableIfAuthorizedDenialKeepsEnrollment(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	ctx := context.Background()
	enrolTOTP(t, st)
	if err := st.ReplaceRecoveryCodes(ctx, []string{"hash-a"}); err != nil {
		t.Fatalf("ReplaceRecoveryCodes: %v", err)
	}

	authorized, err := st.DisableIfAuthorized(ctx,
		func(totppkg.EncryptedSecret, *int64, func(string) (bool, error)) (bool, error) {
			return false, nil
		})
	if err != nil {
		t.Fatalf("DisableIfAuthorized: %v", err)
	}
	if authorized {
		t.Fatal("a refused code reported as authorized")
	}
	if _, ok, _ := st.LoadSecret(ctx); !ok {
		t.Error("enrollment removed despite a refused code")
	}
	if remaining, _, _ := st.RecoveryCounts(ctx); remaining != 1 {
		t.Errorf("recovery codes removed despite a refused code: remaining=%d", remaining)
	}
}

// An authorizer failure (a decrypt error, say) must surface and roll the
// transaction back rather than read as a refusal that quietly kept 2FA on
// while hiding the real fault.
func TestTOTPStoreDisableIfAuthorizedSurfacesAuthorizerError(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	ctx := context.Background()
	enrolTOTP(t, st)
	sentinel := errors.New("cannot decrypt secret")

	authorized, err := st.DisableIfAuthorized(ctx,
		func(totppkg.EncryptedSecret, *int64, func(string) (bool, error)) (bool, error) {
			return false, sentinel
		})
	if !errors.Is(err, sentinel) {
		t.Fatalf("DisableIfAuthorized err = %v, want %v", err, sentinel)
	}
	if authorized {
		t.Error("authorized reported true alongside an error")
	}
	if _, ok, _ := st.LoadSecret(ctx); !ok {
		t.Error("enrollment removed on an authorizer error")
	}
}

// The authorizer sees the stored secret, the last accepted step, and a
// recovery-code probe that reads inside the same transaction. The probe must
// answer true only for a code that exists AND is unused, so a consumed code
// cannot disable 2FA a second time.
func TestTOTPStoreDisableIfAuthorizedProbesRecoveryCodes(t *testing.T) {
	st := NewTOTPStore(newTestStore(t))
	ctx := context.Background()
	enrolTOTP(t, st)
	if err := st.ReplaceRecoveryCodes(ctx, []string{"fresh", "spent"}); err != nil {
		t.Fatalf("ReplaceRecoveryCodes: %v", err)
	}
	if used, err := st.ConsumeRecoveryCode(ctx, "spent"); err != nil || !used {
		t.Fatalf("ConsumeRecoveryCode: %v %v", used, err)
	}
	if ok, err := st.RecordUsedStep(ctx, 4242); err != nil || !ok {
		t.Fatalf("RecordUsedStep: %v %v", ok, err)
	}

	var gotSecret totppkg.EncryptedSecret
	var gotStep *int64
	probe := map[string]bool{}
	authorized, err := st.DisableIfAuthorized(ctx,
		func(sec totppkg.EncryptedSecret, lastStep *int64, recoveryUnused func(string) (bool, error)) (bool, error) {
			gotSecret, gotStep = sec, lastStep
			for _, h := range []string{"fresh", "spent", "never-issued"} {
				v, perr := recoveryUnused(h)
				if perr != nil {
					return false, perr
				}
				probe[h] = v
			}
			return true, nil
		})
	if err != nil {
		t.Fatalf("DisableIfAuthorized: %v", err)
	}
	if !authorized {
		t.Fatal("an accepted code did not authorize the disable")
	}
	if string(gotSecret.Cipher) != "cipher" || string(gotSecret.Nonce) != "nonce" || string(gotSecret.Salt) != "salt" {
		t.Errorf("authorizer got the wrong secret: %+v", gotSecret)
	}
	if gotStep == nil || *gotStep != 4242 {
		t.Errorf("authorizer got last step %v, want 4242", gotStep)
	}
	if !probe["fresh"] {
		t.Error("an unused recovery code probed as unavailable")
	}
	if probe["spent"] {
		t.Error("an already-consumed recovery code probed as unused")
	}
	if probe["never-issued"] {
		t.Error("an unknown recovery code probed as unused")
	}
	if _, ok, _ := st.LoadSecret(ctx); ok {
		t.Error("enrollment survived an authorized disable")
	}
	if _, total, _ := st.RecoveryCounts(ctx); total != 0 {
		t.Errorf("recovery codes survived an authorized disable: total=%d", total)
	}
}
