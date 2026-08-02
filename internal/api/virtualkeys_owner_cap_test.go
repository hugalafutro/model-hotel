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

// Narrowing an owner's cap below what one of their keys already stores must not
// brick the key. An update that omits allowed_providers asserts nothing about
// provider access, so it is not re-checked and the stored list survives
// untouched; the proxy's runtime intersection is what keeps the excess from
// routing. Before this, the key had no legal edit at all: omitting the field
// re-validated the preserved value and 400'd, and re-sending the stored list
// 400'd on the same branch.
func TestUpdateVirtualKey_OmittedProvidersSurviveANarrowedCap(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	p2 := seedProvider(t, "cap-prov-b", "sk-b", testMasterKey)
	w := doJSON(t, router, http.MethodPost, "/users", envAdminToken,
		`{"username":"cap-alice","password":"password123","role":"user","grants":["virtual_keys"],"allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", w.Code, w.Body.String())
	}
	var alice user.User
	if err := json.Unmarshal(w.Body.Bytes(), &alice); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// A key using the full width of alice's cap.
	w = doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"alice-key","owner_user_id":"`+alice.ID.String()+`","allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())

	// Alice's cap is narrowed afterwards; the key's stored list now exceeds it.
	w = doJSON(t, router, http.MethodPut, "/users/"+alice.ID.String(), envAdminToken,
		`{"username":"cap-alice","role":"user","grants":["virtual_keys"],"enabled":true,"allowed_providers":["`+p1+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("narrow cap: %d %s", w.Code, w.Body.String())
	}

	// A plain rename, omitting allowed_providers, must succeed.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"alice-key-renamed"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rename under a narrowed cap: %d, want 200; body %s", w.Code, w.Body.String())
	}

	// And the stored intent survives verbatim: the point is preservation, not
	// merely a 200, so assert what the row actually holds.
	w = doJSON(t, router, http.MethodGet, "/virtual-keys", envAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var keys []struct {
		ID               string    `json:"id"`
		Name             string    `json:"name"`
		AllowedProviders *[]string `json:"allowed_providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found bool
	for _, k := range keys {
		if k.ID != created.ID {
			continue
		}
		found = true
		if k.Name != "alice-key-renamed" {
			t.Errorf("name = %q, want the rename to have landed", k.Name)
		}
		if k.AllowedProviders == nil {
			t.Fatal("stored allowed_providers was cleared by an untouched field")
		}
		if len(*k.AllowedProviders) != 2 {
			t.Fatalf("stored allowed_providers = %v, want both providers preserved", *k.AllowedProviders)
		}
	}
	if !found {
		t.Fatal("renamed key missing from the list")
	}

	// The exemption is only for an untouched field. Naming the over-wide list
	// explicitly is still a claim, and still refused.
	w = doJSON(t, router, http.MethodPut, "/virtual-keys/"+created.ID, envAdminToken,
		`{"name":"alice-key-renamed","allowed_providers":["`+p1+`","`+p2+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("explicit over-wide list: %d, want 400; body %s", w.Code, w.Body.String())
	}
}

// A create always makes a claim, so the exemption above must not reach it: a
// capped owner's new key with no providers named still resolves to the cap
// rather than to no restriction.
func TestCreateVirtualKey_OmittedProvidersStillResolveToTheCap(t *testing.T) {
	router, _, _ := setupOwnershipTest(t)

	p1 := seedProvider(t, "cap-prov-a", "sk-a", testMasterKey)
	seedProvider(t, "cap-prov-b", "sk-b", testMasterKey)
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
		`{"name":"alice-key","owner_user_id":"`+alice.ID.String()+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	created := decodeVK(t, w.Body.Bytes())
	if created.AllowedProviders == nil {
		t.Fatal("ESCALATION: a capped owner's key was created unrestricted")
	}
	if len(*created.AllowedProviders) != 1 || (*created.AllowedProviders)[0] != p1 {
		t.Fatalf("allowed_providers = %v, want [%s]", *created.AllowedProviders, p1)
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

// A store that reports neither a row nor an error yields no cap, and the
// foreign key is what stops the write. The real repository returns ErrNotFound,
// but the cap lookup must not nil-deref if it ever gets the other shape.
func TestVirtualKey_OwnerCapToleratesRowlessUserStore(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)
	h.SetUserAuth(failingUserStore{}, nil)

	w := doJSON(t, router, http.MethodPost, "/virtual-keys", envAdminToken,
		`{"name":"ghost-owner-key","owner_user_id":"00000000-0000-0000-0000-000000000001"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 from the owner foreign key; body %s", w.Code, w.Body.String())
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
