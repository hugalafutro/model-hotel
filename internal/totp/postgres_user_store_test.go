package totp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newUserTestRepo creates a users row and returns a Repository bound to its
// user_totp rows through UserPostgresStore, exercising the per-user store the
// same way main.go wires it. Deleting the users row on cleanup also proves
// the ON DELETE CASCADE on both per-user tables.
func newUserTestRepo(t *testing.T, masterKey string) (*Repository, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := testDB.Pool().QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`,
		"totp-store-"+uuid.NewString()).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return NewRepositoryWithStore(NewUserPostgresStore(testDB.Pool(), id), masterKey), id
}

const userStoreMasterKey = "user-store-master-key-32-bytes+"

func TestUserStore_FullLifecycle(t *testing.T) {
	repo, _ := newUserTestRepo(t, userStoreMasterKey)
	ctx := context.Background()

	// Nothing enrolled yet: every read is the zero state, not an error.
	ok, err := repo.Verify(ctx, "123456")
	require.NoError(t, err)
	assert.False(t, ok)
	enabled, err := repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
	_, has, err := repo.EnabledAt(ctx)
	require.NoError(t, err)
	assert.False(t, has)

	uri, secret, err := repo.EnrollAs(ctx, "alice")
	require.NoError(t, err)
	assert.Contains(t, uri, "alice")

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	ok, err = repo.Verify(ctx, code)
	require.NoError(t, err)
	assert.True(t, ok)

	// Replay of the same step must be refused (RecordUsedStep=false path).
	ok, err = repo.Verify(ctx, code)
	require.NoError(t, err)
	assert.False(t, ok, "same-step replay must fail")

	codes, err := repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, codes)
	require.NoError(t, repo.Enable(ctx))

	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
	at, has, err := repo.EnabledAt(ctx)
	require.NoError(t, err)
	assert.True(t, has)
	assert.WithinDuration(t, time.Now(), at, time.Minute)

	info, err := repo.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, len(codes), info.RecoveryTotal)
	assert.Equal(t, len(codes), info.RecoveryRemaining)

	// A wrong code neither disables nor burns anything.
	ok, err = repo.DisableWithCode(ctx, "000000")
	require.NoError(t, err)
	assert.False(t, ok)
	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)

	// A recovery code disables and is consumed atomically with the disable.
	ok, err = repo.DisableWithCode(ctx, codes[0])
	require.NoError(t, err)
	assert.True(t, ok)
	enabled, err = repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestUserStore_DisableWithTotpCodeAndReEnroll(t *testing.T) {
	repo, _ := newUserTestRepo(t, userStoreMasterKey)
	ctx := context.Background()

	_, secret, err := repo.EnrollAs(ctx, "bob")
	require.NoError(t, err)
	_, err = repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))

	// Disable authorized by a live TOTP code (DisableIfAuthorized TOTP arm).
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	ok, err := repo.DisableWithCode(ctx, code)
	require.NoError(t, err)
	assert.True(t, ok)

	// Re-enrolling after a disable overwrites the old secret row.
	_, secret2, err := repo.EnrollAs(ctx, "bob")
	require.NoError(t, err)
	assert.NotEqual(t, secret, secret2)
	code2, err := totp.GenerateCode(secret2, time.Now())
	require.NoError(t, err)
	ok, err = repo.Verify(ctx, code2)
	require.NoError(t, err)
	assert.True(t, ok)

	// Unconditional admin-reset path.
	require.NoError(t, repo.Disable(ctx))
	enabled, err := repo.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestUserStore_IsolationBetweenUsers(t *testing.T) {
	repoA, _ := newUserTestRepo(t, userStoreMasterKey)
	repoB, _ := newUserTestRepo(t, userStoreMasterKey)
	ctx := context.Background()

	_, secret, err := repoA.EnrollAs(ctx, "alice")
	require.NoError(t, err)
	codesA, err := repoA.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	require.NoError(t, repoA.Enable(ctx))

	// B sees none of A's state.
	enabled, err := repoB.IsEnabled(ctx)
	require.NoError(t, err)
	assert.False(t, enabled)
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	ok, err := repoB.Verify(ctx, code)
	require.NoError(t, err)
	assert.False(t, ok, "B has no enrollment, A's code must not verify")
	ok, err = repoB.ConsumeRecoveryCode(ctx, codesA[0])
	require.NoError(t, err)
	assert.False(t, ok, "A's recovery code must not consume for B")

	// B's reset is scoped to B.
	require.NoError(t, repoB.Disable(ctx))
	enabled, err = repoA.IsEnabled(ctx)
	require.NoError(t, err)
	assert.True(t, enabled)
}

func TestUserStore_AuthorizerErrorPropagates(t *testing.T) {
	repo, id := newUserTestRepo(t, userStoreMasterKey)
	ctx := context.Background()
	_, _, err := repo.EnrollAs(ctx, "erring")
	require.NoError(t, err)

	store := NewUserPostgresStore(testDB.Pool(), id)
	wantErr := assert.AnError
	_, err = store.DisableIfAuthorized(ctx, func(EncryptedSecret, *int64, func(string) (bool, error)) (bool, error) {
		return false, wantErr
	})
	require.ErrorIs(t, err, wantErr)
	// Nothing was deleted on the failed authorization.
	var rows int
	require.NoError(t, testDB.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM user_totp WHERE user_id = $1`, id).Scan(&rows))
	assert.Equal(t, 1, rows)
}

