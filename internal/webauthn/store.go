package webauthn

import (
	"context"

	"github.com/google/uuid"
)

// This file defines the storage contracts for WebAuthn so the ceremony and
// session logic in this package is decoupled from any single database.
//
// *Repository (in webauthn.go) is the PostgreSQL implementation used by the
// main server. The HA "Front Desk" control plane supplies its own SQLite
// implementation of the same interfaces, letting it reuse SessionManager and
// the passkey/credential logic without a Postgres dependency. The method
// signatures here intentionally mirror *Repository exactly, so it satisfies
// them without any change to its behavior.

// CredentialStore persists WebAuthn credentials (passkeys).
type CredentialStore interface {
	StoreCredential(ctx context.Context, cred *CredentialRecord) error
	ListCredentials(ctx context.Context) ([]*CredentialRecord, error)
	GetCredentialByID(ctx context.Context, id []byte) (*CredentialRecord, error)
	DeleteCredential(ctx context.Context, id []byte) error
	RenameCredential(ctx context.Context, id []byte, name string) error
	UpdateSignCount(ctx context.Context, id []byte, signCount uint32) error
}

// SessionStore persists WebAuthn ceremony sessions and the long-lived
// auth_token sessions that back admin logins. SessionManager depends on this
// interface rather than the concrete Repository.
type SessionStore interface {
	CreateSession(ctx context.Context, session *SessionRecord) error
	GetSession(ctx context.Context, id uuid.UUID) (*SessionRecord, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*SessionRecord, error)
	DeleteSession(ctx context.Context, id uuid.UUID) error
	CleanupExpiredSessions(ctx context.Context) (int64, error)
}

// Store is the full WebAuthn persistence contract: credentials plus sessions.
type Store interface {
	CredentialStore
	SessionStore
}

// Compile-time assertion that the Postgres Repository satisfies the full Store.
var _ Store = (*Repository)(nil)
