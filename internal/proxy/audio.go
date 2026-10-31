package proxy

import "net/http"

// The /v1/audio/* handlers: speech, transcription, translation and music.
// Each attaches its endpoint path and family to the shared pass-through
// pipeline in multimodal.go.

// AudioSpeech proxies OpenAI-compatible POST /v1/audio/speech requests.
// The response is binary audio (or SSE when stream_format=sse) and is
// streamed through without buffering.
func (h *Handler) AudioSpeech(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/audio/speech", endpointTypeTTS)
}

// AudioTranscriptions proxies OpenAI-compatible POST /v1/audio/transcriptions
// requests (multipart: audio file + model + optional params).
func (h *Handler) AudioTranscriptions(w http.ResponseWriter, r *http.Request) {
	h.serveMultipartPassthrough(w, r, "/audio/transcriptions", endpointTypeSTT)
}

// AudioTranslations proxies OpenAI-compatible POST /v1/audio/translations
// requests (multipart, same shape as transcriptions, always English output).
func (h *Handler) AudioTranslations(w http.ResponseWriter, r *http.Request) {
	h.serveMultipartPassthrough(w, r, "/audio/translations", endpointTypeSTT)
}

// AudioMusic proxies POST /v1/audio/music, music generation. No OpenAI route
// exists for it, so the body is KoboldCpp's own: `model` plus `caption`,
// `lyrics` and `duration` in seconds (ACE-Step families also take `bpm`,
// `keyscale` and `inference_steps`), forwarded verbatim to the server's
// native /api/extra/music/generate, which answers raw WAV. Only KoboldCpp
// providers serve it: candidates on any other provider type are dropped
// before an attempt, and a request left with none is the client's 400.
func (h *Handler) AudioMusic(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, musicEndpointPath, endpointTypeMusic)
}
