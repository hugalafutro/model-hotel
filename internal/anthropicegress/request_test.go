package anthropicegress

import (
	"encoding/json"
	"strings"
	"testing"
)

// translatedRequest is the Anthropic Messages body as the assertions read it.
type translatedRequest struct {
	Model         string              `json:"model"`
	MaxTokens     int                 `json:"max_tokens"`
	Messages      []translatedMessage `json:"messages"`
	System        string              `json:"system"`
	Stream        bool                `json:"stream"`
	Temperature   *float64            `json:"temperature"`
	TopP          *float64            `json:"top_p"`
	TopK          *int                `json:"top_k"`
	StopSequences []string            `json:"stop_sequences"`
	Tools         []translatedTool    `json:"tools"`
	ToolChoice    *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tool_choice"`
	Thinking *struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	} `json:"thinking"`
}

type translatedMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type translatedTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type translatedBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	} `json:"source"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   string          `json:"content"`
}

// translate runs TranslateRequest and decodes the result for assertions.
func translate(t *testing.T, body string) translatedRequest {
	t.Helper()
	out, _, _, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	var req translatedRequest
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("translated body is not valid JSON: %v", err)
	}
	return req
}

// blocksOf decodes one message's content as a block array.
func blocksOf(t *testing.T, m translatedMessage) []translatedBlock {
	t.Helper()
	var blocks []translatedBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		t.Fatalf("message content is not a block array: %v", err)
	}
	return blocks
}

func TestTranslateRequest_PassesThroughModelAndStream(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	out, model, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	if model != "claude-sonnet-4-5" {
		t.Errorf("model = %q, want claude-sonnet-4-5", model)
	}
	if !stream {
		t.Error("stream = false, want true")
	}

	var req translatedRequest
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatalf("translated body is not valid JSON: %v", err)
	}
	if req.Model != "claude-sonnet-4-5" {
		t.Errorf("body model = %q, want claude-sonnet-4-5", req.Model)
	}
	if !req.Stream {
		t.Error("body stream = false, want true")
	}
}

func TestTranslateRequest_StringContentStaysAString(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"user","content":"hello"},
		{"role":"assistant","content":"hi"}]}`)

	if len(req.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(req.Messages))
	}
	if got := string(req.Messages[0].Content); got != `"hello"` {
		t.Errorf("user content = %s, want a plain JSON string", got)
	}
	if req.Messages[1].Role != "assistant" || string(req.Messages[1].Content) != `"hi"` {
		t.Errorf("assistant message = %+v, want a plain-string assistant turn", req.Messages[1])
	}
}

func TestTranslateRequest_SystemAndDeveloperLiftToSystem(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"system","content":"Be terse."},
		{"role":"developer","content":[{"type":"text","text":"Cite sources."}]},
		{"role":"user","content":"hello"}]}`)

	if req.System != "Be terse.\n\nCite sources." {
		t.Errorf("system = %q, want the two prompts joined by a blank line", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want only the user turn", req.Messages)
	}
}

func TestTranslateRequest_EmptySystemMessageIsDropped(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"system","content":""},
		{"role":"system","content":"Be terse."},
		{"role":"user","content":"hello"}]}`)

	if req.System != "Be terse." {
		t.Errorf("system = %q, want only the non-empty prompt", req.System)
	}
}

