package util

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// ParseBearerToken
// ---------------------------------------------------------------------------

func TestParseBearerToken_Valid(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer my-secret-token")
	token, ok := ParseBearerToken(r)
	if !ok {
		t.Fatal("ParseBearerToken should return true for valid Bearer header")
	}
	if token != "my-secret-token" {
		t.Errorf("expected token %q, got %q", "my-secret-token", token)
	}
}

func TestParseBearerToken_ValidWithSkPrefix(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer sk-abc123def456")
	token, ok := ParseBearerToken(r)
	if !ok {
		t.Fatal("ParseBearerToken should return true for sk- prefixed token")
	}
	if token != "sk-abc123def456" {
		t.Errorf("expected token %q, got %q", "sk-abc123def456", token)
	}
}

func TestParseBearerToken_MissingHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false when Authorization header is missing")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_EmptyHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for empty Authorization header")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_WrongScheme(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for non-Bearer scheme")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_BearerWithoutSpace(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for 'Bearer' without space")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_BearerWithEmptyToken(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer ")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for 'Bearer ' with empty token")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_LowercaseBearer(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "bearer my-token")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should be case-sensitive and reject lowercase 'bearer'")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_TokenWithSpaces(t *testing.T) {
	// Tokens with spaces after the first word are technically part of the token value
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer token with spaces")
	token, ok := ParseBearerToken(r)
	if !ok {
		t.Fatal("ParseBearerToken should return true even if token value contains spaces")
	}
	if token != "token with spaces" {
		t.Errorf("expected %q, got %q", "token with spaces", token)
	}
}

func TestParseBearerToken_ExactSevenChars(t *testing.T) {
	// 7-char auth header is too short for "Bearer " (7 chars + at least 1 token char)
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Basica ")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false for 7-char non-Bearer header")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_TabAfterBearer(t *testing.T) {
	// "Bearer\t" uses a tab instead of a space — should not match
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer\ttoken")
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should reject 'Bearer\\t' (tab instead of space)")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

// ---------------------------------------------------------------------------
// GetIntQueryParam
// ---------------------------------------------------------------------------

func TestGetIntQueryParam_Present(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=5", http.NoBody)
	result := GetIntQueryParam(r, "page", 0)
	if result != 5 {
		t.Errorf("expected 5, got %d", result)
	}
}

func TestGetIntQueryParam_Absent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	result := GetIntQueryParam(r, "page", 1)
	if result != 1 {
		t.Errorf("expected default 1, got %d", result)
	}
}

func TestGetIntQueryParam_InvalidValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=abc", http.NoBody)
	result := GetIntQueryParam(r, "page", 42)
	if result != 42 {
		t.Errorf("expected default 42 for unparseable value, got %d", result)
	}
}

func TestGetIntQueryParam_NegativeValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?offset=-10", http.NoBody)
	result := GetIntQueryParam(r, "offset", 0)
	if result != -10 {
		t.Errorf("expected -10, got %d", result)
	}
}

func TestGetIntQueryParam_ZeroValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?limit=0", http.NoBody)
	result := GetIntQueryParam(r, "limit", 100)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestGetIntQueryParam_EmptyValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=", http.NoBody)
	result := GetIntQueryParam(r, "page", 1)
	if result != 1 {
		t.Errorf("expected default 1 for empty query value, got %d", result)
	}
}

func TestGetIntQueryParam_MultipleParams(t *testing.T) {
	r := httptest.NewRequest("GET", "/?a=1&b=2", http.NoBody)
	resultA := GetIntQueryParam(r, "a", 0)
	resultB := GetIntQueryParam(r, "b", 0)
	if resultA != 1 {
		t.Errorf("expected a=1, got %d", resultA)
	}
	if resultB != 2 {
		t.Errorf("expected b=2, got %d", resultB)
	}
}

func TestGetIntQueryParam_LargeValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?n=9999999", http.NoBody)
	result := GetIntQueryParam(r, "n", 0)
	if result != 9999999 {
		t.Errorf("expected 9999999, got %d", result)
	}
}

// ---------------------------------------------------------------------------
// SanitizeBaseURL
// ---------------------------------------------------------------------------

