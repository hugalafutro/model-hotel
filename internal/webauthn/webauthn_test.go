package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	gowa "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("webauthn")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("webauthn")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1)
	}
	defer testDB.Close()

	os.Exit(m.Run())
}

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	return NewRepository(testDB.Pool())
}

func TestStoreAndListCredentials(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cred := &CredentialRecord{
		ID:                []byte("test-cred-id-1"),
		PublicKey:         []byte("fake-public-key"),
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{"internal"},
		FlagsByte:         0x41,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	creds, err := repo.ListCredentials(ctx)
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}

	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}

	if string(creds[0].ID) != "test-cred-id-1" {
		t.Errorf("expected credential ID 'test-cred-id-1', got %q", string(creds[0].ID))
	}
	if creds[0].AttestationType != "none" {
		t.Errorf("expected attestation type 'none', got %q", creds[0].AttestationType)
	}
	if len(creds[0].Transport) != 1 || creds[0].Transport[0] != "internal" {
		t.Errorf("expected transport ['internal'], got %v", creds[0].Transport)
	}
}

func TestGetCredentialByID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cred := &CredentialRecord{
		ID:                []byte("test-cred-id-2"),
		PublicKey:         []byte("fake-public-key-2"),
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{"usb"},
		FlagsByte:         0x01,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	found, err := repo.GetCredentialByID(ctx, []byte("test-cred-id-2"))
	if err != nil {
		t.Fatalf("GetCredentialByID: %v", err)
	}
	if string(found.ID) != "test-cred-id-2" {
		t.Errorf("expected ID 'test-cred-id-2', got %q", string(found.ID))
	}

	_, err = repo.GetCredentialByID(ctx, []byte("nonexistent-id"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteCredential(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cred := &CredentialRecord{
		ID:                []byte("test-cred-id-3"),
		PublicKey:         []byte("fake-public-key-3"),
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{},
		FlagsByte:         0x01,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	if err := repo.DeleteCredential(ctx, []byte("test-cred-id-3")); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	if err := repo.DeleteCredential(ctx, []byte("test-cred-id-3")); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestUpdateSignCount(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cred := &CredentialRecord{
		ID:                []byte("test-cred-id-4"),
		PublicKey:         []byte("fake-public-key-4"),
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{"internal"},
		FlagsByte:         0x41,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	if err := repo.UpdateSignCount(ctx, []byte("test-cred-id-4"), 42); err != nil {
		t.Fatalf("UpdateSignCount: %v", err)
	}

	found, err := repo.GetCredentialByID(ctx, []byte("test-cred-id-4"))
	if err != nil {
		t.Fatalf("GetCredentialByID: %v", err)
	}
	if found.SignCount != 42 {
		t.Errorf("expected sign count 42, got %d", found.SignCount)
	}

	if err := repo.UpdateSignCount(ctx, []byte("nonexistent-id"), 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestRenameCredential(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cred := &CredentialRecord{
		ID:                []byte("test-cred-id-rename"),
		PublicKey:         []byte("fake-public-key-rename"),
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{"internal"},
		FlagsByte:         0x41,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	if err := repo.RenameCredential(ctx, []byte("test-cred-id-rename"), "My YubiKey"); err != nil {
		t.Fatalf("RenameCredential: %v", err)
	}

	found, err := repo.GetCredentialByID(ctx, []byte("test-cred-id-rename"))
	if err != nil {
		t.Fatalf("GetCredentialByID: %v", err)
	}
	if found.Name != "My YubiKey" {
		t.Errorf("expected name 'My YubiKey', got %q", found.Name)
	}

	if err := repo.RenameCredential(ctx, []byte("nonexistent-id"), "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateAndGetSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	sessionID := uuid.New()
	session := &SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	found, err := repo.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if found.Challenge != "test-challenge" {
		t.Errorf("expected challenge 'test-challenge', got %q", found.Challenge)
	}
	if found.Type != "registration" {
		t.Errorf("expected type 'registration', got %q", found.Type)
	}

	_, err = repo.GetSession(ctx, uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	sessionID := uuid.New()
	session := &SessionRecord{
		ID:          sessionID,
		Challenge:   "test-challenge-2",
		SessionData: []byte(`{"type":"login"}`),
		Type:        "login",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := repo.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if err := repo.DeleteSession(ctx, sessionID); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound on second delete, got %v", err)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	expiredID := uuid.New()
	expiredSession := &SessionRecord{
		ID:          expiredID,
		Challenge:   "expired-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	if err := repo.CreateSession(ctx, expiredSession); err != nil {
		t.Fatalf("CreateSession (expired): %v", err)
	}

	validID := uuid.New()
	validSession := &SessionRecord{
		ID:          validID,
		Challenge:   "valid-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}
	if err := repo.CreateSession(ctx, validSession); err != nil {
		t.Fatalf("CreateSession (valid): %v", err)
	}

	n, err := repo.CleanupExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 expired session cleaned, got %d", n)
	}

	_, err = repo.GetSession(ctx, expiredID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected expired session to be deleted, got err=%v", err)
	}

	_, err = repo.GetSession(ctx, validID)
	if err != nil {
		t.Errorf("expected valid session to remain, got err=%v", err)
	}
}

func TestSessionManagerValidate(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	if !mgr.Validate(ctx, token) {
		t.Error("expected valid token to pass Validate")
	}

	if mgr.Validate(ctx, "") {
		t.Error("expected empty string to fail Validate")
	}

	if mgr.Validate(ctx, "not-a-valid-token") {
		t.Error("expected random string to fail Validate")
	}

	if mgr.Validate(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Error("expected non-existent hex token to fail Validate")
	}
}

func TestSessionManagerExpiredToken(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	expiredID := uuid.New()
	expiredSession := &SessionRecord{
		ID:          expiredID,
		Challenge:   "expired-auth-challenge",
		SessionData: []byte(`{"type":"auth_token"}`),
		Type:        "auth_token",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(-1 * time.Hour),
	}
	if err := repo.CreateSession(ctx, expiredSession); err != nil {
		t.Fatalf("CreateSession (expired): %v", err)
	}

	mgr := NewSessionManager(repo)

	if mgr.Validate(ctx, expiredID.String()) {
		t.Error("expected expired token to fail Validate")
	}
}

func TestSessionManagerRevokeAuthToken(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	if !mgr.Validate(ctx, token) {
		t.Fatal("expected token to be valid before revocation")
	}

	if !mgr.RevokeAuthToken(ctx, token) {
		t.Error("expected RevokeAuthToken to return true for valid token")
	}

	if mgr.Validate(ctx, token) {
		t.Error("expected token to be invalid after revocation")
	}

	if mgr.RevokeAuthToken(ctx, token) {
		t.Error("expected RevokeAuthToken to return false for already-revoked token")
	}
}

func TestSessionManagerGetSessionByTokenHash(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	token, err := NewSessionManager(repo).CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	session, err := repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if session.Type != "auth_token" {
		t.Errorf("expected type 'auth_token', got %q", session.Type)
	}
	if session.TokenHash == nil || *session.TokenHash != tokenHash {
		t.Errorf("expected token_hash %q, got %v", tokenHash, session.TokenHash)
	}

	_, err = repo.GetSessionByTokenHash(ctx, "nonexistent-hash")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAdminUserInterface(t *testing.T) {
	u := NewAdminUser()

	if string(u.WebAuthnID()) != "admin" {
		t.Errorf("expected WebAuthnID 'admin', got %q", string(u.WebAuthnID()))
	}
	if u.WebAuthnName() != "admin" {
		t.Errorf("expected WebAuthnName 'admin', got %q", u.WebAuthnName())
	}
	if u.WebAuthnDisplayName() != "Administrator" {
		t.Errorf("expected WebAuthnDisplayName 'Administrator', got %q", u.WebAuthnDisplayName())
	}
	if len(u.WebAuthnCredentials()) != 0 {
		t.Errorf("expected 0 credentials initially, got %d", len(u.WebAuthnCredentials()))
	}
}

func TestCredentialConversion(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	cred := &CredentialRecord{
		ID:                        []byte("conversion-test-id"),
		PublicKey:                 []byte("fake-pub-key"),
		AttestationType:           "none",
		AttestationFormat:         "packed",
		Transport:                 []string{"internal", "hybrid"},
		FlagsByte:                 0x41,
		SignCount:                 5,
		AAGUID:                    uuid.Nil,
		AttestationObject:         []byte("att-obj"),
		AttestationClientData:     []byte("client-data"),
		AttestationClientDataHash: []byte("client-hash"),
		AttestationPublicKeyAlgo:  -7,
		AuthenticatorData:         []byte("auth-data"),
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	found, err := repo.GetCredentialByID(ctx, []byte("conversion-test-id"))
	if err != nil {
		t.Fatalf("GetCredentialByID: %v", err)
	}

	webauthnCred := found.ToWebAuthnCredential()
	if len(webauthnCred.Transport) != 2 {
		t.Errorf("expected 2 transports, got %d", len(webauthnCred.Transport))
	}

	roundTrip := FromWebAuthnCredential(&webauthnCred)
	if string(roundTrip.ID) != "conversion-test-id" {
		t.Errorf("round-trip ID mismatch: expected 'conversion-test-id', got %q", string(roundTrip.ID))
	}
}

// TestCreateAuthToken_WithCredentialID verifies that CreateAuthToken properly stores the credential_id
func TestCreateAuthToken_WithCredentialID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	// Create an auth token with a specific credential ID
	credentialID := []byte("test-credential-id-123")
	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), credentialID)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Look up the session to verify credential_id is stored
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	session, err := repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}

	if session.CredentialID == nil {
		t.Fatal("expected credential_id to be set")
	}
	if string(session.CredentialID) != "test-credential-id-123" {
		t.Errorf("expected credential_id 'test-credential-id-123', got %q", string(session.CredentialID))
	}
}

// TestDeleteCredential_CascadeDelete verifies that deleting a credential cascades to auth_token sessions
func TestDeleteCredential_CascadeDelete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	// First, create a credential
	credID := []byte("cascade-test-cred-id")
	cred := &CredentialRecord{
		ID:                credID,
		PublicKey:         []byte("fake-public-key"),
		AttestationType:   "none",
		AttestationFormat: "packed",
		Transport:         []string{"internal"},
		FlagsByte:         0x41,
		SignCount:         0,
		AAGUID:            uuid.Nil,
	}

	if err := repo.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	// Create an auth token session with that credential's ID
	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), credID)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	// Verify the session exists
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	session, err := repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash (before delete): %v", err)
	}
	if session.CredentialID == nil {
		t.Fatal("expected credential_id to be set")
	}

	// Delete the credential
	if err := repo.DeleteCredential(ctx, credID); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}

	// Verify the auth_token session is also deleted
	_, err = repo.GetSessionByTokenHash(ctx, tokenHash)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after cascade delete, got %v", err)
	}
}

// TestValidate_WithCredentialID verifies Validate works for tokens with credential_id sessions
func TestValidate_WithCredentialID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	// Create an auth token with a credential ID
	credentialID := []byte("validate-test-cred-id")
	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), credentialID)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	// Verify the token validates successfully
	if !mgr.Validate(ctx, token) {
		t.Error("expected token with credential_id to validate successfully")
	}

	// Clean up
	mgr.RevokeAuthToken(ctx, token)
}

// TestSessionManagerValidate_WrongSessionType verifies that Validate returns false
// for sessions whose Type is not "auth_token". The session is stored with a known
// TokenHash so that GetSessionByTokenHash succeeds and the type-check branch
// (session.Type != "auth_token") is actually exercised.
func TestSessionManagerValidate_WrongSessionType(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Use a known token so we can set its hash on the session record.
	testToken := "test-registration-token-abc"
	hash := sha256.Sum256([]byte(testToken))
	tokenHash := hex.EncodeToString(hash[:])

	sessionID := uuid.New()
	session := &SessionRecord{
		ID:          sessionID,
		Challenge:   "wrong-type-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
		UserID:      []byte("admin"),
		TokenHash:   &tokenHash,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteSession(ctx, sessionID) })

	mgr := NewSessionManager(repo)

	// Validate hashes the token, looks up by token_hash (finds the session),
	// then checks session.Type != "auth_token" and returns false.
	if mgr.Validate(ctx, testToken) {
		t.Error("expected non-auth_token session to fail Validate")
	}
}

