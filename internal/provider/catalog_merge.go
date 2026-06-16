package provider

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// liveModelStub builds a minimal model from a live listing entry (id + owner).
// Only the id, name, owner and a streaming capability are set; every richer
// field is left empty so mergeLiveAndCatalog (and then models.dev) can backfill
// it without a fabricated placeholder masking the catalog value.
func liveModelStub(modelID, ownedBy string, providerID uuid.UUID) *model.Model {
	capJSON, _ := json.Marshal(model.Capability{Streaming: true})
	return &model.Model{
		ID:           uuid.New(),
		ProviderID:   providerID,
		ModelID:      modelID,
		Name:         modelID,
		DisplayName:  modelID,
		Capabilities: string(capJSON),
		Params:       "{}",
		// Modalities are JSONB columns: emit valid empty arrays (not "") so the
		// upsert succeeds even when neither the catalog nor models.dev fill them.
		// "[]" is still treated as empty/fillable by backfill and models.dev.
		InputModalities:  "[]",
		OutputModalities: "[]",
		OwnedBy:          ownedBy,
		Enabled:          true,
	}
}

// mergeLiveAndCatalog unions live-discovered models with catalog models.
//
// Precedence is "live wins, catalog backfills": a model returned by the live
// provider API is the source of truth for its fields, and the matching catalog
// entry only fills fields the live result left empty/nil/placeholder. Catalog
// entries with no live match are unioned in as-is (covering freshly released
// models the provider has not yet added to its /models listing, or providers
// with no listing endpoint at all). Live-only models pass through untouched so
// newly released models are picked up automatically without a catalog edit.
//
// After this merge the caller still runs models.dev enrichment, which fills any
// fields neither live nor catalog supplied. The resulting precedence per field
// is therefore: live > catalog > models.dev > zero value.
//
// Matching is by case-insensitive model_id. Returned order is live models
// first (in their original order), then catalog-only models in catalog order.
func mergeLiveAndCatalog(live, catalog []*model.Model) []*model.Model {
	// Flag the fields the live API actually populated BEFORE the catalog backfills
	// the gaps, so only provider-reported values are marked live-sourced. Catalog
	// backfill and models.dev enrichment run afterward and stay fill-only.
	markLiveMeta(live)

	byID := make(map[string]*model.Model, len(live)+len(catalog))
	out := make([]*model.Model, 0, len(live)+len(catalog))

	for _, m := range live {
		key := strings.ToLower(m.ModelID)
		if _, dup := byID[key]; dup {
			continue
		}
		byID[key] = m
		out = append(out, m)
	}

	for _, c := range catalog {
		key := strings.ToLower(c.ModelID)
		if existing, ok := byID[key]; ok {
			backfillFromCatalog(existing, c)
			continue
		}
		byID[key] = c
		out = append(out, c)
	}

	return out
}

// backfillLiveFromCatalog enriches live models in place from matching catalog
// entries (case-insensitive model_id) WITHOUT unioning catalog-only models.
//
// Use this instead of mergeLiveAndCatalog when the catalog must never introduce
// a model the live API did not return — in particular the OpenAI discoverer,
// which is also the fallback for unknown/custom hosts where adding the gpt-5.x
// catalog as phantom models would be wrong. (For real OpenAI the catalog is a
// subset of the live listing, so there is nothing to union anyway.)
func backfillLiveFromCatalog(live, catalog []*model.Model) []*model.Model {
	// Mark live-sourced fields before backfilling (see mergeLiveAndCatalog). For
	// id-only stub providers this flags nothing, leaving every metadata field
	// fill-only — exactly what keeps their catalog/models.dev values stable.
	markLiveMeta(live)
	if len(catalog) == 0 {
		return live
	}
	byID := make(map[string]*model.Model, len(catalog))
	for _, c := range catalog {
		byID[strings.ToLower(c.ModelID)] = c
	}
	for _, m := range live {
		if c, ok := byID[strings.ToLower(m.ModelID)]; ok {
			backfillFromCatalog(m, c)
		}
	}
	return live
}

