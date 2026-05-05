package util

import (
	"net/http/httptest"
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// ParseBearerToken
// ---------------------------------------------------------------------------

func TestParseBearerToken_Valid(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
	token, ok := ParseBearerToken(r)
	if ok {
		t.Error("ParseBearerToken should return false when Authorization header is missing")
	}
	if token != "" {
		t.Errorf("expected empty token, got %q", token)
	}
}

func TestParseBearerToken_EmptyHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
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
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer token with spaces")
	token, ok := ParseBearerToken(r)
	if !ok {
		t.Fatal("ParseBearerToken should return true even if token value contains spaces")
	}
	if token != "token with spaces" {
		t.Errorf("expected %q, got %q", "token with spaces", token)
	}
}

// ---------------------------------------------------------------------------
// GetIntQueryParam
// ---------------------------------------------------------------------------

func TestGetIntQueryParam_Present(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=5", nil)
	result := GetIntQueryParam(r, "page", 0)
	if result != 5 {
		t.Errorf("expected 5, got %d", result)
	}
}

func TestGetIntQueryParam_Absent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	result := GetIntQueryParam(r, "page", 1)
	if result != 1 {
		t.Errorf("expected default 1, got %d", result)
	}
}

func TestGetIntQueryParam_InvalidValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=abc", nil)
	result := GetIntQueryParam(r, "page", 42)
	if result != 42 {
		t.Errorf("expected default 42 for unparseable value, got %d", result)
	}
}

func TestGetIntQueryParam_NegativeValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?offset=-10", nil)
	result := GetIntQueryParam(r, "offset", 0)
	if result != -10 {
		t.Errorf("expected -10, got %d", result)
	}
}

func TestGetIntQueryParam_ZeroValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?limit=0", nil)
	result := GetIntQueryParam(r, "limit", 100)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestGetIntQueryParam_EmptyValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=", nil)
	result := GetIntQueryParam(r, "page", 1)
	if result != 1 {
		t.Errorf("expected default 1 for empty query value, got %d", result)
	}
}

func TestGetIntQueryParam_MultipleParams(t *testing.T) {
	r := httptest.NewRequest("GET", "/?a=1&b=2", nil)
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
	r := httptest.NewRequest("GET", "/?n=9999999", nil)
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
