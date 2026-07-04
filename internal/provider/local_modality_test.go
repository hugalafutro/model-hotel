package provider

import "testing"

func TestInferNonChatModality(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		// Embedding models: "embed" in the name.
		{"nomic-embed-text", "embedding"},
		{"nomic-embed-text-v1.5", "embedding"},
		{"mxbai-embed-large", "embedding"},
		{"text-embedding-3-small", "embedding"},
		{"text-embedding-ada-002", "embedding"},
		{"snowflake-arctic-embed2", "embedding"},
		{"embeddinggemma", "embedding"},
		{"nomic-ai/nomic-embed-text-v1.5-GGUF", "embedding"},

		// Well-known embedding families without "embed" in the name.
		{"bge-m3", "embedding"},
		{"BAAI/bge-large-en-v1.5", "embedding"},
		{"gte-large", "embedding"},
		{"Alibaba-NLP/gte-Qwen2-7B-instruct", "embedding"},
		{"intfloat/e5-large-v2", "embedding"},
		{"multilingual-e5-large", "embedding"},
		{"all-MiniLM-L6-v2", "embedding"},

		// Rerankers.
		{"bge-reranker-v2-m3", "rerank"},
		{"jina-reranker-v2-base-multilingual", "rerank"},
		{"mxbai-rerank-large-v1", "rerank"},

		// Chat / other models must not be hidden.
		{"llama-3.1-8b-instruct", ""},
		{"qwen2.5-coder-7b", ""},
		{"mistral-7b-instruct", ""},
		{"gpt-4o", ""},
		{"gemma3:4b", ""},
		{"deepseek-r1", ""},
		{"", ""},
		// "gte"/"e5"/"bge" only match as whole segments, not substrings.
		{"together-large", ""},
		{"the5th-model", ""},
	}

	for _, tt := range tests {
		if got := inferNonChatModality(tt.id); got != tt.want {
			t.Errorf("inferNonChatModality(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
