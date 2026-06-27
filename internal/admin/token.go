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
	"sync"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

const tokenLength = 32
const sha256Prefix = "sha256:"

// Manager handles admin token authentication and management.
//
// tokenHash and plainToken are guarded by mu for safe concurrent reads of the
// stored hash and the one-boot plaintext.
type Manager struct {
	mu         sync.RWMutex
	dataDir    string
	tokenHash  string
	plainToken string
	isNew      bool
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
	m.isNew = isNew

	if isNew {
		debuglog.Info("admin: generated new admin token", "data_dir", dataDir)
	}

	return m, isNew, nil
}

// Token returns the plain admin token.
func (m *Manager) Token() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.plainToken
}

// IsNew reports whether a new admin token was generated on this boot.
func (m *Manager) IsNew() bool {
	return m.isNew
}

// Validate checks if the provided token matches the stored admin token hash.
func (m *Manager) Validate(token string) bool {
	if token == "" {
		return false
	}
	m.mu.RLock()
	stored := m.tokenHash
	m.mu.RUnlock()
	if stored == "" {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])

	// tokenHash is always stored without the sha256: prefix (see loadOrCreateToken)
	return subtle.ConstantTimeCompare([]byte(hashHex), []byte(stored)) == 1
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
	if strings.HasPrefix(content, sha256Prefix) {
		return content[len(sha256Prefix):], "", false, nil
	}

	// Legacy: bare 64-char hex hash (no prefix). Not migrated to sha256:
	// prefix to avoid rewriting a file that already stores a valid hash.
	if len(content) == 64 {
		return content, "", false, nil
	}

	// Plaintext: hash and rewrite with sha256: prefix
	hash := sha256.Sum256([]byte(content))
	hashHex := hex.EncodeToString(hash[:])
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

	hash := sha256.Sum256([]byte(plain))
	hashHex := hex.EncodeToString(hash[:])
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