func TestTranslateRequest_ContentParts(t *testing.T) {
	// "hello world" base64-encoded, as a text/plain file payload arrives.
	req := translate(t, `{"model":"m","messages":[{"role":"user","content":[
		{"type":"text","text":"look"},
		{"type":"text","text":""},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0="}},
		{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}},
		{"type":"image_url","image_url":{"url":"data:application/pdf;base64,JVBERi0="}},
		{"type":"file","file":{"file_data":"data:application/pdf;base64,SGVsbG8="}},
		{"type":"file","file":{"file_data":"data:text/plain;base64,aGVsbG8gd29ybGQ="}},
		{"type":"file","file":{"file_url":"https://example.com/report.pdf"}},
		{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]}]}`)

	if len(req.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(req.Messages))
	}
	blocks := blocksOf(t, req.Messages[0])
	if len(blocks) != 7 {
		t.Fatalf("got %d blocks, want 7 (empty text and the unknown part are dropped): %+v", len(blocks), blocks)
	}

	if blocks[0].Type != "text" || blocks[0].Text != "look" {
		t.Errorf("block 0 = %+v, want a text block", blocks[0])
	}
	if blocks[1].Type != "image" || blocks[1].Source.Type != "base64" ||
		blocks[1].Source.MediaType != "image/png" || blocks[1].Source.Data != "iVBORw0=" {
		t.Errorf("block 1 = %+v, want a base64 image source", blocks[1])
	}
	if blocks[2].Type != "image" || blocks[2].Source.Type != "url" ||
		blocks[2].Source.URL != "https://example.com/cat.png" {
		t.Errorf("block 2 = %+v, want a url image source", blocks[2])
	}
	if blocks[3].Type != "document" || blocks[3].Source.Type != "base64" ||
		blocks[3].Source.MediaType != "application/pdf" || blocks[3].Source.Data != "JVBERi0=" {
		t.Errorf("block 3 = %+v, want the image-slot PDF as a base64 document", blocks[3])
	}
	if blocks[4].Type != "document" || blocks[4].Source.Type != "base64" ||
		blocks[4].Source.MediaType != "application/pdf" || blocks[4].Source.Data != "SGVsbG8=" {
		t.Errorf("block 4 = %+v, want a base64 document source", blocks[4])
	}
	if blocks[5].Type != "document" || blocks[5].Source.Type != "text" ||
		blocks[5].Source.MediaType != "text/plain" || blocks[5].Source.Data != "hello world" {
		t.Errorf("block 5 = %+v, want a text document source carrying the decoded text", blocks[5])
	}
	if blocks[6].Type != "document" || blocks[6].Source.Type != "url" ||
		blocks[6].Source.URL != "https://example.com/report.pdf" {
		t.Errorf("block 6 = %+v, want a url document source", blocks[6])
	}
}

func TestTranslateRequest_UnusableFileAndImagePartsAreDropped(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[{"role":"user","content":[
		{"type":"text","text":"look"},
		{"type":"image_url","image_url":{"url":""}},
		{"type":"file"},
		{"type":"file","file":{}},
		{"type":"file","file":{"file_data":"JVBERi0="}},
		{"type":"file","file":{"file_data":"data:text/plain;base64,!!not-base64!!"}}]}]}`)

	blocks := blocksOf(t, req.Messages[0])
	if len(blocks) != 1 || blocks[0].Type != "text" {
		t.Errorf("blocks = %+v, want only the text block", blocks)
	}
}

func TestTranslateRequest_PlainTextDataURIIsPercentDecoded(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[{"role":"user","content":[
		{"type":"file","file":{"file_data":"data:text/plain,hello%20world"}}]}]}`)

	blocks := blocksOf(t, req.Messages[0])
	if len(blocks) != 1 || blocks[0].Source.Type != "text" || blocks[0].Source.Data != "hello world" {
		t.Errorf("blocks = %+v, want one text document source carrying the decoded text", blocks)
	}
}

func TestTranslateRequest_ToolCallArgumentsBecomeAnObject(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":"checking","tool_calls":[
			{"id":"call_1","function":{"name":"get_weather","arguments":"{\"city\":\"Oslo\"}"}},
			{"id":"call_2","function":{"name":"get_time","arguments":""}},
			{"id":"call_3","function":{"name":"get_news","arguments":"{not json"}},
			{"id":"call_4","function":{"name":"get_stocks","arguments":"[1,2]"}}]}]}`)

	if len(req.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(req.Messages))
	}
	blocks := blocksOf(t, req.Messages[1])
	if len(blocks) != 5 {
		t.Fatalf("got %d blocks, want a text block plus four tool_use blocks: %+v", len(blocks), blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "checking" {
		t.Errorf("block 0 = %+v, want the assistant text alongside the calls", blocks[0])
	}

	wantInputs := []string{`{"city":"Oslo"}`, `{}`, `{}`, `{}`}
	wantNames := []string{"get_weather", "get_time", "get_news", "get_stocks"}
	for i, b := range blocks[1:] {
		if b.Type != "tool_use" {
			t.Errorf("block %d type = %q, want tool_use", i+1, b.Type)
		}
		if b.Name != wantNames[i] {
			t.Errorf("block %d name = %q, want %q", i+1, b.Name, wantNames[i])
		}
		if string(b.Input) != wantInputs[i] {
			t.Errorf("block %d input = %s, want %s", i+1, b.Input, wantInputs[i])
		}
	}
	if blocks[1].ID != "call_1" {
		t.Errorf("tool_use id = %q, want call_1", blocks[1].ID)
	}
}

func TestTranslateRequest_ToolCallOnlyAssistantTurn(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":null,"tool_calls":[
			{"id":"call_1","function":{"name":"get_weather","arguments":"{}"}}]}]}`)

	blocks := blocksOf(t, req.Messages[1])
	if len(blocks) != 1 || blocks[0].Type != "tool_use" {
		t.Errorf("blocks = %+v, want a single tool_use block", blocks)
	}
}

