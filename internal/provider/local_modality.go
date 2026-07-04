package provider

import "strings"

// inferNonChatModality guesses a non-chat modality ("embedding" or "rerank")
// from a model ID when the provider does not report one.
//
// Self-hosted OpenAI-compatible servers (LM Studio's /v1/models, KoboldCPP,
// llama.cpp, vLLM, LocalAI, text-generation-webui, ...) list embedding and
// reranker models with no type information. Without this they land as
// modality:"text" and wrongly appear in the chat/arena pickers, where they can
// never work. Local Ollama and LM Studio's native /api/v0 endpoint report the
// type authoritatively and don't need this; it is the fallback for the plain
// OpenAI /models listing.
//
// It returns "" for anything that does not clearly look like an embedding or
// reranker, so a normal chat model is never hidden. The returned strings must
// stay in sync with the frontend's NON_CHAT_MODALITIES set
// (web/src/utils/model.ts).
func inferNonChatModality(modelID string) string {
	id := strings.ToLower(modelID)

	// Rerankers: "bge-reranker", "jina-reranker", "mxbai-rerank", "cohere-rerank".
	if strings.Contains(id, "rerank") {
		return "rerank"
	}

	// Embedding models almost always carry "embed" in the name: nomic-embed-text,
	// mxbai-embed-large, text-embedding-3-small, snowflake-arctic-embed,
	// embeddinggemma, gte-*-embedding, ...
	if strings.Contains(id, "embed") {
		return "embedding"
	}

	// Well-known embedding families that don't spell out "embed". Match them as
	// whole segments (split on the usual id separators) so a substring can't
	// trip a chat model that merely contains these letters.
	for _, seg := range splitModelIDSegments(id) {
		switch seg {
		case "bge", "gte", "e5", "minilm":
			return "embedding"
		}
	}

	return ""
}

// splitModelIDSegments splits a lower-cased model ID on the separators commonly
// found in HuggingFace-style IDs (org/name-with-parts).
func splitModelIDSegments(id string) []string {
	return strings.FieldsFunc(id, func(r rune) bool {
		switch r {
		case '/', '-', '_', '.', ':', ' ':
			return true
		default:
			return false
		}
	})
}