func TestSanitizeBaseURL_TrailingSlash(t *testing.T) {
	result := SanitizeBaseURL("https://api.openai.com/")
	if result != "https://api.openai.com" {
		t.Errorf("expected %q, got %q", "https://api.openai.com", result)
	}
}

func TestSanitizeBaseURL_NoTrailingSlash(t *testing.T) {
	input := "https://api.openai.com"
	result := SanitizeBaseURL(input)
	if result != input {
		t.Errorf("expected %q, got %q", input, result)
	}
}

func TestSanitizeBaseURL_MultipleTrailingSlashes(t *testing.T) {
	// Only the final slash should be trimmed
	result := SanitizeBaseURL("https://api.openai.com/v1/")
	if result != "https://api.openai.com/v1" {
		t.Errorf("expected %q, got %q", "https://api.openai.com/v1", result)
	}
}

func TestSanitizeBaseURL_PathWithoutTrailingSlash(t *testing.T) {
	input := "https://api.openai.com/v1/chat"
	result := SanitizeBaseURL(input)
	if result != input {
		t.Errorf("expected %q, got %q", input, result)
	}
}

func TestSanitizeBaseURL_EmptyString(t *testing.T) {
	result := SanitizeBaseURL("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSanitizeBaseURL_JustSlash(t *testing.T) {
	result := SanitizeBaseURL("/")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// SplitAndTrim
// ---------------------------------------------------------------------------

func TestSplitAndTrim_Simple(t *testing.T) {
	result := SplitAndTrim("a,b,c")
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	expected := []string{"a", "b", "c"}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("element %d: expected %q, got %q", i, v, result[i])
		}
	}
}

func TestSplitAndTrim_WithSpaces(t *testing.T) {
	result := SplitAndTrim("  a , b , c  ")
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	expected := []string{"a", "b", "c"}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("element %d: expected %q, got %q", i, v, result[i])
		}
	}
}

func TestSplitAndTrim_EmptyString(t *testing.T) {
	result := SplitAndTrim("")
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestSplitAndTrim_SingleValue(t *testing.T) {
	result := SplitAndTrim("hello")
	if len(result) != 1 || result[0] != "hello" {
		t.Errorf("expected [\"hello\"], got %v", result)
	}
}

func TestSplitAndTrim_EmptyElements(t *testing.T) {
	result := SplitAndTrim("a,,b, ,c")
	if len(result) != 3 {
		t.Fatalf("expected 3 non-empty elements, got %d: %v", len(result), result)
	}
	expected := []string{"a", "b", "c"}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("element %d: expected %q, got %q", i, v, result[i])
		}
	}
}

func TestSplitAndTrim_OnlyCommas(t *testing.T) {
	result := SplitAndTrim(",,,")
	if result != nil {
		t.Errorf("expected nil for input of only commas, got %v", result)
	}
}

func TestSplitAndTrim_OnlySpaces(t *testing.T) {
	result := SplitAndTrim("   ")
	if result != nil {
		t.Errorf("expected nil for input of only spaces, got %v", result)
	}
}

func TestSplitAndTrim_URLs(t *testing.T) {
	result := SplitAndTrim("http://localhost:5173, http://localhost:8081")
	if len(result) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(result))
	}
	if result[0] != "http://localhost:5173" {
		t.Errorf("element 0: expected %q, got %q", "http://localhost:5173", result[0])
	}
	if result[1] != "http://localhost:8081" {
		t.Errorf("element 1: expected %q, got %q", "http://localhost:8081", result[1])
	}
}

func TestSplitAndTrim_TrailingComma(t *testing.T) {
	result := SplitAndTrim("a,b,")
	if len(result) != 2 {
		t.Fatalf("expected 2 elements (trailing comma ignored), got %d: %v", len(result), result)
	}
	if result[0] != "a" || result[1] != "b" {
		t.Errorf("expected [\"a\",\"b\"], got %v", result)
	}
}

func TestSplitAndTrim_LeadingComma(t *testing.T) {
	result := SplitAndTrim(",a,b")
	if len(result) != 2 {
		t.Fatalf("expected 2 elements (leading comma ignored), got %d: %v", len(result), result)
	}
	if result[0] != "a" || result[1] != "b" {
		t.Errorf("expected [\"a\",\"b\"], got %v", result)
	}
}

