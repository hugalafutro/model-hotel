package user

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestAdminIdentity(t *testing.T) {
	id := AdminIdentity()
	if !id.IsAdmin() {
		t.Error("AdminIdentity must be admin")
	}
	if id.Username != "admin" {
		t.Errorf("AdminIdentity username = %q, want admin", id.Username)
	}
	// Admins bypass every grant check regardless of the (empty) grant list.
	for _, g := range AllGrants() {
		if !id.Can(g) {
			t.Errorf("admin must be able to %q", g)
		}
	}
}

func TestIdentity_IsAdmin(t *testing.T) {
	if (&Identity{Role: RoleUser}).IsAdmin() {
		t.Error("user role must not be admin")
	}
	var nilID *Identity
	if nilID.IsAdmin() {
		t.Error("nil identity must not be admin")
	}
}

func TestIdentity_Can(t *testing.T) {
	u := &Identity{Role: RoleUser, Grants: []string{string(GrantUsage), string(GrantLogs)}}
	if !u.Can(GrantUsage) || !u.Can(GrantLogs) {
		t.Error("user must have its granted features")
	}
	if u.Can(GrantVirtualKeys) {
		t.Error("user must not have an ungranted feature")
	}
	var nilID *Identity
	if nilID.Can(GrantUsage) {
		t.Error("nil identity can do nothing")
	}
}

func TestIdentity_ContextRoundTrip(t *testing.T) {
	uid := uuid.New()
	want := &Identity{Role: RoleUser, Grants: []string{string(GrantChat)}, UserID: &uid, Username: "kim"}
	ctx := WithIdentity(context.Background(), want)
	if got := IdentityFrom(ctx); got != want {
		t.Errorf("IdentityFrom = %v, want %v", got, want)
	}
	// A context that never passed through the middleware yields nil.
	if got := IdentityFrom(context.Background()); got != nil {
		t.Errorf("IdentityFrom(empty) = %v, want nil", got)
	}
}