func TestTranslateRequest_ConsecutiveToolResultsCoalesce(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"user","content":"weather?"},
		{"role":"assistant","content":null,"tool_calls":[
			{"id":"call_1","function":{"name":"get_weather","arguments":"{}"}},
			{"id":"call_2","function":{"name":"get_time","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"call_1","content":"sunny"},
		{"role":"tool","tool_call_id":"call_2","content":"12:00"},
		{"role":"assistant","content":"Sunny at noon."},
		{"role":"tool","tool_call_id":"call_3","content":"late"}]}`)

	if len(req.Messages) != 5 {
		t.Fatalf("got %d messages, want 5: %+v", len(req.Messages), req.Messages)
	}
	roles := []string{"user", "assistant", "user", "assistant", "user"}
	for i, want := range roles {
		if req.Messages[i].Role != want {
			t.Errorf("message %d role = %q, want %q", i, req.Messages[i].Role, want)
		}
	}

	results := blocksOf(t, req.Messages[2])
	if len(results) != 2 {
		t.Fatalf("got %d tool_result blocks in the coalesced turn, want 2: %+v", len(results), results)
	}
	for i, want := range []struct{ id, content string }{{"call_1", "sunny"}, {"call_2", "12:00"}} {
		if results[i].Type != "tool_result" || results[i].ToolUseID != want.id || results[i].Content != want.content {
			t.Errorf("tool_result %d = %+v, want %+v", i, results[i], want)
		}
	}

	// The run breaks on the assistant turn: the later tool message opens a new one.
	tail := blocksOf(t, req.Messages[4])
	if len(tail) != 1 || tail[0].ToolUseID != "call_3" {
		t.Errorf("trailing tool turn = %+v, want a single call_3 result", tail)
	}
}

func TestTranslateRequest_ToolResultContentPartsFlatten(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"user","content":"weather?"},
		{"role":"tool","tool_call_id":"call_1","content":[
			{"type":"text","text":"sun"},
			{"type":"text","text":"ny"}]}]}`)

	results := blocksOf(t, req.Messages[1])
	if len(results) != 1 || results[0].Content != "sunny" {
		t.Errorf("tool_result = %+v, want the parts flattened to text", results)
	}
}

func TestTranslateRequest_ToolsGetAnInputSchema(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[{"role":"user","content":"hi"}],
		"tools":[
			{"type":"function","function":{"name":"get_weather","description":"Weather.",
				"parameters":{"type":"object","properties":{"city":{"type":"string"}}}}},
			{"type":"function","function":{"name":"ping"}}]}`)

	if len(req.Tools) != 2 {
		t.Fatalf("got %d tools, want 2", len(req.Tools))
	}
	if req.Tools[0].Name != "get_weather" || req.Tools[0].Description != "Weather." {
		t.Errorf("tool 0 = %+v, want name and description carried over", req.Tools[0])
	}
	if !strings.Contains(string(req.Tools[0].InputSchema), `"city"`) {
		t.Errorf("tool 0 input_schema = %s, want the parameters verbatim", req.Tools[0].InputSchema)
	}
	if string(req.Tools[1].InputSchema) != `{"type":"object","properties":{}}` {
		t.Errorf("tool 1 input_schema = %s, want an empty object schema", req.Tools[1].InputSchema)
	}
}

func TestTranslateRequest_ToolChoice(t *testing.T) {
	tests := []struct {
		name       string
		toolChoice string
		wantType   string
		wantName   string
		wantTools  bool
	}{
		{name: "absent", toolChoice: "", wantType: "", wantTools: true},
		{name: "auto", toolChoice: `"auto"`, wantType: "auto", wantTools: true},
		{name: "required", toolChoice: `"required"`, wantType: "any", wantTools: true},
		{name: "none drops the tools too", toolChoice: `"none"`, wantType: "", wantTools: false},
		{
			name:       "named function",
			toolChoice: `{"type":"function","function":{"name":"get_weather"}}`,
			wantType:   "tool",
			wantName:   "get_weather",
			wantTools:  true,
		},
		{name: "unrecognised string", toolChoice: `"banana"`, wantType: "", wantTools: true},
		{name: "object with no function name", toolChoice: `{"type":"function"}`, wantType: "", wantTools: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"model":"m","messages":[{"role":"user","content":"hi"}],
				"tools":[{"type":"function","function":{"name":"get_weather"}}]`
			if tt.toolChoice != "" {
				body += `,"tool_choice":` + tt.toolChoice
			}
			req := translate(t, body+"}")

			switch {
			case tt.wantType == "":
				if req.ToolChoice != nil {
					t.Errorf("tool_choice = %+v, want it omitted", req.ToolChoice)
				}
			case req.ToolChoice == nil:
				t.Fatalf("tool_choice omitted, want type %q", tt.wantType)
			case req.ToolChoice.Type != tt.wantType || req.ToolChoice.Name != tt.wantName:
				t.Errorf("tool_choice = %+v, want type %q name %q", req.ToolChoice, tt.wantType, tt.wantName)
			}

			if gotTools := len(req.Tools) > 0; gotTools != tt.wantTools {
				t.Errorf("tools present = %v, want %v", gotTools, tt.wantTools)
			}
		})
	}
}

