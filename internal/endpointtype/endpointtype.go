// Package endpointtype holds the vocabulary of request_logs.endpoint_type:
// the families a request log row can be stamped with. It is a leaf so both the
// proxy, which stamps the column, and the admin API, which filters on it and
// writes probe rows, share one list without the API importing the proxy.
package endpointtype

// Endpoint families recorded in request_logs.endpoint_type.
const (
	Chat       = "chat"
	Messages   = "messages"
	Responses  = "responses"
	Embeddings = "embeddings"
	Rerank     = "rerank"
	Image      = "image"
	TTS        = "tts"
	STT        = "stt"
)

// All returns every family the constants above name, in the order the
// dashboard offers them. The endpoint_type log filter validates against it, so
// a family added above and left out here would be a filter value that matches
// every row: TestAllCoversEveryConstant keeps the two in step. Each call
// returns a fresh slice, so no caller can edit the list another one reads.
func All() []string {
	return []string{Chat, Messages, Responses, Embeddings, Rerank, Image, TTS, STT}
}
