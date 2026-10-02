package provider

import (
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// newServedModel is the row a self-hosted server's listing entry starts
// from: enabled, named after its id, with no capabilities or params until
// the family's discovery fills them in. ownedBy is the family (the listing's
// owned_by is the server's own label and varies in case), description the
// family's one-line note.
func newServedModel(provider *Provider, id, ownedBy, description string) *model.Model {
	return &model.Model{
		ID:           uuid.New(),
		ProviderID:   provider.ID,
		ModelID:      id,
		Name:         id,
		DisplayName:  id,
		Description:  description,
		Capabilities: "{}",
		Params:       "{}",
		OwnedBy:      ownedBy,
		Enabled:      true,
	}
}