func TestTranslateRequest_MaxTokens(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "default when neither field is set",
			body: `{"model":"m","messages":[{"role":"user","content":"hi"}]}`,
			want: DefaultMaxTokens,
		},
		{
			name: "max_tokens",
			body: `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":100}`,
			want: 100,
		},
		{
			name: "max_completion_tokens wins",
			body: `{"model":"m","messages":[{"role":"user","content":"hi"}],"max_tokens":100,"max_completion_tokens":200}`,
			want: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translate(t, tt.body).MaxTokens; got != tt.want {
				t.Errorf("max_tokens = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTranslateRequest_SamplingAndStop(t *testing.T) {
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}],
		"temperature":0.5,"top_p":0.9,"top_k":40,"stop":"END",
		"frequency_penalty":1,"presence_penalty":1,"n":2,"seed":7,
		"response_format":{"type":"json_object"},"logprobs":true,"stream_options":{"include_usage":true}}`
	req := translate(t, body)

	if req.Temperature == nil || *req.Temperature != 0.5 {
		t.Errorf("temperature = %v, want 0.5", req.Temperature)
	}
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("top_p = %v, want 0.9", req.TopP)
	}
	if req.TopK == nil || *req.TopK != 40 {
		t.Errorf("top_k = %v, want 40", req.TopK)
	}
	if len(req.StopSequences) != 1 || req.StopSequences[0] != "END" {
		t.Errorf("stop_sequences = %v, want [END]", req.StopSequences)
	}

	// Params with no Anthropic equivalent must not survive the translation.
	out, _, _, err := TranslateRequest([]byte(body))
	if err != nil {
		t.Fatalf("TranslateRequest failed: %v", err)
	}
	for _, dropped := range []string{"frequency_penalty", "presence_penalty", `"n"`, "seed", "logprobs", "response_format", "stream_options"} {
		if strings.Contains(string(out), dropped) {
			t.Errorf("translated body still carries %s: %s", dropped, out)
		}
	}
}

func TestTranslateRequest_Stop(t *testing.T) {
	tests := []struct {
		name string
		stop string
		want []string
	}{
		{name: "array", stop: `["A","B"]`, want: []string{"A", "B"}},
		{name: "string", stop: `"END"`, want: []string{"END"}},
		{name: "empty string", stop: `""`, want: nil},
		{name: "null", stop: `null`, want: nil},
		{name: "unusable shape", stop: `{"a":1}`, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := translate(t, `{"model":"m","messages":[{"role":"user","content":"hi"}],"stop":`+tt.stop+`}`)

			if len(req.StopSequences) != len(tt.want) {
				t.Fatalf("stop_sequences = %v, want %v", req.StopSequences, tt.want)
			}
			for i, want := range tt.want {
				if req.StopSequences[i] != want {
					t.Errorf("stop_sequences[%d] = %q, want %q", i, req.StopSequences[i], want)
				}
			}
		})
	}
}

func TestTranslateRequest_EmptyTurnsAreDropped(t *testing.T) {
	req := translate(t, `{"model":"m","messages":[
		{"role":"user","content":"hi"},
		{"role":"assistant","content":""},
		{"role":"assistant","content":[{"type":"text","text":""}]},
		{"role":"user","content":null},
		{"role":"user","content":"still here"}]}`)

	if len(req.Messages) != 2 {
		t.Fatalf("got %d messages, want the two non-empty turns: %+v", len(req.Messages), req.Messages)
	}
	if string(req.Messages[1].Content) != `"still here"` {
		t.Errorf("second message content = %s, want the trailing user turn", req.Messages[1].Content)
	}
}

func TestTranslateRequest_Thinking(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		wantBudget int // 0 means no thinking block
	}{
		{name: "low", effort: "low", wantBudget: 1024},
		{name: "medium", effort: "medium", wantBudget: 4096},
		{name: "high", effort: "high", wantBudget: 8192},
		{name: "none", effort: "none", wantBudget: 0},
		{name: "absent", effort: "", wantBudget: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"model":"m","messages":[{"role":"user","content":"hi"}],
				"max_tokens":16000,"temperature":0.5,"top_p":0.9,"top_k":40`
			if tt.effort != "" {
				body += `,"reasoning_effort":"` + tt.effort + `"`
			}
			req := translate(t, body+"}")

			if tt.wantBudget == 0 {
				if req.Thinking != nil {
					t.Errorf("thinking = %+v, want it omitted", req.Thinking)
				}
				if req.Temperature == nil || req.TopP == nil || req.TopK == nil {
					t.Error("sampling params dropped without a thinking block")
				}
				return
			}

			if req.Thinking == nil {
				t.Fatalf("thinking omitted, want budget %d", tt.wantBudget)
			}
			if req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != tt.wantBudget {
				t.Errorf("thinking = %+v, want enabled with budget %d", req.Thinking, tt.wantBudget)
			}
			// Anthropic rejects sampling params alongside thinking.
			if req.Temperature != nil || req.TopP != nil || req.TopK != nil {
				t.Errorf("sampling params survived thinking: temperature=%v top_p=%v top_k=%v",
					req.Temperature, req.TopP, req.TopK)
			}
		})
	}
}

