package anthropicegress

import (
	"encoding/json"
	"strings"
	"testing"
)

// The types below mirror the wire shape the translator emits but are declared
// independently of chunk and friends (the production types) — same pattern as
// translatedCompletion in response_test.go. Decoding the output with the very
// struct that produced it would make a JSON-tag regression on the production
// side invisible to these tests. translatedUsage is shared with response_test.go
// because a chunk's usage block is the same shape a completion's is.

type streamedChunk struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []streamedChoice `json:"choices"`
	Usage   *translatedUsage `json:"usage"`
}

type streamedChoice struct {
	Index        int           `json:"index"`
	Delta        streamedDelta `json:"delta"`
	FinishReason *string       `json:"finish_reason"`
}

type streamedDelta struct {
	Role             string             `json:"role"`
	Content          string             `json:"content"`
	ReasoningContent string             `json:"reasoning_content"`
	ToolCalls        []streamedToolCall `json:"tool_calls"`
}

type streamedToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// parseChunks decodes an emitted SSE stream into its chunks and reports whether
// it ended with the [DONE] sentinel. Every frame must be a "data: " frame: the
// adapter generates its own framing, so anything else is a defect.
func parseChunks(t *testing.T, sse string) (chunks []streamedChunk, done bool) {
	t.Helper()
	for _, frame := range strings.Split(sse, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		payload, ok := strings.CutPrefix(frame, "data: ")
		if !ok {
			t.Fatalf("frame is not a data frame: %q", frame)
		}
		if payload == "[DONE]" {
			done = true
			continue
		}
		if done {
			t.Fatalf("frame after [DONE]: %q", frame)
		}
		var c streamedChunk
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			t.Fatalf("decode chunk %q: %v", payload, err)
		}
		if c.Object != "chat.completion.chunk" {
			t.Errorf("object = %q, want chat.completion.chunk", c.Object)
		}
		if len(c.Choices) != 1 {
			t.Fatalf("choices = %d, want 1 in %q", len(c.Choices), payload)
		}
		chunks = append(chunks, c)
	}
	return chunks, done
}

// feed runs each event through the translator and returns the concatenated SSE
// output.
func feed(t *testing.T, tr *StreamTranslator, events ...string) string {
	t.Helper()
	var sb strings.Builder
	for _, ev := range events {
		out, err := tr.Translate([]byte(ev))
		if err != nil {
			t.Fatalf("Translate(%s): %v", ev, err)
		}
		sb.Write(out)
	}
	return sb.String()
}

// joinContent concatenates the content deltas of a chunk list.
func joinContent(chunks []streamedChunk) string {
	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(c.Choices[0].Delta.Content)
	}
	return sb.String()
}

// toolArgsByIndex reassembles the streamed arguments per OpenAI tool-call index.
func toolArgsByIndex(chunks []streamedChunk) map[int]string {
	args := map[int]string{}
	for _, c := range chunks {
		for _, tc := range c.Choices[0].Delta.ToolCalls {
			args[tc.Index] += tc.Function.Arguments
		}
	}
	return args
}

