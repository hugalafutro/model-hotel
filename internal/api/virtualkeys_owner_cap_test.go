package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// A capped user must not be able to mint an unrestricted key. This is the half
// of the reported finding that a per-key guard could not close: on create there
// is no prior key value to use as a ceiling.
func TestCreateVirtualKey_NonAdminCannotEscapeOwnerCap(t *testing.T) {
	router, loginAs, _ := setupOwnershipTest(t)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"cap-alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Alice mints a key without naming providers: she gets her cap, not "all".
	w = doJSONAs(t, router, http.MethodPost, "/virtual-keys", loginAs, &alice, `{"name":"alice-key"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	var key struct {
		AllowedProviders *[]string `json:"allowed_providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &key); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	if key.AllowedProviders == nil {
		t.Fatal("ESCALATION: capped user minted an unrestricted key")
	}
	if len(*key.AllowedProviders) != 1 || (*key.AllowedProviders)[0] != p1 {
		t.Fatalf("key allowed_providers = %v, want [%s]", *key.AllowedProviders, p1)
	}
}

func TestCreateVirtualKey_NonAdminCannotNameProviderOutsideCap(t *testing.T) {
	router, loginAs, _ := setupOwnershipTest(t)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	p2 := seedProvider(t, "cap-prov-b", "sk-b", testMasterKey)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"cap-alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}

	w = doJSONAs(t, router, http.MethodPost, "/virtual-keys", loginAs, &alice,
		`{"name":"alice-key","allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cap-alice") {
		t.Errorf("error should name the owner so an admin knows what to raise: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cap-prov-b") {
		t.Errorf("error should name the provider that was refused: %s", w.Body.String())
	}

	// An id with no provider row is still refused, named by its raw id: the
	// message degrades, the rule does not.
	ghost := "00000000-0000-0000-0000-0000000000ab"
	w = doJSONAs(t, router, http.MethodPost, "/virtual-keys", loginAs, &alice,
		`{"name":"alice-key","allowed_providers":["`+ghost+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ghost provider: status = %d, want 400; body %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ghost) {
		t.Errorf("error should fall back to the raw provider id: %s", w.Body.String())
	}
}

// The rule binds admins too, so the dashboard can never show access that the
// proxy will deny at runtime. Admins are not blocked: they raise the cap first.
func TestUpdateVirtualKey_AdminBoundByOwnerCap(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	p2 := seedProvider(t, "cap-prov-b", "sk-b", testMasterKey)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"cap-alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}

	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"alice-key","owner_user_id":"`+alice.ID.String()+`","allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())

	// Admin tries to widen the key past alice's cap.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"alice-key","allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
	}

	// Raising alice's cap first makes the same edit succeed.
	w = doJSON(t, router, http.MethodPut, "/users/"+alice.ID.String(), envAdminToken,
		`{"username":"cap-alice","role":"user","grants":["virtual_keys"],"enabled":true,"allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("raise cap: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"alice-key","allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status after raising cap = %d, want 200; body %s", w.Code, w.Body.String())
	}
}

// The cap that binds an update is the OWNER THE WRITE STORES, not the one the
// key had: reassigning a wide-open key to a capped user must not smuggle the
// wide-open list past the cap.
func TestUpdateVirtualKey_CapFollowsTheNewOwner(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	p2 := seedProvider(t, "cap-prov-b", "sk-b", testMasterKey)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"cap-alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// An unowned key reaching both providers.
	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"service-key","allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())

	// Handing it to alice is refused: her cap, not the key's old freedom, rules.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"service-key","owner_user_id":"`+alice.ID.String()+`"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", w.Code, w.Body.String())
	}

	// Narrowing it to her cap in the same write is fine.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"service-key","owner_user_id":"`+alice.ID.String()+`","allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("narrowed reassign: %d %s", w.Code, w.Body.String())
	}
}

// failingUserStore breaks only Get, which is the call the cap lookup makes.
// The other methods are never reached in the test that uses it.
type failingUserStore struct{ err error }

func (f failingUserStore) Get(context.Context, uuid.UUID) (*user.User, error) { return nil, f.err }
func (f failingUserStore) List(context.Context) ([]*user.User, error)         { return nil, f.err }
func (f failingUserStore) Create(context.Context, string, string, *string, string, user.Role, []string, user.Limits, *[]string) (*user.User, error) {
	return nil, f.err
}

func (f failingUserStore) Update(context.Context, uuid.UUID, string, string, *string, user.Role, []string, bool, user.Limits, *[]string) (*user.User, error) {
	return nil, f.err
}
func (f failingUserStore) SetPassword(context.Context, uuid.UUID, string) error { return f.err }
func (f failingUserStore) Delete(context.Context, uuid.UUID) error              { return f.err }

// An unreadable user store must fail the write, not wave it through uncapped:
// a database blip is not evidence that the owner has no cap.
func TestVirtualKey_OwnerCapLookupFailsClosed(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, webauthn_sessions, virtual_keys CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	h.SetUserAuth(user.NewRepository(pool), nil)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"cap-alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}
	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"alice-key","owner_user_id":"`+alice.ID.String()+`","allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())

	h.SetUserAuth(failingUserStore{err: errors.New("user store unavailable")}, nil)

	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"another-key","owner_user_id":"`+alice.ID.String()+`"}`)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("create with unreadable cap: %d, want 500; body %s", w.Code, w.Body.String())
	}
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"alice-key","allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("update with unreadable cap: %d, want 500; body %s", w.Code, w.Body.String())
	}
}

// The error-message helpers degrade rather than panic when nothing is wired:
// they only decide how friendly a refusal reads, never whether it happens.
func TestOwnerCapMessageHelpers_FallBackToIDs(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()

	if got := h.ownerLabel(ctx, nil); got != "the owner" {
		t.Errorf("ownerLabel(nil) = %q, want %q", got, "the owner")
	}
	id := uuid.New()
	if got := h.ownerLabel(ctx, &id); got != id.String() {
		t.Errorf("ownerLabel(unknown) = %q, want the raw id", got)
	}
	if got := h.providerNameByID(ctx)("some-id"); got != "some-id" {
		t.Errorf("providerNameByID(unknown) = %q, want the raw id", got)
	}
}

// An unowned key (admin service key) has no owner and therefore no cap.
func TestVirtualKey_UnownedKeyUnaffectedByCaps(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)
	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken, `{"name":"service-key"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())
	if created.AllowedProviders != nil {
		t.Errorf("unowned key gained a restriction: %v", *created.AllowedProviders)
	}
}
