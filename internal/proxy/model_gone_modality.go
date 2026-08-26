package proxy

import (
	"encoding/json"
	"slices"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// modalityRulesOutSurface reports whether a gone-classified refusal that arrived
// on this upstream surface may count against the model at all.
//
// Nothing filters a request by modality on the way in, so `POST /v1/embeddings`
// naming a chat model is forwarded, and a provider answering "gpt-4o is not
// supported for embeddings" has named the model beside a gone-phrase. The probe
// cannot rescue it: it asks on the surface the strikes arrived on, so it
// reproduces the misuse and confirms.
//
// The two surfaces answer to different burdens of proof, because the cost of
// guessing is not symmetric:
//
//   - Embeddings requires POSITIVE evidence — the catalog has to say this model
//     produces embeddings. Being wrong retires a live chat model gateway-wide;
//     being cautious only means an embeddings model whose catalog declares
//     nothing is never auto-retired, the same trade probeEndpointForFamily takes
//     for rerank. It matters because liveModelStub writes "[]" for every model no
//     catalog covers.
//   - Chat keeps the opposite default. Chat is what most models are and what
//     most refusals arrive on, so demanding a declared modality would switch
//     traffic-driven retirement off for every uncatalogued model at once. A
//     silent catalog may strike; one that positively describes something a chat
//     completion cannot be about may not.
//
// The chat test reads both arrays because either can settle it and neither can
// alone: an image model declares output ["image"] and a TTS model ["audio"],
// while a speech-to-text model declares output ["text"] like any chat model and
// gives itself away on the INPUT side with ["audio"].
//
// Text and code are the whole allow-list, and deliberately not the vocabulary of
// things a chat completion can CARRY: a chat model that also emits images
// declares ["text","image"] and is admitted by the text, while one declaring
// image alone is an images-endpoint model whatever it can be coaxed into. See
// canonicalModalityRank in internal/provider for the full vocabulary.
func modalityRulesOutSurface(m *model.Model, probeEndpoint string) bool {
	out := declaredModalities(m.OutputModalities)
	if probeEndpoint == probeEmbeddingsEndpoint {
		// Positive evidence, so an undeclared list rules the surface out.
		return !slices.Contains(out, "embedding")
	}
	return !admitsChatModalities(out) || !admitsChatModalities(declaredModalities(m.InputModalities))
}

// modalityAdmitsBothProbeSurfaces reports whether the catalog says this model
// serves chat AND embeddings.
//
// Such a model is not auto-retired at all, because of the mismatch between what
// the evidence is about and what the action does. Strikes, probes and successes
// are per surface, but AutoRetireIfConfirmed disables the model ROW, so a chat
// retirement would take the working embeddings surface with it. No probe can
// catch that: the probe is right, the model really is gone on chat, and the
// disable is simply broader than the finding.
//
// Retiring per surface is the honest fix and the schema does not offer it — one
// row is one model, enabled or not — so this takes the same trade as image, TTS,
// STT and rerank and leaves the model enabled until discovery drops it or an
// operator disables it by hand.
//
// It costs almost nothing: it needs a provider serving one model id on both
// surfaces, which is rare, and an empty modality list already rules the
// embeddings surface out.
func modalityAdmitsBothProbeSurfaces(m *model.Model) bool {
	return !modalityRulesOutSurface(m, probeChatEndpoint) && !modalityRulesOutSurface(m, probeEmbeddingsEndpoint)
}

// declaredModalities parses one of the model's modality columns, returning nil
// for anything it cannot read as a list. Callers treat nil as "the catalog says
// nothing", which is not the same as "the catalog says no".
func declaredModalities(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil {
		return nil
	}
	return list
}

// admitsChatModalities reports whether a declared modality list leaves room for
// a chat completion. An empty list does: nothing declared is not evidence.
func admitsChatModalities(list []string) bool {
	if len(list) == 0 {
		return true
	}
	return slices.Contains(list, "text") || slices.Contains(list, "code")
}
