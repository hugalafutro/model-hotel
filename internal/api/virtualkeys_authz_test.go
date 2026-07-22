package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

func TestCanTouchKey(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	ownedKey := &virtualkey.VirtualKey{OwnerUserID: &owner}
	unownedKey := &virtualkey.VirtualKey{}

	admin := &user.Identity{Role: user.RoleAdmin}
	ownerUser := &user.Identity{Role: user.RoleUser, UserID: &owner}
	otherUser := &user.Identity{Role: user.RoleUser, UserID: &other}

	tests := []struct {
		name string
		id   *user.Identity
		vk   *virtualkey.VirtualKey
		want bool
	}{
		{"nil identity fails closed", nil, ownedKey, false},
		{"admin may touch any key", admin, ownedKey, true},
		{"admin may touch an unowned key", admin, unownedKey, true},
		{"owner may touch own key", ownerUser, ownedKey, true},
		{"non-owner may not touch another's key", otherUser, ownedKey, false},
		{"user may not touch an unowned key", ownerUser, unownedKey, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canTouchKey(tt.id, tt.vk); got != tt.want {
				t.Fatalf("canTouchKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