// ---------------------------------------------------------------------------
// OpenAIErrorType
// ---------------------------------------------------------------------------

func TestOpenAIErrorType_401(t *testing.T) {
	result := OpenAIErrorType(401)
	if result != "authentication_error" {
		t.Errorf("OpenAIErrorType(401) = %q, want %q", result, "authentication_error")
	}
}

func TestOpenAIErrorType_403(t *testing.T) {
	result := OpenAIErrorType(403)
	if result != "permission_error" {
		t.Errorf("OpenAIErrorType(403) = %q, want %q", result, "permission_error")
	}
}

func TestOpenAIErrorType_404(t *testing.T) {
	result := OpenAIErrorType(404)
	if result != "not_found_error" {
		t.Errorf("OpenAIErrorType(404) = %q, want %q", result, "not_found_error")
	}
}

func TestOpenAIErrorType_429(t *testing.T) {
	result := OpenAIErrorType(429)
	if result != "rate_limit_error" {
		t.Errorf("OpenAIErrorType(429) = %q, want %q", result, "rate_limit_error")
	}
}

func TestOpenAIErrorType_500(t *testing.T) {
	result := OpenAIErrorType(500)
	if result != "server_error" {
		t.Errorf("OpenAIErrorType(500) = %q, want %q", result, "server_error")
	}
}

func TestOpenAIErrorType_502(t *testing.T) {
	result := OpenAIErrorType(502)
	if result != "server_error" {
		t.Errorf("OpenAIErrorType(502) = %q, want %q", result, "server_error")
	}
}

func TestOpenAIErrorType_503(t *testing.T) {
	result := OpenAIErrorType(503)
	if result != "server_error" {
		t.Errorf("OpenAIErrorType(503) = %q, want %q", result, "server_error")
	}
}

func TestOpenAIErrorType_400(t *testing.T) {
	result := OpenAIErrorType(400)
	if result != "invalid_request_error" {
		t.Errorf("OpenAIErrorType(400) = %q, want %q", result, "invalid_request_error")
	}
}

func TestOpenAIErrorType_200(t *testing.T) {
	result := OpenAIErrorType(200)
	if result != "invalid_request_error" {
		t.Errorf("OpenAIErrorType(200) = %q, want %q", result, "invalid_request_error")
	}
}

func TestOpenAIErrorType_0(t *testing.T) {
	result := OpenAIErrorType(0)
	if result != "invalid_request_error" {
		t.Errorf("OpenAIErrorType(0) = %q, want %q", result, "invalid_request_error")
	}
}

// IntToStr
// ---------------------------------------------------------------------------