// TestSessionManagerRevokeAuthToken_EmptyToken verifies that RevokeAuthToken
// returns false immediately for an empty string token.
func TestSessionManagerRevokeAuthToken_EmptyToken(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	if mgr.RevokeAuthToken(context.Background(), "") {
		t.Error("expected RevokeAuthToken to return false for empty token")
	}
}

// TestGenerateChallenge verifies that generateChallenge produces correct-length
// hex strings and is non-deterministic across calls.
func TestGenerateChallenge(t *testing.T) {
	ch1, err := generateChallenge(32)
	if err != nil {
		t.Fatalf("generateChallenge(32): %v", err)
	}
	if len(ch1) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("expected 64-char hex string, got %d chars", len(ch1))
	}

	// Zero-length should return empty string
	ch0, err := generateChallenge(0)
	if err != nil {
		t.Fatalf("generateChallenge(0): %v", err)
	}
	if ch0 != "" {
		t.Errorf("expected empty string for length 0, got %q", ch0)
	}

	// Two calls with the same length should produce different values (randomness)
	ch2, err := generateChallenge(32)
	if err != nil {
		t.Fatalf("generateChallenge(32) second call: %v", err)
	}
	if ch1 == ch2 {
		t.Error("expected different challenge values from consecutive calls")
	}
}