func TestStreamTranslator_TextStream(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-1", "claude-sonnet-4-5", 1700)
	out := feed(t, tr,
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5-20250929","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"cache_creation_input_tokens":5,"cache_read_input_tokens":20,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"ping"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	)

	chunks, done := parseChunks(t, out)
	if !done {
		t.Fatalf("stream did not end with [DONE]:\n%s", out)
	}
	// message_start (role), two text deltas, terminal. ping,
	// content_block_start(text), content_block_stop and message_delta emit
	// nothing.
	if len(chunks) != 4 {
		t.Fatalf("chunks = %d, want 4:\n%s", len(chunks), out)
	}
	if got := chunks[0].Choices[0].Delta.Role; got != "assistant" {
		t.Errorf("first chunk role = %q, want assistant", got)
	}
	roles := 0
	for _, c := range chunks {
		if c.Choices[0].Delta.Role != "" {
			roles++
		}
		if c.ID != "chatcmpl-1" || c.Model != "claude-sonnet-4-5" || c.Created != 1700 {
			t.Errorf("envelope = %q/%q/%d, want the caller's id/model/created", c.ID, c.Model, c.Created)
		}
	}
	if roles != 1 {
		t.Errorf("role deltas = %d, want 1", roles)
	}
	if got := joinContent(chunks); got != "Hello" {
		t.Errorf("content = %q, want Hello", got)
	}

	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "stop" {
		t.Errorf("terminal finish_reason = %v, want stop", last.Choices[0].FinishReason)
	}
	// Only the terminal chunk carries finish_reason and usage.
	for _, c := range chunks[:len(chunks)-1] {
		if c.Choices[0].FinishReason != nil {
			t.Errorf("non-terminal chunk carries finish_reason %q", *c.Choices[0].FinishReason)
		}
		if c.Usage != nil {
			t.Errorf("non-terminal chunk carries usage %+v", c.Usage)
		}
	}
	// Anthropic's three input counts are disjoint, so prompt_tokens is their
	// sum; output_tokens comes from message_delta, not message_start's 1.
	u := last.Usage
	if u == nil {
		t.Fatal("terminal chunk has no usage")
	}
	if u.PromptTokens != 35 || u.CompletionTokens != 7 || u.TotalTokens != 42 {
		t.Errorf("usage = %d/%d/%d, want 35/7/42", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	}
	if u.CacheReadInputTokens != 20 || u.CacheCreationInputTokens != 5 {
		t.Errorf("cache counts = %d/%d, want 20/5", u.CacheReadInputTokens, u.CacheCreationInputTokens)
	}
	if u.PromptTokensDetails == nil || u.PromptTokensDetails.CachedTokens != 20 {
		t.Errorf("prompt_tokens_details = %+v, want cached_tokens 20", u.PromptTokensDetails)
	}
}

func TestStreamTranslator_ToolCallFragmentedArguments(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-2", "m", 1)
	out := feed(t, tr,
		`{"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"get_weather","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":":\"Oslo\""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`,
	)

	chunks, done := parseChunks(t, out)
	if !done {
		t.Fatalf("stream did not end with [DONE]:\n%s", out)
	}

	// The header fragment carries id/type/name and empty arguments; the
	// fragments that follow carry arguments only.
	header := chunks[1].Choices[0].Delta.ToolCalls
	if len(header) != 1 {
		t.Fatalf("header tool_calls = %d, want 1:\n%s", len(header), out)
	}
	if header[0].ID != "toolu_a" || header[0].Type != "function" || header[0].Function.Name != "get_weather" {
		t.Errorf("header = %+v, want id toolu_a, type function, name get_weather", header[0])
	}
	if header[0].Function.Arguments != "" {
		t.Errorf("header arguments = %q, want empty", header[0].Function.Arguments)
	}
	for _, c := range chunks[2:] {
		for _, tc := range c.Choices[0].Delta.ToolCalls {
			if tc.ID != "" || tc.Type != "" || tc.Function.Name != "" {
				t.Errorf("argument fragment repeats header fields: %+v", tc)
			}
		}
	}

	if got := toolArgsByIndex(chunks)[0]; got != `{"city":"Oslo"}` {
		t.Errorf("reassembled arguments = %q, want {\"city\":\"Oslo\"}", got)
	}
	last := chunks[len(chunks)-1]
	if last.Choices[0].FinishReason == nil || *last.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", last.Choices[0].FinishReason)
	}
}

