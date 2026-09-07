// Package admin provides admin token authentication and management.
package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

const tokenLength = 32
const sha256Prefix = "sha256:"

// Manager handles admin token authentication and management.
//
// tokenHash and plainToken are set once in New before the Manager escapes and
// are never written again, so reads need no locking.
type Manager struct {
	dataDir    string
	tokenHash  string
	plainToken string
}

// New creates a new Manager. If initialToken is non-empty, it is used as the
// admin token on first boot (when no admin-token file exists) instead of
// generating a random one. If the admin-token file already exists, initialToken
// is ignored — the stored hash takes precedence.
func New(dataDir, initialToken string) (*Manager, bool, error) {
	m := &Manager{dataDir: dataDir}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("failed to create data directory: %w", err)
	}

	tokenHash, plainToken, isNew, err := m.loadOrCreateToken(initialToken)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load or create admin token: %w", err)
	}

	m.tokenHash = tokenHash
	m.plainToken = plainToken

	if isNew {
		debuglog.Info("admin: generated new admin token", "data_dir", dataDir)
	}

	return m, isNew, nil
}

// Token returns the plain admin token.
func (m *Manager) Token() string {
	return m.plainToken
}

// Validate checks if the provided token matches the stored admin token hash.
func (m *Manager) Validate(token string) bool {
	if token == "" {
		return false
	}
	// tokenHash is always stored without the sha256: prefix (see loadOrCreateToken)
	return subtle.ConstantTimeCompare([]byte(util.SHA256Hex(token)), []byte(m.tokenHash)) == 1
}

// writeTokenFileAtomic writes the admin-token file via a temp file + fsync +
// rename so a crash mid-write can never leave a truncated or empty file. An
// empty admin-token file makes loadOrCreateToken regenerate a brand-new token on
// the next boot, silently rotating the member out of its group.
func writeTokenFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	//nolint:gosec // path is built from dataDir, not user input; 0600 secret file
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// isSHA256Hex reports whether s is a SHA-256 digest in the form this package
// stores: exactly 64 hex characters, decoding to 32 bytes.
func isSHA256Hex(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == sha256.Size
}

func (m *Manager) loadOrCreateToken(initialToken string) (tokenHash, plainToken string, isNew bool, err error) {
	tokenPath := filepath.Join(m.dataDir, "admin-token")

	//nolint:gosec // tokenPath is constructed from dataDir constant
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.createAndSaveToken(tokenPath, initialToken)
		}
		debuglog.Error("admin: failed to read token file", "path", tokenPath, "error", err)
		return "", "", false, fmt.Errorf("failed to read token file: %w", err)
	}

	content := string(data)
	if content == "" {
		return m.createAndSaveToken(tokenPath, initialToken)
	}

	// sha256: prefix format
	if stored, ok := strings.CutPrefix(content, sha256Prefix); ok {
		if !isSHA256Hex(stored) {
			return "", "", false, fmt.Errorf("token file %s holds a malformed %s hash: expected 64 hex characters, got %d characters", tokenPath, sha256Prefix, len(stored))
		}
		return stored, "", false, nil
	}

	// Legacy: bare 64-char hex hash (no prefix). Not migrated to sha256:
	// prefix to avoid rewriting a file that already stores a valid hash. A
	// 64-character value that is not hex cannot be a digest, so it is a
	// corrupt file rather than a plaintext token to migrate: no caller could
	// ever authenticate against it, and silently accepting it would lock the
	// dashboard out with no diagnosis.
	if len(content) == 64 {
		if !isSHA256Hex(content) {
			return "", "", false, fmt.Errorf("token file %s holds a malformed legacy hash: 64 characters that are not hex", tokenPath)
		}
		return content, "", false, nil
	}

	// Plaintext: hash and rewrite with sha256: prefix
	hashHex := util.SHA256Hex(content)
	prefixed := sha256Prefix + hashHex
	debuglog.Warn("admin: migrating plaintext token to hashed format")
	if err := writeTokenFileAtomic(tokenPath, []byte(prefixed)); err != nil {
		return "", "", false, fmt.Errorf("failed to migrate token file: %w", err)
	}

	return hashHex, "", false, nil
}

func (m *Manager) createAndSaveToken(tokenPath, initialToken string) (tokenHash, plainToken string, isNew bool, err error) {
	var plain string
	if initialToken != "" {
		plain = initialToken
	} else {
		generated, err := m.generateToken()
		if err != nil {
			return "", "", false, fmt.Errorf("failed to generate token: %w", err)
		}
		plain = generated
	}

	hashHex := util.SHA256Hex(plain)
	prefixed := sha256Prefix + hashHex

	if err := writeTokenFileAtomic(tokenPath, []byte(prefixed)); err != nil {
		debuglog.Error("admin: failed to write token file", "path", tokenPath, "error", err)
		return "", "", false, fmt.Errorf("failed to write token file: %w", err)
	}

	return hashHex, plain, true, nil
}

func (m *Manager) generateToken() (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate UUID: %w", err)
	}

	hash := sha256.Sum256(u[:])
	token := hex.EncodeToString(hash[:])[:tokenLength]

	return token, nil
}