// markLiveMeta flags each model's currently-set pricing/context fields as
// live-sourced, recording that the provider's own API reported them this scan.
//
// Call it on the provider-built live slice at the boundary between fetching the
// provider payload and backfilling from the catalog / models.dev, so only
// provider-reported values are flagged. NEVER call it on a catalog slice or a
// catalog-only fallback return (e.g. xAI on a 403/429): catalog values must
// stay non-live so a degraded scan can't overwrite a stored live value.
//
// Marked fields overwrite on upsert (a genuine provider change propagates);
// everything else is fill-only and stays stable. Discoverers whose pricing is a
// hardcoded table rather than wire data (anthropic/google/cohere) deliberately
// leave their values unmarked — fill-only freezes them to the constant table
// value, which is what we want.
func markLiveMeta(models []*model.Model) {
	for _, m := range models {
		if m != nil {
			m.MarkLiveMetaFromCurrent()
		}
	}
}

// backfillFromCatalog fills fields of the live model dst that are empty, nil, or
// a known low-quality placeholder using the catalog model src. It never
// overwrites a meaningful value the live API already provided.
func backfillFromCatalog(dst, src *model.Model) {
	if src == nil {
		return
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
	// DisplayName == ModelID is the low-quality default many live endpoints
	// emit (the raw id). Treat it as empty so a nicer catalog name wins, while
	// a genuine live display name still takes precedence.
	if (dst.DisplayName == "" || dst.DisplayName == dst.ModelID) && src.DisplayName != "" {
		dst.DisplayName = src.DisplayName
	}
	if dst.Modality == "" {
		dst.Modality = src.Modality
	}
	if isEmptyModalities(dst.InputModalities) {
		dst.InputModalities = src.InputModalities
	}
	if isEmptyModalities(dst.OutputModalities) {
		dst.OutputModalities = src.OutputModalities
	}
	if dst.ContextLength == nil {
		dst.ContextLength = src.ContextLength
	}
	if dst.MaxOutputTokens == nil {
		dst.MaxOutputTokens = src.MaxOutputTokens
	}
	if dst.InputPricePerMillion == nil {
		dst.InputPricePerMillion = src.InputPricePerMillion
	}
	if dst.InputPricePerMillionCacheHit == nil {
		dst.InputPricePerMillionCacheHit = src.InputPricePerMillionCacheHit
	}
	if dst.OutputPricePerMillion == nil {
		dst.OutputPricePerMillion = src.OutputPricePerMillion
	}
	if dst.OwnedBy == "" {
		dst.OwnedBy = src.OwnedBy
	}
	dst.Capabilities = mergeCapabilities(dst.Capabilities, src.Capabilities)
}

// isEmptyModalities reports whether a modalities JSON string carries no useful
// data (empty, an empty array/object, or null).
func isEmptyModalities(s string) bool {
	switch strings.TrimSpace(s) {
	case "", "[]", "{}", "null":
		return true
	}
	return false
}

// mergeCapabilities ORs the boolean capability flags of the live and catalog
// capability JSON. A capability is enabled in the result if either source
// reports it, which matches how capabilities are additive metadata rather than
// authoritative on/off switches the live API guarantees.
func mergeCapabilities(liveJSON, catalogJSON string) string {
	var live, cat model.Capability
	if liveJSON != "" && liveJSON != "{}" {
		_ = json.Unmarshal([]byte(liveJSON), &live)
	}
	if catalogJSON != "" && catalogJSON != "{}" {
		_ = json.Unmarshal([]byte(catalogJSON), &cat)
	}
	merged := model.Capability{
		Streaming:         live.Streaming || cat.Streaming,
		Vision:            live.Vision || cat.Vision,
		VideoInput:        live.VideoInput || cat.VideoInput,
		AudioInput:        live.AudioInput || cat.AudioInput,
		Reasoning:         live.Reasoning || cat.Reasoning,
		ToolCalling:       live.ToolCalling || cat.ToolCalling,
		ParallelToolCalls: live.ParallelToolCalls || cat.ParallelToolCalls,
		StructuredOutput:  live.StructuredOutput || cat.StructuredOutput,
		PDFUpload:         live.PDFUpload || cat.PDFUpload,
	}
	b, _ := json.Marshal(merged)
	return string(b)
}