func TestUserStore_CascadeOnUserDelete(t *testing.T) {
	repo, id := newUserTestRepo(t, userStoreMasterKey)
	ctx := context.Background()

	_, _, err := repo.EnrollAs(ctx, "doomed")
	require.NoError(t, err)
	_, err = repo.GenerateRecoveryCodes(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.Enable(ctx))

	_, err = testDB.Pool().Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	require.NoError(t, err)

	var totpRows, recoveryRows int
	require.NoError(t, testDB.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM user_totp WHERE user_id = $1`, id).Scan(&totpRows))
	require.NoError(t, testDB.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM user_totp_recovery WHERE user_id = $1`, id).Scan(&recoveryRows))
	assert.Zero(t, totpRows)
	assert.Zero(t, recoveryRows)
}

// TestUserStore_ZeroStateReadBranches covers the "no enrollment" read paths that
// return the zero value rather than an error: DisableIfAuthorized's ErrNoRows
// arm, Info's LastUsedStep ErrNoRows arm, and RecoveryCounts on an empty set.
func TestUserStore_ZeroStateReadBranches(t *testing.T) {
	repo, id := newUserTestRepo(t, userStoreMasterKey)
	store := NewUserPostgresStore(testDB.Pool(), id)
	ctx := context.Background()

	// DisableWithCode on a never-enrolled user: the load hits ErrNoRows and
	// reports "not authorized" with no error, never touching the authorizer.
	ok, err := repo.DisableWithCode(ctx, "000000")
	require.NoError(t, err)
	assert.False(t, ok)

	// Info on a never-enrolled user: RecoveryCounts is (0,0) and LastUsedStep
	// hits ErrNoRows, so LastUsed stays the zero time.
	info, err := repo.Info(ctx)
	require.NoError(t, err)
	assert.Zero(t, info.RecoveryTotal)
	assert.True(t, info.LastUsed.IsZero())

	// EnabledAt on a row that is enabled but has a NULL confirmed_at (an
	// inconsistent state the normal Enable path never writes) reports ok=false.
	_, err = testDB.Pool().Exec(ctx,
		`INSERT INTO user_totp (user_id, secret_cipher, secret_nonce, secret_salt, enabled, confirmed_at)
		 VALUES ($1, 'c', 'n', 's', TRUE, NULL)`, id)
	require.NoError(t, err)
	at, has, err := store.EnabledAt(ctx)
	require.NoError(t, err)
	assert.False(t, has)
	assert.True(t, at.IsZero())
}

// TestUserStore_ReplaceRecoveryCodesInsertConflict covers the batched-INSERT
// error arm: two identical hashes collide on the (user_id, code_hash) PK, so
// the insert fails inside the transaction and the whole replace rolls back.
func TestUserStore_ReplaceRecoveryCodesInsertConflict(t *testing.T) {
	_, id := newUserTestRepo(t, userStoreMasterKey)
	store := NewUserPostgresStore(testDB.Pool(), id)
	ctx := context.Background()

	err := store.ReplaceRecoveryCodes(ctx, []string{"dup", "dup"})
	require.Error(t, err)

	// The failed replace left no rows behind (transaction rolled back).
	var n int
	require.NoError(t, testDB.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM user_totp_recovery WHERE user_id = $1`, id).Scan(&n))
	assert.Zero(t, n)
}

// TestUserPostgresStoreErrorsOnCanceledContext covers the DB-error branches of
// every UserPostgresStore method, mirroring the admin-store error test: a
// canceled context makes each query/transaction fail before touching a row.
func TestUserPostgresStoreErrorsOnCanceledContext(t *testing.T) {
	store := NewUserPostgresStore(testDB.Pool(), uuid.New())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := store.UpsertEnrollment(ctx, []byte("c"), []byte("n"), []byte("s")); err == nil {
		t.Error("UpsertEnrollment: want error")
	}
	if _, _, err := store.LoadSecret(ctx); err == nil {
		t.Error("LoadSecret: want error")
	}
	if _, err := store.RecordUsedStep(ctx, 1); err == nil {
		t.Error("RecordUsedStep: want error")
	}
	if _, err := store.Enable(ctx); err == nil {
		t.Error("Enable: want error")
	}
	if err := store.Disable(ctx); err == nil {
		t.Error("Disable: want error")
	}
	if _, err := store.DisableIfAuthorized(ctx, func(EncryptedSecret, *int64, func(string) (bool, error)) (bool, error) {
		return true, nil
	}); err == nil {
		t.Error("DisableIfAuthorized: want error")
	}
	if _, err := store.IsEnabled(ctx); err == nil {
		t.Error("IsEnabled: want error")
	}
	if _, _, err := store.EnabledAt(ctx); err == nil {
		t.Error("EnabledAt: want error")
	}
	if _, _, err := store.RecoveryCounts(ctx); err == nil {
		t.Error("RecoveryCounts: want error")
	}
	if _, _, err := store.LastUsedStep(ctx); err == nil {
		t.Error("LastUsedStep: want error")
	}
	if err := store.ReplaceRecoveryCodes(ctx, []string{"h"}); err == nil {
		t.Error("ReplaceRecoveryCodes: want error")
	}
	if _, err := store.ConsumeRecoveryCode(ctx, "h"); err == nil {
		t.Error("ConsumeRecoveryCode: want error")
	}
}