func TestIntToStr(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-1, "-1"},
		{999999, "999999"},
		{-12345, "-12345"},
	}
	for _, tc := range tests {
		result := IntToStr(tc.input)
		if result != tc.expected {
			t.Errorf("IntToStr(%d) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// ParseInt
// ---------------------------------------------------------------------------

func TestParseInt_Valid(t *testing.T) {
	result, err := ParseInt("12345")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != 12345 {
		t.Errorf("expected 12345, got %d", result)
	}
}

func TestParseInt_Zero(t *testing.T) {
	result, err := ParseInt("0")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestParseInt_EmptyString(t *testing.T) {
	result, err := ParseInt("")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != 0 {
		t.Errorf("expected 0 for empty string, got %d", result)
	}
}

func TestParseInt_TrailingNonDigits(t *testing.T) {
	// ParseInt stops at the first non-digit character
	result, err := ParseInt("123abc")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != 123 {
		t.Errorf("expected 123 (stops at non-digit), got %d", result)
	}
}

func TestParseInt_LargeNumber(t *testing.T) {
	result, err := ParseInt("9999999999999")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != int64(9999999999999) {
		t.Errorf("expected 9999999999999, got %d", result)
	}
}

func TestParseInt_LeadingNonDigit(t *testing.T) {
	result, err := ParseInt("abc123")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != 0 {
		t.Errorf("expected 0 for leading non-digits, got %d", result)
	}
}

func TestParseInt_SingleDigit(t *testing.T) {
	result, err := ParseInt("7")
	if err != nil {
		t.Fatalf("ParseInt failed: %v", err)
	}
	if result != 7 {
		t.Errorf("expected 7, got %d", result)
	}
}

func TestSanitizeLogBody_ShortBody_NoTruncation(t *testing.T) {
	body := "hello world"
	result := SanitizeLogBody(body, 100)
	if result != body {
		t.Errorf("expected %q, got %q", body, result)
	}
}

func TestSanitizeLogBody_ExactLength_NoTruncation(t *testing.T) {
	body := "hello world"
	result := SanitizeLogBody(body, 11)
	if result != body {
		t.Errorf("expected %q, got %q", body, result)
	}
}

func TestSanitizeLogBody_LongBody_Truncated(t *testing.T) {
	body := "hello world this is a long string"
	result := SanitizeLogBody(body, 10)
	// After removing runes until len <= 10: "hello worl" (10 bytes) + "…" (3 bytes) = 13 bytes
	if len(result) != 13 {
		t.Errorf("expected length 13 (10 bytes + 3-byte …), got %d", len(result))
	}
	expected := "hello worl…"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeLogBody_MultiByteUTF8(t *testing.T) {
	// Chinese characters are 3 bytes each in UTF-8
	// "你好世界" is 12 bytes; truncating to maxLen=10 removes last rune '界' (3 bytes),
	// leaving "你好世" (9 bytes) + "…" (3 bytes) = 12 bytes
	body := "你好世界"
	result := SanitizeLogBody(body, 10)
	if !utf8.ValidString(result) {
		t.Errorf("result is not valid UTF-8: %q", result)
	}
	expected := "你好世…"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeLogBody_UUIDRedacted(t *testing.T) {
	body := "error: team 793ac38b-0211-43e6-baa7-aa7054c39931 not found"
	result := SanitizeLogBody(body, 200)
	expected := "error: team [REDACTED] not found"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeLogBody_UUIDRedactedAfterTruncation(t *testing.T) {
	// Body with UUID, truncation first then redaction
	body := "error team 793ac38b-0211-43e6-baa7-aa7054c39931 not found in region us-east-1"
	result := SanitizeLogBody(body, 30)
	// After removing runes until len <= 30:
	// "error team 793ac38b-0211-43e6-" (30 bytes) + "…" (3 bytes) = 33 bytes
	// The UUID is partial, so regex does not match. Result is unchanged from truncation.
	expected := "error team 793ac38b-0211-43e6-…"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeLogBody_MultipleUUIDs(t *testing.T) {
	body := "ids: 793ac38b-0211-43e6-baa7-aa7054c39931 and 550e8400-e29b-41d4-a716-446655440000"
	result := SanitizeLogBody(body, 200)
	expected := "ids: [REDACTED] and [REDACTED]"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestSanitizeLogBody_ZeroMaxLen(t *testing.T) {
	body := "hello"
	result := SanitizeLogBody(body, 0)
	if result != "…" {
		t.Errorf("expected %q, got %q", "…", result)
	}
}

func TestSanitizeLogBody_EmptyBody(t *testing.T) {
	result := SanitizeLogBody("", 100)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// ParseUUIDParam
// ---------------------------------------------------------------------------

func TestParseUUIDParam_Valid(t *testing.T) {
	testUUID := uuid.Must(uuid.Parse("793ac38b-0211-43e6-baa7-aa7054c39931"))
	r := httptest.NewRequest("GET", "/test/"+testUUID.String(), http.NoBody)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testUUID.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	result, err := ParseUUIDParam(r, "id")
	if err != nil {
		t.Fatalf("ParseUUIDParam failed: %v", err)
	}
	if result != testUUID {
		t.Errorf("expected %v, got %v", testUUID, result)
	}
}

func TestParseUUIDParam_Invalid(t *testing.T) {
	r := httptest.NewRequest("GET", "/test/not-a-uuid", http.NoBody)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "not-a-uuid")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

	result, err := ParseUUIDParam(r, "id")
	if err == nil {
		t.Error("ParseUUIDParam should return error for invalid UUID")
	}
	if result != uuid.Nil {
		t.Errorf("expected uuid.Nil, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// WriteOpenAIError
// ---------------------------------------------------------------------------

func TestWriteOpenAIError(t *testing.T) {
	w := httptest.NewRecorder()
	testMessage := "test error"
	testStatusCode := 429

	WriteOpenAIError(w, testMessage, testStatusCode)

	// Check status code
	if w.Code != testStatusCode {
		t.Errorf("expected status code %d, got %d", testStatusCode, w.Code)
	}

	// Check content type
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", w.Header().Get("Content-Type"))
	}

	// Check response body structure
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	errorObj, ok := response["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response should contain 'error' object")
	}

	if errorObj["message"] != testMessage {
		t.Errorf("expected message %q, got %q", testMessage, errorObj["message"])
	}

	if errorObj["type"] != "rate_limit_error" {
		t.Errorf("expected type %q, got %q", "rate_limit_error", errorObj["type"])
	}

	if errorObj["code"] != float64(testStatusCode) {
		t.Errorf("expected code %d, got %v", testStatusCode, errorObj["code"])
	}
}

// ---------------------------------------------------------------------------
// BuildProviderTargetURL
// ---------------------------------------------------------------------------

func TestBuildProviderTargetURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		providerType string
		endpoint     string
		expected     string
	}{
		{
			name:         "OpenAI type with /v1 in base URL",
			baseURL:      "https://api.openai.com/v1",
			providerType: "openai",
			expected:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:         "Embeddings endpoint",
			baseURL:      "https://api.openai.com/v1",
			providerType: "openai",
			endpoint:     "/embeddings",
			expected:     "https://api.openai.com/v1/embeddings",
		},
		{
			name:         "Image generations endpoint",
			baseURL:      "https://api.openai.com/v1",
			providerType: "openai",
			endpoint:     "/images/generations",
			expected:     "https://api.openai.com/v1/images/generations",
		},
		{
			name:         "Audio speech endpoint",
			baseURL:      "https://api.openai.com/v1",
			providerType: "openai",
			endpoint:     "/audio/speech",
			expected:     "https://api.openai.com/v1/audio/speech",
		},
		{
			name:         "Audio transcriptions endpoint with trailing slash base",
			baseURL:      "https://api.openai.com/v1/",
			providerType: "openai",
			endpoint:     "/audio/transcriptions",
			expected:     "https://api.openai.com/v1/audio/transcriptions",
		},
		{
			name:         "Anthropic without /v1 gets prefix for any endpoint",
			baseURL:      "https://api.anthropic.com",
			providerType: "anthropic",
			endpoint:     "/embeddings",
			expected:     "https://api.anthropic.com/v1/embeddings",
		},
		{
			name:         "Anthropic with /v1 already for any endpoint",
			baseURL:      "https://api.anthropic.com/v1",
			providerType: "anthropic",
			endpoint:     "/embeddings",
			expected:     "https://api.anthropic.com/v1/embeddings",
		},
		{
			name:         "Wafer AI without /v1 (user must include it)",
			baseURL:      "https://pass.wafer.ai",
			providerType: "openai",
			expected:     "https://pass.wafer.ai/chat/completions",
		},
		{
			name:         "Wafer AI with /v1 in base URL",
			baseURL:      "https://pass.wafer.ai/v1",
			providerType: "openai",
			expected:     "https://pass.wafer.ai/v1/chat/completions",
		},
		{
			name:         "Anthropic type",
			baseURL:      "https://api.anthropic.com",
			providerType: "anthropic",
			expected:     "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "Anthropic with /v1 already",
			baseURL:      "https://api.anthropic.com/v1",
			providerType: "anthropic",
			expected:     "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "DeepSeek with /v1 in base URL",
			baseURL:      "https://api.deepseek.com/v1",
			providerType: "deepseek",
			expected:     "https://api.deepseek.com/v1/chat/completions",
		},
		{
			name:         "Ollama Cloud with /v1",
			baseURL:      "https://ollama.com/v1",
			providerType: "ollama-cloud",
			expected:     "https://ollama.com/v1/chat/completions",
		},
		{
			name:         "OpenRouter with /api/v1",
			baseURL:      "https://openrouter.ai/api/v1",
			providerType: "openrouter",
			expected:     "https://openrouter.ai/api/v1/chat/completions",
		},
		{
			name:         "Trailing slash gets sanitized",
			baseURL:      "https://api.openai.com/v1/",
			providerType: "openai",
			expected:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:         "Empty provider type (default)",
			baseURL:      "https://api.example.com/v1",
			providerType: "",
			expected:     "https://api.example.com/v1/chat/completions",
		},
		{
			name:         "Ollama (self-hosted) bare host gets /v1",
			baseURL:      "http://ollama-a1:11434",
			providerType: "ollama",
			expected:     "http://ollama-a1:11434/v1/chat/completions",
		},
		{
			name:         "Ollama (self-hosted) with /v1 not doubled",
			baseURL:      "http://ollama-a1:11434/v1",
			providerType: "ollama",
			expected:     "http://ollama-a1:11434/v1/chat/completions",
		},
		{
			name:         "Ollama (self-hosted) trailing slash bare host gets /v1",
			baseURL:      "http://ollama-a1:11434/",
			providerType: "ollama",
			expected:     "http://ollama-a1:11434/v1/chat/completions",
		},
		{
			name:         "LM Studio bare host gets /v1",
			baseURL:      "http://localhost:1234",
			providerType: "lmstudio",
			endpoint:     "/embeddings",
			expected:     "http://localhost:1234/v1/embeddings",
		},
		{
			name:         "LM Studio with /v1 not doubled",
			baseURL:      "http://localhost:1234/v1",
			providerType: "lmstudio",
			expected:     "http://localhost:1234/v1/chat/completions",
		},
		{
			name:         "KoboldCPP bare host gets /v1",
			baseURL:      "http://localhost:5001",
			providerType: "koboldcpp",
			expected:     "http://localhost:5001/v1/chat/completions",
		},
		{
			name:         "Ollama Cloud stays on default branch (already has /v1)",
			baseURL:      "https://ollama.com/v1",
			providerType: "ollama-cloud",
			expected:     "https://ollama.com/v1/chat/completions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := tc.endpoint
			if endpoint == "" {
				endpoint = "/chat/completions"
			}
			result := BuildProviderTargetURL(tc.baseURL, tc.providerType, endpoint)
			if result != tc.expected {
				t.Errorf("BuildProviderTargetURL(%q, %q, %q) = %q, want %q", tc.baseURL, tc.providerType, endpoint, result, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SetProviderAuthHeaders
// ---------------------------------------------------------------------------

func TestSetProviderAuthHeaders(t *testing.T) {
	tests := []struct {
		name             string
		providerType     string
		apiKey           string
		expectAuthHeader string
		expectXAPIKey    string
		expectVersion    string
	}{
		{
			name:             "Anthropic",
			providerType:     "anthropic",
			apiKey:           "sk-ant-12345",
			expectXAPIKey:    "sk-ant-12345",
			expectVersion:    "2023-06-01",
			expectAuthHeader: "",
		},
		{
			name:             "OpenAI",
			providerType:     "openai",
			apiKey:           "sk-12345",
			expectAuthHeader: "Bearer sk-12345",
			expectXAPIKey:    "",
			expectVersion:    "",
		},
		{
			name:             "Empty apiKey",
			providerType:     "openai",
			apiKey:           "",
			expectAuthHeader: "",
			expectXAPIKey:    "",
			expectVersion:    "",
		},
		{
			name:             "Other provider type",
			providerType:     "deepseek",
			apiKey:           "sk-deepseek-123",
			expectAuthHeader: "Bearer sk-deepseek-123",
			expectXAPIKey:    "",
			expectVersion:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", http.NoBody)

			SetProviderAuthHeaders(req, tc.providerType, tc.apiKey)

			authHeader := req.Header.Get("Authorization")
			if authHeader != tc.expectAuthHeader {
				t.Errorf("Authorization header = %q, want %q", authHeader, tc.expectAuthHeader)
			}

			xAPIKey := req.Header.Get("x-api-key")
			if xAPIKey != tc.expectXAPIKey {
				t.Errorf("x-api-key header = %q, want %q", xAPIKey, tc.expectXAPIKey)
			}

			version := req.Header.Get("anthropic-version")
			if version != tc.expectVersion {
				t.Errorf("anthropic-version header = %q, want %q", version, tc.expectVersion)
			}
		})
	}
}