func TestStreamTranslator_ToolCallIndicesSkipTextBlocks(t *testing.T) {
	// Anthropic block indices count every block (text at 0, tools at 1 and 2);
	// OpenAI tool-call indices count only tool calls, so they must be 0 and 1.
	tr := NewStreamTranslator("chatcmpl-3", "m", 1)
	out := feed(t, tr,
		`{"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"checking"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_a","name":"first","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_b","name":"second","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"b\":2}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		`{"type":"message_stop"}`,
	)

	chunks, _ := parseChunks(t, out)
	if got := joinContent(chunks); got != "checking" {
		t.Errorf("content = %q, want checking", got)
	}

	var headers []streamedToolCall
	for _, c := range chunks {
		for _, tc := range c.Choices[0].Delta.ToolCalls {
			if tc.ID != "" {
				headers = append(headers, tc)
			}
		}
	}
	if len(headers) != 2 {
		t.Fatalf("tool-call headers = %d, want 2:\n%s", len(headers), out)
	}
	if headers[0].Index != 0 || headers[0].Function.Name != "first" {
		t.Errorf("first header = index %d name %q, want 0/first", headers[0].Index, headers[0].Function.Name)
	}
	if headers[1].Index != 1 || headers[1].Function.Name != "second" {
		t.Errorf("second header = index %d name %q, want 1/second", headers[1].Index, headers[1].Function.Name)
	}

	// Each block's arguments land under its own tool-call index, not its
	// Anthropic block index.
	args := toolArgsByIndex(chunks)
	if args[0] != `{"a":1}` || args[1] != `{"b":2}` {
		t.Errorf("arguments by index = %v, want 0:{\"a\":1} 1:{\"b\":2}", args)
	}
	if _, ok := args[2]; ok {
		t.Errorf("arguments emitted under Anthropic block index 2: %v", args)
	}
}

func TestStreamTranslator_ThinkingStream(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-4", "m", 1)
	out := feed(t, tr,
		`{"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"weigh"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"ing"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"abc123"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"answer"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`,
		`{"type":"message_stop"}`,
	)

	chunks, done := parseChunks(t, out)
	if !done {
		t.Fatalf("stream did not end with [DONE]:\n%s", out)
	}
	var reasoning strings.Builder
	for _, c := range chunks {
		reasoning.WriteString(c.Choices[0].Delta.ReasoningContent)
	}
	if reasoning.String() != "weighing" {
		t.Errorf("reasoning_content = %q, want weighing", reasoning.String())
	}
	if got := joinContent(chunks); got != "answer" {
		t.Errorf("content = %q, want answer", got)
	}
	// signature_delta has no chat-completion equivalent and must not leak into
	// either stream.
	if strings.Contains(out, "abc123") {
		t.Errorf("signature_delta leaked into the chunk stream:\n%s", out)
	}
}

func TestStreamTranslator_ErrorEventFailsWithoutFinish(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-5", "m", 1)
	feed(t, tr, `{"type":"message_start","message":{"usage":{"input_tokens":4}}}`)

	_, err := tr.Translate([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"the prompt about kittens is too long"}}`))
	if err == nil {
		t.Fatal("expected an error for an error event")
	}
	if !strings.HasPrefix(err.Error(), "anthropicegress:") {
		t.Errorf("error = %q, want an anthropicegress: prefix", err)
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Errorf("error = %q, want the error type named", err)
	}
	// The upstream error message can echo request content and must not appear.
	if strings.Contains(err.Error(), "kittens") {
		t.Errorf("error message leaked upstream content: %q", err)
	}

	// A failed stream is never closed off as a clean one.
	fin, finErr := tr.Finish()
	if finErr != nil {
		t.Fatalf("Finish after error: %v", finErr)
	}
	if len(fin) != 0 {
		t.Errorf("Finish emitted a terminal chunk after an error event:\n%s", fin)
	}
}

func TestStreamTranslator_FinishIdempotentAfterMessageStop(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-6", "m", 1)
	out := feed(t, tr,
		`{"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	)
	chunks, done := parseChunks(t, out)
	if !done {
		t.Fatalf("message_stop did not emit [DONE]:\n%s", out)
	}
	if r := chunks[len(chunks)-1].Choices[0].FinishReason; r == nil || *r != "length" {
		t.Errorf("finish_reason = %v, want length", r)
	}

	for i := range 2 {
		fin, err := tr.Finish()
		if err != nil {
			t.Fatalf("Finish %d: %v", i, err)
		}
		if len(fin) != 0 {
			t.Errorf("Finish %d emitted a second terminal chunk:\n%s", i, fin)
		}
	}
}

func TestStreamTranslator_FinishWithoutMessageStop(t *testing.T) {
	// An upstream that dies after message_delta still owes the client a
	// terminal chunk; Finish supplies it, carrying the role when no chunk did.
	tr := NewStreamTranslator("chatcmpl-7", "m", 1)
	feed(t, tr, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`)

	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	chunks, done := parseChunks(t, string(fin))
	if !done || len(chunks) != 1 {
		t.Fatalf("Finish = %d chunks, done=%v:\n%s", len(chunks), done, fin)
	}
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Errorf("terminal chunk carries no role: %+v", chunks[0].Choices[0].Delta)
	}
	if r := chunks[0].Choices[0].FinishReason; r == nil || *r != "stop" {
		t.Errorf("finish_reason = %v, want stop", r)
	}
	// output_tokens alone still counts as usage; prompt_tokens is simply 0.
	if chunks[0].Usage == nil || chunks[0].Usage.CompletionTokens != 3 {
		t.Errorf("usage = %+v, want completion_tokens 3", chunks[0].Usage)
	}
}

func TestStreamTranslator_NoUsageReportedOmitsUsage(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-8", "m", 1)
	fin, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	chunks, _ := parseChunks(t, string(fin))
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d, want 1", len(chunks))
	}
	if chunks[0].Usage != nil {
		t.Errorf("usage = %+v, want none when the upstream reported none", chunks[0].Usage)
	}
}

func TestStreamTranslator_EmptyDeltasEmitNothing(t *testing.T) {
	// A delta with nothing in it must not become a chunk carrying an empty
	// delta object, which clients read as a content-free assistant message.
	tr := NewStreamTranslator("chatcmpl-11", "m", 1)
	out := feed(t, tr,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_a","name":"f","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`,
		`{"type":"content_block_delta","index":0}`,
	)
	chunks, _ := parseChunks(t, out)
	if len(chunks) != 1 { // the tool-call header alone
		t.Errorf("chunks = %d, want 1:\n%s", len(chunks), out)
	}
}

func TestStreamTranslator_InvalidEventFails(t *testing.T) {
	tr := NewStreamTranslator("chatcmpl-9", "m", 1)
	if _, err := tr.Translate([]byte(`{not json`)); err == nil {
		t.Fatal("expected an error for a malformed event")
	} else if !strings.HasPrefix(err.Error(), "anthropicegress:") {
		t.Errorf("error = %q, want an anthropicegress: prefix", err)
	}
}

func TestStreamTranslator_DecodeErrorOmitsPayload(t *testing.T) {
	// encoding/json's own messages quote the offending byte and print the
	// offending literal verbatim, and a stream event's fields are model output.
	// The error must describe WHERE the document broke, never what it said.
	cases := []struct {
		name    string
		payload string
		want    string // the sanitized shape the error must take
		secret  string // a fragment of the payload that must not survive
	}{
		{
			name:    "syntax error",
			payload: `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Kohlrabi"`,
			want:    "malformed JSON at byte ",
			secret:  "Kohlrabi",
		},
		{
			name:    "type error",
			payload: `{"type":"content_block_delta","index":8675309.42}`,
			want:    "undecodable JSON (",
			secret:  "8675309.42",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewStreamTranslator("chatcmpl-12", "m", 1).Translate([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.HasPrefix(err.Error(), "anthropicegress: invalid stream event: "+tc.want) {
				t.Errorf("error = %q, want the sanitized %q form", err, tc.want)
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Errorf("error leaked the payload: %q", err)
			}
		})
	}
}

func TestStreamTranslator_AllZeroUsageOmitsUsage(t *testing.T) {
	// A usage object reporting nothing is not usage: emitting 0/0/0 would have
	// the meter record a real completion as free.
	tr := NewStreamTranslator("chatcmpl-13", "m", 1)
	out := feed(t, tr,
		`{"type":"message_start","message":{"usage":{"input_tokens":0,"output_tokens":0}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`,
		`{"type":"message_stop"}`,
	)
	chunks, _ := parseChunks(t, out)
	if u := chunks[len(chunks)-1].Usage; u != nil {
		t.Errorf("usage = %+v, want none when every count is zero", u)
	}
}

func TestStreamTranslator_OrphanToolArgumentsFail(t *testing.T) {
	// Arguments for a block no content_block_start opened cannot be indexed;
	// dropping them would hand the client silently truncated arguments.
	tr := NewStreamTranslator("chatcmpl-10", "m", 1)
	_, err := tr.Translate([]byte(`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`))
	if err == nil {
		t.Fatal("expected an error for arguments on an unopened block")
	}
	if !strings.HasPrefix(err.Error(), "anthropicegress:") {
		t.Errorf("error = %q, want an anthropicegress: prefix", err)
	}
}
