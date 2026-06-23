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
// tokenHash and plainToken are guarded by mu so the hash can be hot-reloaded at
// runtime (SetHash) without restarting the process: the HA Front Desk control
// plane pushes a new admin-token hash and the member applies it live.
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

// Hash returns the current admin token hash in sha256:<hex> form, or an empty
// string when no token is set. Used by the HA token-hash sync endpoint so the
// Front Desk control plane can compare members before converging them.
func (m *Manager) Hash() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.tokenHash == "" {
		return ""
	}
	return sha256Prefix + m.tokenHash
}

// SetHash overwrites the stored admin token hash and persists it to the
// admin-token file, taking effect immediately (no restart). It accepts either a
// sha256:<64-hex> value or a bare 64-char hex hash. The plaintext token (if any
// from this boot) is cleared, since it no longer matches. The file write
// happens before the in-memory swap so a failed write leaves the live token
// unchanged.
func (m *Manager) SetHash(value string) error {
	hashHex, err := normalizeTokenHash(value)
	if err != nil {
		return err
	}

	tokenPath := filepath.Join(m.dataDir, "admin-token")
	//nolint:gosec // tokenPath is constructed from dataDir constant, not user input
	if err := os.WriteFile(tokenPath, []byte(sha256Prefix+hashHex), 0o600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	m.mu.Lock()
	m.tokenHash = hashHex
	m.plainToken = ""
	m.mu.Unlock()

	debuglog.Info("admin: admin token hash updated")
	return nil
}

// normalizeTokenHash validates a sha256:<hex> or bare 64-hex value and returns
// the lowercased bare hex hash.
func normalizeTokenHash(value string) (string, error) {
	v := strings.TrimSpace(value)
	v = strings.TrimPrefix(v, sha256Prefix)
	v = strings.ToLower(v)
	if len(v) != 64 {
		return "", fmt.Errorf("admin: token hash must be a 64-character hex SHA-256 digest")
	}
	if _, err := hex.DecodeString(v); err != nil {
		return "", fmt.Errorf("admin: token hash is not valid hex: %w", err)
	}
	return v, nil
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
	//nolint:gosec // tokenPath is constructed from dataDir constant, not user input
	if err := os.WriteFile(tokenPath, []byte(prefixed), 0o600); err != nil {
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

	if err := os.WriteFile(tokenPath, []byte(prefixed), 0o600); err != nil {
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