// TestSessionManagerRevokeAuthToken_NonAuthToken verifies that RevokeAuthToken
// returns false when no auth_token session matches the token. A registration-type
// session with the same token hash exists in the DB, but GetSessionByTokenHash
// filters by type='auth_token', so the lookup returns ErrNotFound.
func TestSessionManagerRevokeAuthToken_NonAuthToken(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create a registration session with a known token hash
	testToken := "revoke-non-auth-token"
	hash := sha256.Sum256([]byte(testToken))
	tokenHash := hex.EncodeToString(hash[:])

	sessionID := uuid.New()
	session := &SessionRecord{
		ID:          sessionID,
		Challenge:   "revoke-non-auth-challenge",
		SessionData: []byte(`{"type":"registration"}`),
		Type:        "registration",
		UserID:      []byte("admin"),
		TokenHash:   &tokenHash,
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteSession(ctx, sessionID) })

	mgr := NewSessionManager(repo)

	// RevokeAuthToken looks up by token_hash WHERE type='auth_token',
	// so the registration session is invisible to this query.
	if mgr.RevokeAuthToken(ctx, testToken) {
		t.Error("expected RevokeAuthToken to return false when no auth_token session matches")
	}
}

// TestSessionManagerValidate_ExpiredTokenWithHash verifies that Validate returns
// false for expired auth_token sessions that have a proper token hash.
func TestSessionManagerValidate_ExpiredTokenWithHash(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	testToken := "expired-auth-token-with-hash"
	hash := sha256.Sum256([]byte(testToken))
	tokenHash := hex.EncodeToString(hash[:])

	sessionID := uuid.New()
	session := &SessionRecord{
		ID:          sessionID,
		Challenge:   "expired-auth-challenge-2",
		SessionData: []byte(`{"type":"auth_token"}`),
		Type:        "auth_token",
		UserID:      []byte("admin"),
		TokenHash:   &tokenHash,
		ExpiresAt:   time.Now().Add(-1 * time.Hour), // expired
	}

	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Cleanup(func() { _ = repo.DeleteSession(ctx, sessionID) })

	mgr := NewSessionManager(repo)

	if mgr.Validate(ctx, testToken) {
		t.Error("expected expired auth_token session to fail Validate")
	}
}

