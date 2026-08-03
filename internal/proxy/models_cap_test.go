package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// capFixture is one /v1/models scenario: two providers with one enabled model
// each, plus a failover group whose priority order puts the first provider's
// model ahead of the second's. Every case below varies only the allow-lists in
// the request context, so what changes in the response is attributable to the
// cap and nothing else.
type capFixture struct {
	providerAID uuid.UUID
	providerBID uuid.UUID
	groupName   string
}

func newCapFixture(t *testing.T, h *Handler, suffix string) capFixture {
	t.Helper()
	ctx := context.Background()

	mk := func(name, key string) uuid.UUID {
		kp, err := auth.Encrypt(key, h.cfg.MasterKey)
		if err != nil {
			t.Fatalf("failed to encrypt key: %v", err)
		}
		p, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{
			Name:    name,
			BaseURL: "https://api.example.com",
			APIKey:  key,
		}, kp.Ciphertext, kp.Nonce, kp.Salt)
		if err != nil {
			t.Fatalf("failed to create provider %s: %v", name, err)
		}
		t.Cleanup(func() { _ = h.providerRepo.Delete(context.Background(), p.ID) })
		return p.ID
	}

	provA := mk("cap-list-a-"+suffix, "sk-cap-list-a-"+suffix)
	provB := mk("cap-list-b-"+suffix, "sk-cap-list-b-"+suffix)

	mkModel := func(providerID uuid.UUID, modelID, displayName string) uuid.UUID {
		id := uuid.New()
		m := &model.Model{
			ID:               id,
			ProviderID:       providerID,
			ModelID:          modelID,
			Name:             displayName,
			DisplayName:      displayName,
			Capabilities:     "{}",
			Params:           "{}",
			Modality:         "text",
			InputModalities:  "[]",
			OutputModalities: "[]",
			Enabled:          true,
			CreatedAt:        time.Now(),
			LastSeenAt:       time.Now(),
		}
		if err := h.modelRepo.Upsert(ctx, m); err != nil {
			t.Fatalf("failed to upsert model %s: %v", modelID, err)
		}
		t.Cleanup(func() { _ = h.modelRepo.DeleteByID(context.Background(), id) })
		return id
	}

	modelA := mkModel(provA, "cap-model-a-"+suffix, "Cap Model A")
	modelB := mkModel(provB, "cap-model-b-"+suffix, "Cap Model B")

	group := "cap-group-" + suffix
	if _, err := h.failoverRepo.UpsertWithConfig(ctx, group, []uuid.UUID{modelA, modelB}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	t.Cleanup(func() { _ = h.failoverRepo.Delete(context.Background(), group) })

	return capFixture{providerAID: provA, providerBID: provB, groupName: group}
}

// listWithCap calls /v1/models with the given key-side and owner-side
// allow-lists in the request context, exactly as ProxyKeyMiddleware sets them,
// and returns the response items keyed by model id.
func listWithCap(t *testing.T, h *Handler, keyAllowed, ownerAllowed *[]string) map[string]map[string]any {
	t.Helper()
	ctx := context.Background()
	if keyAllowed != nil {
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyAllowedProvidersKey, keyAllowed)
	}
	if ownerAllowed != nil {
		ctx = context.WithValue(ctx, ctxkeys.UserAllowedProvidersKey, ownerAllowed)
	}

	req := httptest.NewRequest("GET", "/models", http.NoBody).WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	byID := make(map[string]map[string]any, len(resp.Data))
	for _, item := range resp.Data {
		id, _ := item["id"].(string)
		byID[id] = item
	}
	return byID
}

// TestListModels_NoCapListsEverything pins the unrestricted case: an absent
// allow-list on both sides must leave the catalogue exactly as it was before
// this endpoint learned about caps.
func TestListModels_NoCapListsEverything(t *testing.T) {
	h := newIntegrationHandler()
	fx := newCapFixture(t, h, "nocap")

	got := listWithCap(t, h, nil, nil)

	for _, id := range []string{
		"cap-list-a-nocap/cap-model-a-nocap",
		"cap-list-b-nocap/cap-model-b-nocap",
		"hotel/" + fx.groupName,
	} {
		if _, ok := got[id]; !ok {
			t.Errorf("uncapped listing should contain %q", id)
		}
	}
}

// TestListModels_KeyCapHidesOtherProviders covers the key's own
// allowed_providers, which this endpoint never honoured before.
func TestListModels_KeyCapHidesOtherProviders(t *testing.T) {
	h := newIntegrationHandler()
	fx := newCapFixture(t, h, "keycap")

	keyAllowed := []string{fx.providerBID.String()}
	got := listWithCap(t, h, &keyAllowed, nil)

	if _, ok := got["cap-list-a-keycap/cap-model-a-keycap"]; ok {
		t.Error("a model on a provider outside the key's allow-list must not be listed")
	}
	if _, ok := got["cap-list-b-keycap/cap-model-b-keycap"]; !ok {
		t.Error("a model on an allowed provider must still be listed")
	}

	// The group's first priority entry sits on the denied provider, so the
	// listing must fall through to the entry that would actually serve.
	group, ok := got["hotel/"+fx.groupName]
	if !ok {
		t.Fatal("a group with one reachable entry must stay listed")
	}
	if group["name"] != "Cap Model B" {
		t.Errorf("group should describe the first reachable entry, got name=%v", group["name"])
	}
}

// TestListModels_OwnerCapNarrowsKeyList exercises the intersection: the key
// names both providers, the owner account allows only one, and the listing must
// show the overlap rather than either side alone.
func TestListModels_OwnerCapNarrowsKeyList(t *testing.T) {
	h := newIntegrationHandler()
	fx := newCapFixture(t, h, "ownercap")

	keyAllowed := []string{fx.providerAID.String(), fx.providerBID.String()}
	ownerAllowed := []string{fx.providerBID.String()}
	got := listWithCap(t, h, &keyAllowed, &ownerAllowed)

	if _, ok := got["cap-list-a-ownercap/cap-model-a-ownercap"]; ok {
		t.Error("a provider the key names but the owner cap denies must not be listed")
	}
	if _, ok := got["cap-list-b-ownercap/cap-model-b-ownercap"]; !ok {
		t.Error("a provider both sides allow must be listed")
	}
}

// TestListModels_DenyAllCapListsNothing pins the empty-list state, which is
// reachable in production (provider.PruneAllowLists rewrites a fully deleted
// allow-list to '{}') and means deny-everything, not no-restriction.
func TestListModels_DenyAllCapListsNothing(t *testing.T) {
	h := newIntegrationHandler()
	fx := newCapFixture(t, h, "denyall")

	denyAll := []string{}
	got := listWithCap(t, h, nil, &denyAll)

	if len(got) != 0 {
		t.Errorf("an empty owner cap must list no models at all, got %d", len(got))
	}
	if _, ok := got["hotel/"+fx.groupName]; ok {
		t.Error("a group with no reachable entry must be dropped")
	}
}
