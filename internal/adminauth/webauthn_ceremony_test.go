package adminauth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	webauthnx "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// The relying party the ceremony tests register against.
const (
	softRPID   = "localhost"
	softOrigin = "http://localhost"
)

// softAuthenticator is a FIDO2 authenticator in software: one P-256 key behind
// one resident credential for the fixed admin user. It produces the bytes a
// browser hands the finish endpoints, so a test can carry a ceremony through
// the library's verification instead of stopping at the first rejected field.
type softAuthenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate authenticator key: %v", err)
	}
	return &softAuthenticator{key: key, credID: []byte("soft-" + uuid.NewString())}
}

// softAuthenticatorWithID is newSoftAuthenticator for a credential id the test
// chose, given base64url-encoded as the wire format carries it.
func softAuthenticatorWithID(t *testing.T, credentialIDB64 string) *softAuthenticator {
	t.Helper()
	id, err := base64.RawURLEncoding.DecodeString(credentialIDB64)
	if err != nil {
		t.Fatalf("decode credential id %q: %v", credentialIDB64, err)
	}
	a := newSoftAuthenticator(t)
	a.credID = id
	return a
}

// attestation returns the create() response for challenge: a "none"
// attestation carrying the credential's public key.
func (a *softAuthenticator) attestation(t *testing.T, challenge string) json.RawMessage {
	t.Helper()
	attObj, err := cbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": a.authData(t, 0x41, 0, true), // UP | AT
	})
	if err != nil {
		t.Fatalf("marshal attestation object: %v", err)
	}
	return a.credential(t, map[string]any{
		"attestationObject": base64.RawURLEncoding.EncodeToString(attObj),
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(a.clientData(t, "webauthn.create", challenge)),
		"transports":        []string{"internal"},
	})
}

// assertion returns the get() response for challenge with the signature
// counter at counter, signed by the credential's key.
func (a *softAuthenticator) assertion(t *testing.T, challenge string, counter uint32) json.RawMessage {
	t.Helper()
	authData := a.authData(t, 0x01, counter, false) // UP
	clientData := a.clientData(t, "webauthn.get", challenge)
	clientDataHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientDataHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return a.credential(t, map[string]any{
		"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		"signature":         base64.RawURLEncoding.EncodeToString(sig),
		"userHandle":        base64.RawURLEncoding.EncodeToString(webauthn.NewAdminUser().WebAuthnID()),
	})
}

func (a *softAuthenticator) clientData(t *testing.T, ceremony, challenge string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type": ceremony, "challenge": challenge, "origin": softOrigin, "crossOrigin": false,
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	return b
}

// authData lays out the authenticator data: rpIdHash, flags, counter and, for
// a registration, the attested credential data (zero AAGUID, id, COSE key).
func (a *softAuthenticator) authData(t *testing.T, flags byte, counter uint32, attested bool) []byte {
	t.Helper()
	rpIDHash := sha256.Sum256([]byte(softRPID))
	out := append([]byte(nil), rpIDHash[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, counter)
	if !attested {
		return out
	}
	point, err := a.key.PublicKey.Bytes() // uncompressed: 0x04 || x || y
	if err != nil {
		t.Fatalf("encode public key: %v", err)
	}
	coseKey, err := cbor.Marshal(map[int]any{
		1:  2,  // kty: EC2
		3:  -7, // alg: ES256
		-1: 1,  // crv: P-256
		-2: point[1:33],
		-3: point[33:65],
	})
	if err != nil {
		t.Fatalf("marshal COSE key: %v", err)
	}
	out = append(out, make([]byte, 16)...) // AAGUID
	out = binary.BigEndian.AppendUint16(out, uint16(len(a.credID)))
	out = append(out, a.credID...)
	return append(out, coseKey...)
}

func (a *softAuthenticator) credential(t *testing.T, response map[string]any) json.RawMessage {
	t.Helper()
	id := base64.RawURLEncoding.EncodeToString(a.credID)
	b, err := json.Marshal(map[string]any{"id": id, "rawId": id, "type": "public-key", "response": response})
	if err != nil {
		t.Fatalf("marshal credential: %v", err)
	}
	return b
}

// startCeremony calls a start endpoint and returns the session id it minted.
func startCeremony(t *testing.T, start http.HandlerFunc, path string) string {
	t.Helper()
	req, w := newChiRequest(http.MethodPost, path, http.NoBody)
	start(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	var resp struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil || resp.SessionID == "" {
		t.Fatalf("%s: no session_id in response (%v)", path, err)
	}
	return resp.SessionID
}

// sessionChallenge reads back the challenge a start endpoint stored for the
// ceremony, the one a browser would echo in clientDataJSON.
func sessionChallenge(t *testing.T, repo *webauthn.Repository, sessionID string) string {
	t.Helper()
	rec, err := repo.GetSession(context.Background(), uuid.MustParse(sessionID))
	if err != nil {
		t.Fatalf("load ceremony session: %v", err)
	}
	var data webauthnx.SessionData
	if err := json.Unmarshal(rec.SessionData, &data); err != nil {
		t.Fatalf("decode ceremony session: %v", err)
	}
	return data.Challenge
}

func finishCeremony(t *testing.T, finish http.HandlerFunc, path, sessionID string, cred json.RawMessage) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"session_id": sessionID, "credential": cred})
	if err != nil {
		t.Fatalf("marshal finish body: %v", err)
	}
	req, w := newChiRequest(http.MethodPost, path, bytes.NewReader(body))
	finish(w, req)
	return w
}

