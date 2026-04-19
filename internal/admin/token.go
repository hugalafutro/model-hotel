package admin

import (
	cryptoRand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const tokenLength = 32

type Manager struct {
	dataDir string
	token   string
}

func New(dataDir string) (*Manager, error) {
	m := &Manager{dataDir: dataDir}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	token, err := m.loadOrCreateToken()
	if err != nil {
		return nil, fmt.Errorf("failed to load or create admin token: %w", err)
	}

	m.token = token

	return m, nil
}

func (m *Manager) Token() string {
	return m.token
}

func (m *Manager) Validate(token string) bool {
	return m.token == token
}

func (m *Manager) loadOrCreateToken() (string, error) {
	tokenPath := filepath.Join(m.dataDir, "admin-token")

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return m.createAndSaveToken(tokenPath)
		}
		return "", fmt.Errorf("failed to read token file: %w", err)
	}

	token := string(data)
	if token == "" {
		return m.createAndSaveToken(tokenPath)
	}

	return token, nil
}

func (m *Manager) createAndSaveToken(tokenPath string) (string, error) {
	token, err := m.generateToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	if err := os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		return "", fmt.Errorf("failed to write token file: %w", err)
	}

	return token, nil
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

func HashProxyKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func GenerateProxyKey() (string, error) {
	randomBytes := make([]byte, 24)
	if _, err := cryptoRand.Read(randomBytes); err != nil {
		return "", err
	}
	return "llmp_" + base64.RawURLEncoding.EncodeToString(randomBytes), nil
}
