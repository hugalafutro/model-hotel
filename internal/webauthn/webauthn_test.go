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

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"))
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

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"))
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

	token, err := NewSessionManager(repo).CreateAuthToken(ctx, []byte("admin"))
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