func TestTranslateRequest_ThinkingRaisesMaxTokensAboveTheBudget(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens string
		want      int
	}{
		{name: "default is below the budget", maxTokens: "", want: 8192 + DefaultMaxTokens},
		{name: "equal to the budget", maxTokens: `,"max_tokens":8192`, want: 8192 + DefaultMaxTokens},
		{name: "already above the budget", maxTokens: `,"max_tokens":9000`, want: 9000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := translate(t, `{"model":"m","messages":[{"role":"user","content":"hi"}],
				"reasoning_effort":"high"`+tt.maxTokens+`}`)

			if req.MaxTokens != tt.want {
				t.Errorf("max_tokens = %d, want %d", req.MaxTokens, tt.want)
			}
			if req.MaxTokens <= req.Thinking.BudgetTokens {
				t.Errorf("max_tokens %d is not above the thinking budget %d", req.MaxTokens, req.Thinking.BudgetTokens)
			}
		})
	}
}

func TestTranslateRequest_Errors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `{"model":`},
		{name: "no model", body: `{"messages":[{"role":"user","content":"hi"}]}`},
		{name: "no usable messages", body: `{"model":"m","messages":[{"role":"system","content":"Be terse."}]}`},
		{name: "invalid message content", body: `{"model":"m","messages":[{"role":"user","content":[1,2,3]}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := TranslateRequest([]byte(tt.body))
			if err == nil {
				t.Fatal("TranslateRequest succeeded, want an error")
			}
			if !strings.HasPrefix(err.Error(), "anthropicegress: ") {
				t.Errorf("error = %q, want an anthropicegress: prefix", err)
			}
		})
	}
}

func TestTranslateRequest_ErrorsCarryNoRequestContent(t *testing.T) {
	_, _, _, err := TranslateRequest([]byte(`{"model":"m","messages":[
		{"role":"user","content":["sensitive prompt text"]}]}`))
	if err == nil {
		t.Fatal("TranslateRequest succeeded, want an error")
	}
	if strings.Contains(err.Error(), "sensitive prompt text") {
		t.Errorf("error leaks request content: %q", err)
	}
}
