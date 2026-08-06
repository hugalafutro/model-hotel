package user

import (
	"context"
	"errors"
	"testing"
)

// TestValidateHashFormat_AcceptsRealHashes pins the validator to what
// HashPassword actually produces: a validator that rejected genuine hashes
// would lock every synced account out of logging in.
func TestValidateHashFormat_AcceptsRealHashes(t *testing.T) {
	hash, err := HashPassword(context.Background(), "correct-horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := ValidateHashFormat(hash); err != nil {
		t.Errorf("ValidateHashFormat rejected a hash from HashPassword: %v", err)
	}
}

func TestValidateHashFormat_RejectsMalformed(t *testing.T) {
	cases := []struct {
		name string
		hash string
	}{
		{"empty", ""},
		{"plaintext password", "hunter2"},
		{"bcrypt", "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"},
		{"wrong algorithm", "$argon2i$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ"},
		{"truncated", "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ"},
		{"missing version", "$argon2id$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ"},
		{"zero parameters", "$argon2id$v=19$m=0,t=0,p=0$c29tZXNhbHQ$c29tZWtleQ"},
		{"absurd memory cost", "$argon2id$v=19$m=4194304,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ"},
		{"non-base64 salt", "$argon2id$v=19$m=19456,t=2,p=1$not!base64$c29tZWtleQ"},
		{"empty key", "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateHashFormat(tc.hash); !errors.Is(err, ErrHashFormat) {
				t.Errorf("ValidateHashFormat(%q) err = %v, want ErrHashFormat", tc.hash, err)
			}
		})
	}
}

// TestValidateHashFormat_AgreesWithVerifyPassword keeps the two entry points on
// one parser: a hash the validator accepts must never be one login rejects as
// malformed, or a synced account would import cleanly and then fail to log in.
func TestValidateHashFormat_AgreesWithVerifyPassword(t *testing.T) {
	hash, err := HashPassword(context.Background(), "correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{hash, "", "hunter2", "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ"} {
		validErr := ValidateHashFormat(candidate)
		_, verifyErr := VerifyPassword(context.Background(), "correct-horse", candidate)
		if errors.Is(validErr, ErrHashFormat) != errors.Is(verifyErr, ErrHashFormat) {
			t.Errorf("%q: validator says %v but VerifyPassword says %v", candidate, validErr, verifyErr)
		}
	}
}