// TestSessionManagerCreateAuthToken_DifferentUserIDs verifies that CreateAuthToken
// correctly stores the provided userID in the session record.
func TestSessionManagerCreateAuthToken_DifferentUserIDs(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	// Create token with a custom user ID
	customUserID := []byte("custom-user-123")
	token, err := mgr.CreateAuthToken(ctx, customUserID, nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	// Verify the session has the correct user ID
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	session, err := repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	if string(session.UserID) != "custom-user-123" {
		t.Errorf("expected UserID 'custom-user-123', got %q", string(session.UserID))
	}
}

// TestSessionManagerCreateAuthToken_CanceledContext verifies that CreateAuthToken
// returns an error when the context is already canceled.
func TestSessionManagerCreateAuthToken_CanceledContext(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

// TestSessionManagerRevokeAuthToken_CanceledContext verifies that RevokeAuthToken
// returns false when the context is already canceled.
func TestSessionManagerRevokeAuthToken_CanceledContext(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	if mgr.RevokeAuthToken(canceledCtx, token) {
		t.Error("expected RevokeAuthToken to return false for canceled context")
	}

	// Token should still be valid since revocation failed
	if !mgr.Validate(ctx, token) {
		t.Error("expected token to still be valid after failed revocation")
	}
}

// TestGenerateChallenge_OutputLength verifies generateChallenge returns
// correctly sized hex-encoded output.
func TestGenerateChallenge_OutputLength(t *testing.T) {
	for _, size := range []int{1, 16, 32, 64} {
		result, err := generateChallenge(size)
		if err != nil {
			t.Errorf("generateChallenge(%d): %v", size, err)
		}
		if len(result) != size*2 {
			t.Errorf("generateChallenge(%d): got length %d, want %d", size, len(result), size*2)
		}
	}

	result, err := generateChallenge(0)
	if err != nil {
		t.Errorf("generateChallenge(0): %v", err)
	}
	if result != "" {
		t.Errorf("generateChallenge(0): got %q, want empty string", result)
	}
}

// ---------------------------------------------------------------------------
// AdminUser.SetCredentials
// ---------------------------------------------------------------------------

// TestSetCredentials verifies that SetCredentials updates the credentials
// returned by WebAuthnCredentials.
func TestSetCredentials(t *testing.T) {
	u := NewAdminUser()

	if len(u.WebAuthnCredentials()) != 0 {
		t.Fatalf("expected 0 credentials initially, got %d", len(u.WebAuthnCredentials()))
	}

	creds := []gowa.Credential{
		{ID: []byte("cred-1")},
		{ID: []byte("cred-2")},
	}
	u.SetCredentials(creds)

	got := u.WebAuthnCredentials()
	if len(got) != 2 {
		t.Fatalf("expected 2 credentials after SetCredentials, got %d", len(got))
	}
	if string(got[0].ID) != "cred-1" {
		t.Errorf("expected first credential ID 'cred-1', got %q", string(got[0].ID))
	}
	if string(got[1].ID) != "cred-2" {
		t.Errorf("expected second credential ID 'cred-2', got %q", string(got[1].ID))
	}

	// SetCredentials replaces, not appends
	u.SetCredentials([]gowa.Credential{{ID: []byte("cred-3")}})
	got = u.WebAuthnCredentials()
	if len(got) != 1 {
		t.Fatalf("expected 1 credential after second SetCredentials, got %d", len(got))
	}
	if string(got[0].ID) != "cred-3" {
		t.Errorf("expected credential ID 'cred-3', got %q", string(got[0].ID))
	}

	// Setting nil clears credentials
	u.SetCredentials(nil)
	if len(u.WebAuthnCredentials()) != 0 {
		t.Errorf("expected 0 credentials after SetCredentials(nil), got %d", len(u.WebAuthnCredentials()))
	}
}

// ---------------------------------------------------------------------------
// notFoundError.Error
// ---------------------------------------------------------------------------

// TestErrNotFound_Error verifies the sentinel error message.
func TestErrNotFound_Error(t *testing.T) {
	if ErrNotFound.Error() != "webauthn record not found" {
		t.Errorf("expected 'webauthn record not found', got %q", ErrNotFound.Error())
	}

	// ErrNotFound should satisfy errors.Is
	if !errors.Is(ErrNotFound, ErrNotFound) {
		t.Error("errors.Is(ErrNotFound, ErrNotFound) should be true")
	}
}

// ---------------------------------------------------------------------------
// NewRelyingParty
// ---------------------------------------------------------------------------

// TestNewRelyingParty_ValidConfig verifies that a valid config produces a
// non-nil WebAuthn instance.
func TestNewRelyingParty_ValidConfig(t *testing.T) {
	w, err := NewRelyingParty("localhost", "Model Hotel", []string{"https://localhost:8081"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil WebAuthn instance")
	}
}

// TestNewRelyingParty_MultipleOrigins verifies that multiple origins
// are accepted without error.
func TestNewRelyingParty_MultipleOrigins(t *testing.T) {
	w, err := NewRelyingParty("example.com", "Test App", []string{"https://example.com", "https://app.example.com"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil WebAuthn instance")
	}
}

// ---------------------------------------------------------------------------
// ToWebAuthnCredential / FromWebAuthnCredential edge cases
// ---------------------------------------------------------------------------

// TestToWebAuthnCredential_EmptyTransport verifies that an empty transport
// slice does not panic and produces a credential with nil transports.
func TestToWebAuthnCredential_EmptyTransport(t *testing.T) {
	record := &CredentialRecord{
		ID:              []byte("empty-transport-id"),
		PublicKey:       []byte("pub-key"),
		AttestationType: "none",
		AAGUID:          uuid.Nil,
		Transport:       []string{},
		FlagsByte:       0x01,
	}

	cred := record.ToWebAuthnCredential()
	if string(cred.ID) != "empty-transport-id" {
		t.Errorf("expected ID 'empty-transport-id', got %q", string(cred.ID))
	}
	if len(cred.Transport) != 0 {
		t.Errorf("expected 0 transports, got %d", len(cred.Transport))
	}
}

// TestToWebAuthnCredential_NilTransport verifies that a nil transport slice
// produces a credential with nil transports.
func TestToWebAuthnCredential_NilTransport(t *testing.T) {
	record := &CredentialRecord{
		ID:              []byte("nil-transport-id"),
		PublicKey:       []byte("pub-key"),
		AttestationType: "none",
		AAGUID:          uuid.Nil,
		Transport:       nil,
		FlagsByte:       0x01,
	}

	cred := record.ToWebAuthnCredential()
	if len(cred.Transport) != 0 {
		t.Errorf("expected 0 transports for nil, got %d", len(cred.Transport))
	}
}

// TestToWebAuthnCredential_FullAttestation verifies that all attestation
// fields are properly propagated through the conversion.
func TestToWebAuthnCredential_FullAttestation(t *testing.T) {
	record := &CredentialRecord{
		ID:                        []byte("full-att-id"),
		PublicKey:                 []byte("pub-key"),
		AttestationType:           "packed",
		AttestationFormat:         "tpm",
		Transport:                 []string{"usb", "nfc"},
		FlagsByte:                 0x45,
		SignCount:                 10,
		AAGUID:                    uuid.Nil,
		AttestationObject:         []byte("att-obj-full"),
		AttestationClientData:     []byte("client-data-full"),
		AttestationClientDataHash: []byte("client-hash-full"),
		AttestationPublicKeyAlgo:  -257,
		AuthenticatorData:         []byte("auth-data-full"),
	}

	cred := record.ToWebAuthnCredential()
	if cred.AttestationType != "packed" {
		t.Errorf("expected attestation type 'packed', got %q", cred.AttestationType)
	}
	if cred.AttestationFormat != "tpm" {
		t.Errorf("expected attestation format 'tpm', got %q", cred.AttestationFormat)
	}
	if string(cred.Attestation.Object) != "att-obj-full" {
		t.Errorf("expected attestation object 'att-obj-full', got %q", string(cred.Attestation.Object))
	}
	if string(cred.Attestation.ClientDataJSON) != "client-data-full" {
		t.Errorf("expected client data 'client-data-full', got %q", string(cred.Attestation.ClientDataJSON))
	}
	if string(cred.Attestation.ClientDataHash) != "client-hash-full" {
		t.Errorf("expected client data hash 'client-hash-full', got %q", string(cred.Attestation.ClientDataHash))
	}
	if cred.Attestation.PublicKeyAlgorithm != -257 {
		t.Errorf("expected public key algo -257, got %d", cred.Attestation.PublicKeyAlgorithm)
	}
	if string(cred.Attestation.AuthenticatorData) != "auth-data-full" {
		t.Errorf("expected auth data 'auth-data-full', got %q", string(cred.Attestation.AuthenticatorData))
	}
	if len(cred.Transport) != 2 {
		t.Errorf("expected 2 transports, got %d", len(cred.Transport))
	}
	if cred.Authenticator.SignCount != 10 {
		t.Errorf("expected sign count 10, got %d", cred.Authenticator.SignCount)
	}
}

// TestFromWebAuthnCredential_InvalidAAGUID verifies that FromWebAuthnCredential
// gracefully handles an invalid AAGUID by falling back to uuid.Nil.
func TestFromWebAuthnCredential_InvalidAAGUID(t *testing.T) {
	cred := &gowa.Credential{
		ID:              []byte("invalid-aaguid-id"),
		PublicKey:       []byte("pub-key"),
		AttestationType: "none",
		Authenticator: gowa.Authenticator{
			AAGUID:    []byte{0xFF, 0xFF, 0xFF}, // too short for UUID
			SignCount: 0,
		},
	}

	record := FromWebAuthnCredential(cred)
	if record.AAGUID != uuid.Nil {
		t.Errorf("expected uuid.Nil for invalid AAGUID, got %q", record.AAGUID)
	}
	if string(record.ID) != "invalid-aaguid-id" {
		t.Errorf("expected ID 'invalid-aaguid-id', got %q", string(record.ID))
	}
}

// TestFromWebAuthnCredential_EmptyTransport verifies that empty transports
// are handled correctly.
func TestFromWebAuthnCredential_EmptyTransport(t *testing.T) {
	validAAGUID := make([]byte, 16)
	copy(validAAGUID, uuid.Nil[:])

	cred := &gowa.Credential{
		ID:              []byte("empty-transport-cred"),
		PublicKey:       []byte("pub-key"),
		AttestationType: "none",
		Transport:       nil,
		Authenticator: gowa.Authenticator{
			AAGUID:    validAAGUID,
			SignCount: 0,
		},
	}

	record := FromWebAuthnCredential(cred)
	if len(record.Transport) != 0 {
		t.Errorf("expected 0 transports, got %d", len(record.Transport))
	}
	if record.AAGUID != uuid.Nil {
		t.Errorf("expected uuid.Nil, got %q", record.AAGUID)
	}
}
