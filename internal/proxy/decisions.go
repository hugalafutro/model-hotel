package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// SystemOne proxies POST /v1/systemone, the System One decision API of vLLM
// Semantic Router: a state and typed questions (choice, yes/no, score, span)
// in, an answer with probabilities per question out, never generated text.
// The body passes through verbatim with the model rewritten; the upstream
// reports usage as input_tokens, which extractPassthroughUsage reads.
func (h *Handler) SystemOne(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/systemone", endpointTypeDecisions)
}

// Decisions proxies POST /v1/decisions, the same API in its native form. The
// two routes differ in how a question the model cannot answer is reported: the
// upstream fails a /v1/systemone request whole with a 4xx, and answers a
// /v1/decisions one in place with an error member on that answer inside a 200,
// which is forwarded as the answer it is.
func (h *Handler) Decisions(w http.ResponseWriter, r *http.Request) {
	h.serveJSONPassthrough(w, r, "/decisions", endpointTypeDecisions)
}

// carriesDecisionRefusal reports a System One refusal on a decisions request:
// the flat shape /v1/systemone answers a payload error in, a "code" beside a
// "detail", with no "error" object for carriesErrorObject to find. It names
// the question that failed and why (a type the model does not answer, a
// malformed option list, an input over the limit), which is the only way a
// caller learns what to change, so it is forwarded like any other
// payload-class error object. Read for the decisions family alone: "detail"
// is FastAPI's member on every route, and only here is its shape a contract.
func carriesDecisionRefusal(endpointType string, body []byte) bool {
	if endpointType != endpointTypeDecisions {
		return false
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	return util.ValueCarries(envelope["code"]) && util.ValueCarries(envelope["detail"])
}
