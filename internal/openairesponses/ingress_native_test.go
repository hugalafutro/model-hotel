package openairesponses

import "testing"

func TestParseResponseUsage(t *testing.T) {
	u := ParseResponseUsage([]byte(`{"id":"resp_1","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":60},"output_tokens":"7","total_tokens":107}}`))
	if u != (NativeUsage{PromptTokens: 100, CompletionTokens: 7, CacheHitTokens: 60, CacheMissTokens: 40}) {
		t.Errorf("usage = %+v", u)
	}
	if u := ParseResponseUsage([]byte(`{"usage":{"input_tokens":10,"output_tokens":1}}`)); u.CacheHitTokens != 0 || u.CacheMissTokens != 0 || u.PromptTokens != 10 {
		t.Errorf("uncached must report no split: %+v", u)
	}
	// A cached figure above the prompt is nonsense, not a split.
	if u := ParseResponseUsage([]byte(`{"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":50},"output_tokens":1}}`)); u.CacheHitTokens != 0 {
		t.Errorf("absurd cache split must be dropped: %+v", u)
	}
	if u := ParseResponseUsage([]byte(`{"usage":null}`)); u != (NativeUsage{}) {
		t.Errorf("null usage = %+v", u)
	}
	if u := ParseResponseUsage([]byte(`nope`)); u != (NativeUsage{}) {
		t.Errorf("broken body = %+v", u)
	}
}

func TestResponseCarriesContentStatusTextBytes(t *testing.T) {
	body := []byte(`{"id":"resp_1","status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"abc"}]},{"type":"message","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","name":"ls","arguments":"{\"p\":1}"}]}`)
	if !ResponseCarriesContent(body) || ResponseStatus(body) != "completed" {
		t.Error("content and status")
	}
	if n := ResponseTextBytes(body); n != 3+5+2+7 {
		t.Errorf("text bytes = %d", n)
	}
	if ResponseStatus([]byte(`{"id":"resp_1","status":"failed","output":[]}`)) != "" {
		t.Error("a failed status is not an answer")
	}
	empty := []byte(`{"id":"resp_1","status":"incomplete","output":[]}`)
	if ResponseCarriesContent(empty) || ResponseStatus(empty) != "incomplete" || ResponseTextBytes(empty) != 0 {
		t.Error("empty output")
	}
	if ResponseCarriesContent([]byte(`x`)) || ResponseStatus([]byte(`x`)) != "" || ResponseTextBytes([]byte(`x`)) != 0 {
		t.Error("broken body")
	}
}

func TestInspectStreamEvent(t *testing.T) {
	ev := InspectStreamEvent([]byte(`{"type":"response.completed","sequence_number":9,"response":{"id":"resp_1","status":"completed","usage":{"input_tokens":20,"input_tokens_details":{"cached_tokens":5},"output_tokens":3}}}`))
	if !ev.Terminal || !ev.HasInput || ev.InputTokens != 20 || ev.CacheHitTokens != 5 || ev.CacheMissTokens != 15 || !ev.HasOutput || ev.OutputTokens != 3 || ev.ErrorMessage != "" || ev.CarriesError {
		t.Errorf("completed = %+v", ev)
	}
	if !ev.HasSequence || ev.SequenceNumber != 9 || ev.ResponseID != "resp_1" {
		t.Errorf("sequence/id = %+v", ev)
	}
	if ev := InspectStreamEvent([]byte(`{"type":"response.output_text.delta","delta":"x"}`)); ev.HasSequence || ev.ResponseID != "" {
		t.Errorf("no sequence/id = %+v", ev)
	}
	ev = InspectStreamEvent([]byte(`{"type":"response.incomplete","response":{"usage":{"input_tokens":0,"output_tokens":0}}}`))
	if !ev.Terminal || ev.HasInput || ev.HasOutput {
		t.Errorf("zero counts are not readings: %+v", ev)
	}
	ev = InspectStreamEvent([]byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"boom"}}}`))
	if !ev.Terminal || ev.ErrorMessage != "boom" || !ev.CarriesError {
		t.Errorf("failed = %+v", ev)
	}
	if ev := InspectStreamEvent([]byte(`{"type":"response.failed","response":{"status":"failed"}}`)); !ev.Terminal || ev.ErrorMessage == "" || !ev.CarriesError {
		t.Errorf("failed without an error member still fails: %+v", ev)
	}
	ev = InspectStreamEvent([]byte(`{"type":"error","code":"rate_limit","message":"slow down"}`))
	if ev.Terminal || ev.ErrorMessage != "slow down" || !ev.CarriesError {
		t.Errorf("error = %+v", ev)
	}
	if ev := InspectStreamEvent([]byte(`{"type":"error"}`)); ev.ErrorMessage == "" {
		t.Error("an error event without text still carries an error")
	}
	if ev := InspectStreamEvent([]byte(`{"type":"response.output_text.delta","delta":"héllo"}`)); ev.TextBytes != 6 || ev.Terminal {
		t.Errorf("delta = %+v", ev)
	}
	if ev := InspectStreamEvent([]byte(`{"type":"response.function_call_arguments.delta","delta":"{}"}`)); ev.TextBytes != 2 {
		t.Errorf("args delta = %+v", ev)
	}
	if ev := InspectStreamEvent([]byte(`{"type":"response.output_item.added","error":{"message":"stamped"}}`)); !ev.CarriesError || ev.ErrorMessage != "" {
		t.Errorf("stamped error member = %+v", ev)
	}
	if ev := InspectStreamEvent([]byte(`garbage`)); ev.Type != "" {
		t.Errorf("garbage = %+v", ev)
	}
}
