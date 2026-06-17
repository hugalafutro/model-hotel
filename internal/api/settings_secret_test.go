package api

import (
	"testing"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
)

const secretTestMasterKey = "api-secret-test-master-key-32bytesmin!!"

func TestEncryptSecretSettings(t *testing.T) {
	h := &Handler{cfg: &config.Config{MasterKey: secretTestMasterKey}}

	t.Run("encrypts a new plaintext value", func(t *testing.T) {
		req := map[string]string{"alert_apprise_targets": "tgram://tok/chat"}
		if err := h.encryptSecretSettings(req); err != nil {
			t.Fatalf("encryptSecretSettings: %v", err)
		}
		got := req["alert_apprise_targets"]
		if !auth.IsEncryptedString(got) {
			t.Fatalf("value not encrypted: %q", got)
		}
		dec, err := auth.DecryptString(got, secretTestMasterKey)
		if err != nil || dec != "tgram://tok/chat" {
			t.Errorf("round trip failed: dec=%q err=%v", dec, err)
		}
	})

	t.Run("masked value is dropped to preserve stored ciphertext", func(t *testing.T) {
		req := map[string]string{"alert_apprise_targets": secretMaskValue, "alert_enabled": "true"}
		if err := h.encryptSecretSettings(req); err != nil {
			t.Fatalf("encryptSecretSettings: %v", err)
		}
		if _, ok := req["alert_apprise_targets"]; ok {
			t.Error("masked secret should be removed from the write set")
		}
		if req["alert_enabled"] != "true" {
			t.Error("non-secret key must be untouched")
		}
	})

	t.Run("empty value clears the secret", func(t *testing.T) {
		req := map[string]string{"alert_apprise_targets": ""}
		if err := h.encryptSecretSettings(req); err != nil {
			t.Fatalf("encryptSecretSettings: %v", err)
		}
		if v, ok := req["alert_apprise_targets"]; !ok || v != "" {
			t.Errorf("empty secret should remain empty, got ok=%v v=%q", ok, v)
		}
	})

	t.Run("non-secret keys are untouched", func(t *testing.T) {
		req := map[string]string{"circuit_breaker_threshold": "5"}
		if err := h.encryptSecretSettings(req); err != nil {
			t.Fatalf("encryptSecretSettings: %v", err)
		}
		if req["circuit_breaker_threshold"] != "5" {
			t.Error("non-secret value mutated")
		}
	})
}

func TestEncryptSecretSettingsRequiresMasterKey(t *testing.T) {
	h := &Handler{cfg: &config.Config{MasterKey: ""}}
	req := map[string]string{"alert_apprise_targets": "tgram://tok"}
	if err := h.encryptSecretSettings(req); err == nil {
		t.Error("expected error when MASTER_KEY is unset and a new secret is supplied")
	}
}

func TestInjectReadOnlyStatusMasksSecrets(t *testing.T) {
	h := &Handler{appVersion: "test"}

	masked := h.injectReadOnlyStatus(map[string]string{"alert_apprise_targets": "enc:v1:abc:def:ghi"})
	if masked["alert_apprise_targets"] != secretMaskValue {
		t.Errorf("configured secret not masked: %q", masked["alert_apprise_targets"])
	}

	empty := h.injectReadOnlyStatus(map[string]string{"alert_apprise_targets": ""})
	if empty["alert_apprise_targets"] != "" {
		t.Errorf("unset secret should stay empty, got %q", empty["alert_apprise_targets"])
	}
}
