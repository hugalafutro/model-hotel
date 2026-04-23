package admin

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const tokenLength = 32

type Manager struct {
	dataDir    string
	tokenHash  string
	plainToken string
	isNew      bool
}

func New(dataDir string) (*Manager, bool, error) {
	m := &Manager{dataDir: dataDir}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, false, fmt.Errorf("failed to create data directory: %w", err)
	}

	tokenHash, plainToken, isNew, err := m.loadOrCreateToken()
	if err != nil {
		return nil, false, fmt.Errorf("failed to load or create admin token: %w", err)
	}

	m.tokenHash = tokenHash
	m.plainToken = plainToken
	m.isNew = isNew

	return m, isNew, nil
}

func (m *Manager) Token() string {
	return m.plainToken
}

func (m *Manager) IsNew() bool {
	return m.isNew
}

func (m *Manager) Validate(token string) bool {
	if token == "" || m.tokenHash == "" {
		return false
	}
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])
	return subtle.ConstantTimeCompare([]byte(hashHex), []byte(m.tokenHash)) == 1
}

func (m *Manager) loadOrCreateToken() (tokenHash string, plainToken string, isNew bool, err error) {
	tokenPath := filepath.Join(m.dataDir, "admin-token")

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.createAndSaveToken(tokenPath)
		}
		return "", "", false, fmt.Errorf("failed to read token file: %w", err)
	}

	content := string(data)
	if content == "" {
		return m.createAndSaveToken(tokenPath)
	}

	if len(content) == 64 {
		return content, "", false, nil
	}

	hash := sha256.Sum256([]byte(content))
	hashHex := hex.EncodeToString(hash[:])
	if err := os.WriteFile(tokenPath, []byte(hashHex), 0600); err != nil {
		return "", "", false, fmt.Errorf("failed to migrate token file: %w", err)
	}

	return hashHex, "", false, nil
}

func (m *Manager) createAndSaveToken(tokenPath string) (tokenHash string, plainToken string, isNew bool, err error) {
	plain, err := m.generateToken()
	if err != nil {
		return "", "", false, fmt.Errorf("failed to generate token: %w", err)
	}

	hash := sha256.Sum256([]byte(plain))
	hashHex := hex.EncodeToString(hash[:])

	if err := os.WriteFile(tokenPath, []byte(hashHex), 0600); err != nil {
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