// A passkey is enrolled and then used to sign in, end to end through the real
// library verification: the registration stores the credential, the login
// mints a session token the session manager accepts and advances the stored
// signature counter. An assertion that replays a counter is the library's
// clone signal: the handler refuses it and revokes the credential, so neither
// the clone nor the original can sign in with it again.
func TestPasskeyCeremony_RegisterLoginAndCloneRefusal(t *testing.T) {
	ctx := context.Background()
	repo := webauthn.NewRepository(apiTestDB.Pool())
	rp, err := webauthn.NewRelyingParty(softRPID, "Model Hotel Test", []string{softOrigin})
	if err != nil {
		t.Fatalf("relying party: %v", err)
	}
	adminMgr := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := newTestWebAuthnHandler(repo, rp, webauthn.NewSessionManager(repo), adminMgr)
	auth := newSoftAuthenticator(t)
	t.Cleanup(func() { _ = repo.DeleteCredential(ctx, auth.credID) })

	sessionID := startCeremony(t, h.RegisterStart, "/webauthn/register/start")
	w := finishCeremony(t, h.RegisterFinish, "/webauthn/register/finish", sessionID,
		auth.attestation(t, sessionChallenge(t, repo, sessionID)))
	if w.Code != http.StatusOK {
		t.Fatalf("register finish: %d %s", w.Code, w.Body.String())
	}
	cred, err := repo.GetCredentialByID(ctx, auth.credID)
	if err != nil {
		t.Fatalf("registered credential not on file: %v", err)
	}
	if cred.AttestationFormat != "none" || cred.SignCount != 0 {
		t.Errorf("stored credential = format %q, sign count %d; want none, 0", cred.AttestationFormat, cred.SignCount)
	}

	sessionID = startCeremony(t, h.LoginStart, "/webauthn/login/start")
	w = finishCeremony(t, h.LoginFinish, "/webauthn/login/finish", sessionID,
		auth.assertion(t, sessionChallenge(t, repo, sessionID), 1))
	if w.Code != http.StatusOK {
		t.Fatalf("login finish: %d %s", w.Code, w.Body.String())
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&login); err != nil || login.Token == "" {
		t.Fatalf("login finish returned no token (%v): %s", err, w.Body.String())
	}
	t.Cleanup(func() { h.sessionMgr.RevokeAuthToken(ctx, login.Token) })
	if !h.sessionMgr.Validate(ctx, login.Token) {
		t.Error("the session manager does not accept the token the login minted")
	}
	if cred, err = repo.GetCredentialByID(ctx, auth.credID); err != nil || cred.SignCount != 1 {
		t.Errorf("after login: sign count %d, err %v; want 1 stored", cred.SignCount, err)
	}

	sessionID = startCeremony(t, h.LoginStart, "/webauthn/login/start")
	w = finishCeremony(t, h.LoginFinish, "/webauthn/login/finish", sessionID,
		auth.assertion(t, sessionChallenge(t, repo, sessionID), 1))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("replayed counter: %d %s; want %d", w.Code, w.Body.String(), http.StatusBadRequest)
	}
	if _, err := repo.GetCredentialByID(ctx, auth.credID); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("credential after the clone signal: err %v; want it revoked", err)
	}
}
