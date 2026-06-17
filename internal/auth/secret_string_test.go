package auth

import "testing"

const testMasterKey = "test-master-key-at-least-32-bytes-long!!"

func TestEncryptStringRoundTrip(t *testing.T) {
	plaintext := "tgram://123456:ABCDEF/789012"
	enc, err := EncryptString(plaintext, testMasterKey)
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	if !IsEncryptedString(enc) {
		t.Fatalf("encrypted value not tagged: %q", enc)
	}
	if enc == plaintext {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := DecryptString(enc, testMasterKey)
	if err != nil {
		t.Fatalf("DecryptString: %v", err)
	}
	if got != plaintext {
		t.Errorf("round trip = %q, want %q", got, plaintext)
	}
}

func TestDecryptStringPassThroughPlaintext(t *testing.T) {
	// A non-encrypted value (no prefix) returns unchanged.
	for _, s := range []string{"", "tgram://plain", "not encrypted"} {
		got, err := DecryptString(s, testMasterKey)
		if err != nil {
			t.Fatalf("DecryptString(%q): %v", s, err)
		}
		if got != s {
			t.Errorf("passthrough %q = %q", s, got)
		}
	}
}

func TestDecryptStringMalformed(t *testing.T) {
	if _, err := DecryptString(secretStringPrefix+"only:two", testMasterKey); err == nil {
		t.Error("expected error for wrong segment count")
	}
	// Invalid base64 in each of the three segments must error (ciphertext, nonce, salt).
	cases := []string{
		"!!!:QUJD:QUJD", // bad ciphertext
		"QUJD:!!!:QUJD", // bad nonce
		"QUJD:QUJD:!!!", // bad salt
	}
	for _, c := range cases {
		if _, err := DecryptString(secretStringPrefix+c, testMasterKey); err == nil {
			t.Errorf("expected error for malformed segment %q", c)
		}
	}
}

func TestDecryptStringWrongKey(t *testing.T) {
	enc, err := EncryptString("secret", testMasterKey)
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	if _, err := DecryptString(enc, "a-totally-different-master-key-32bytes!!"); err == nil {
		t.Error("expected decryption failure with wrong master key")
	}
}
