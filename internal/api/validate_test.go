package api

import (
	"testing"
)

// ---------------------------------------------------------------------------
// validateStringLength
// ---------------------------------------------------------------------------

func TestValidateStringLength_Valid(t *testing.T) {
	err := validateStringLength("name", "hello", 1, 100)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateStringLength_TooShort(t *testing.T) {
	err := validateStringLength("name", "", 1, 100)
	if err == nil {
		t.Error("expected error for empty string with min=1")
	}
}

func TestValidateStringLength_TooLong(t *testing.T) {
	err := validateStringLength("name", "abcdefgh", 1, 5)
	if err == nil {
		t.Error("expected error for string exceeding max length")
	}
}

func TestValidateStringLength_Trimmed(t *testing.T) {
	err := validateStringLength("name", "  ab  ", 2, 5)
	if err != nil {
		t.Errorf("trimmed value 'ab' within [2,5], expected no error, got %v", err)
	}
}

func TestValidateStringLength_TrimmedTooShort(t *testing.T) {
	err := validateStringLength("name", "   ", 1, 100)
	if err == nil {
		t.Error("expected error for whitespace-only string trimmed to empty with min=1")
	}
}

// ---------------------------------------------------------------------------
// validateStringPtrLength
// ---------------------------------------------------------------------------

func TestValidateStringPtrLength_Nil(t *testing.T) {
	err := validateStringPtrLength("name", nil, 1, 100)
	if err != nil {
		t.Errorf("nil pointer should return nil error, got %v", err)
	}
}

func TestValidateStringPtrLength_Valid(t *testing.T) {
	s := "hello"
	err := validateStringPtrLength("name", &s, 1, 100)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateStringPtrLength_TooShort(t *testing.T) {
	s := ""
	err := validateStringPtrLength("name", &s, 1, 100)
	if err == nil {
		t.Error("expected error for empty string with min=1")
	}
}

// ---------------------------------------------------------------------------
// validateIntRange
// ---------------------------------------------------------------------------

func TestValidateIntRange_WithinRange(t *testing.T) {
	err := validateIntRange("age", 5, 1, 10)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateIntRange_BelowMin(t *testing.T) {
	err := validateIntRange("age", 0, 1, 10)
	if err == nil {
		t.Error("expected error for value below min")
	}
}

func TestValidateIntRange_AboveMax(t *testing.T) {
	err := validateIntRange("age", 11, 1, 10)
	if err == nil {
		t.Error("expected error for value above max")
	}
}

func TestValidateIntRange_AtMin(t *testing.T) {
	err := validateIntRange("age", 1, 1, 10)
	if err != nil {
		t.Errorf("value at min boundary should be valid, got %v", err)
	}
}

func TestValidateIntRange_AtMax(t *testing.T) {
	err := validateIntRange("age", 10, 1, 10)
	if err != nil {
		t.Errorf("value at max boundary should be valid, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// validateIntPtrRange
// ---------------------------------------------------------------------------

func TestValidateIntPtrRange_Nil(t *testing.T) {
	err := validateIntPtrRange("age", nil, 1, 10)
	if err != nil {
		t.Errorf("nil pointer should return nil error, got %v", err)
	}
}

func TestValidateIntPtrRange_Valid(t *testing.T) {
	v := 5
	err := validateIntPtrRange("age", &v, 1, 10)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateIntPtrRange_OutOfRange(t *testing.T) {
	v := 0
	err := validateIntPtrRange("age", &v, 1, 10)
	if err == nil {
		t.Error("expected error for value below min")
	}
}

// ---------------------------------------------------------------------------
// validateFloatRange
// ---------------------------------------------------------------------------

func TestValidateFloatRange_WithinRange(t *testing.T) {
	err := validateFloatRange("rate", 0.5, 0.0, 1.0)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFloatRange_BelowMin(t *testing.T) {
	err := validateFloatRange("rate", -0.1, 0.0, 1.0)
	if err == nil {
		t.Error("expected error for value below min")
	}
}

func TestValidateFloatRange_AboveMax(t *testing.T) {
	err := validateFloatRange("rate", 1.1, 0.0, 1.0)
	if err == nil {
		t.Error("expected error for value above max")
	}
}

func TestValidateFloatRange_AtBoundaries(t *testing.T) {
	if err := validateFloatRange("rate", 0.0, 0.0, 1.0); err != nil {
		t.Errorf("at min boundary should be valid, got %v", err)
	}
	if err := validateFloatRange("rate", 1.0, 0.0, 1.0); err != nil {
		t.Errorf("at max boundary should be valid, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// validateFloatPtrRange
// ---------------------------------------------------------------------------

func TestValidateFloatPtrRange_Nil(t *testing.T) {
	err := validateFloatPtrRange("rate", nil, 0.0, 1.0)
	if err != nil {
		t.Errorf("nil pointer should return nil error, got %v", err)
	}
}

func TestValidateFloatPtrRange_Valid(t *testing.T) {
	v := 0.5
	err := validateFloatPtrRange("rate", &v, 0.0, 1.0)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFloatPtrRange_OutOfRange(t *testing.T) {
	v := 1.5
	err := validateFloatPtrRange("rate", &v, 0.0, 1.0)
	if err == nil {
		t.Error("expected error for value above max")
	}
}

// ---------------------------------------------------------------------------
// validateMapSize
// ---------------------------------------------------------------------------

func TestValidateMapSize_WithinLimit(t *testing.T) {
	m := map[string]bool{"a": true, "b": true}
	err := validateMapSize("tags", m, 5)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateMapSize_ExceedsLimit(t *testing.T) {
	m := map[string]bool{"a": true, "b": true, "c": true}
	err := validateMapSize("tags", m, 2)
	if err == nil {
		t.Error("expected error for map exceeding max entries")
	}
}

func TestValidateMapSize_EmptyMap(t *testing.T) {
	m := map[string]bool{}
	err := validateMapSize("tags", m, 0)
	if err != nil {
		t.Errorf("empty map with max=0 should be valid, got %v", err)
	}
}

func TestValidateMapSize_Nil(t *testing.T) {
	err := validateMapSize("tags", nil, 0)
	if err != nil {
		t.Errorf("nil map with max=0 should be valid, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// validateNameString
// ---------------------------------------------------------------------------

func TestValidateNameString_Valid(t *testing.T) {
	result, err := validateNameString("name", "  hello  ", 1, 100)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "hello" {
		t.Errorf("expected trimmed value %q, got %q", "hello", result)
	}
}

func TestValidateNameString_TooShort(t *testing.T) {
	_, err := validateNameString("name", "  a  ", 2, 100)
	if err == nil {
		t.Error("expected error for trimmed string shorter than min")
	}
}

func TestValidateNameString_TooLong(t *testing.T) {
	_, err := validateNameString("name", "abcdefgh", 1, 5)
	if err == nil {
		t.Error("expected error for string exceeding max length after trimming")
	}
}

func TestValidateNameString_WithControlChar(t *testing.T) {
	_, err := validateNameString("name", "hel\x00lo", 1, 100)
	if err == nil {
		t.Error("expected error for control character in name")
	}
}

func TestValidateNameString_WithZeroWidthSpace(t *testing.T) {
	_, err := validateNameString("name", "hel\u200Blo", 1, 100)
	if err == nil {
		t.Error("expected error for zero-width space in name")
	}
}

func TestValidateNameString_WithNonBreakingSpace(t *testing.T) {
	_, err := validateNameString("name", "hel\u00A0lo", 1, 100)
	if err == nil {
		t.Error("expected error for non-breaking space in name")
	}
}

func TestValidateNameString_WithEmoji(t *testing.T) {
	// Emoji should be allowed (not control, not zero-width, not non-standard whitespace)
	result, err := validateNameString("name", "hello 🎉", 1, 100)
	if err != nil {
		t.Errorf("emoji should be allowed, got error: %v", err)
	}
	if result != "hello 🎉" {
		t.Errorf("expected %q, got %q", "hello 🎉", result)
	}
}

// ---------------------------------------------------------------------------
// validateNamePtr
// ---------------------------------------------------------------------------

func TestValidateNamePtr_Nil(t *testing.T) {
	result, err := validateNamePtr("name", nil, 1, 100)
	if err != nil {
		t.Errorf("nil pointer should return nil error, got %v", err)
	}
	if result != nil {
		t.Errorf("nil pointer should return nil result, got %v", result)
	}
}

func TestValidateNamePtr_Valid(t *testing.T) {
	s := "  hello  "
	result, err := validateNamePtr("name", &s, 1, 100)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result == nil || *result != "hello" {
		t.Errorf("expected trimmed pointer to %q, got %v", "hello", result)
	}
}

func TestValidateNamePtr_InvalidControlChar(t *testing.T) {
	s := "hel\x00lo"
	_, err := validateNamePtr("name", &s, 1, 100)
	if err == nil {
		t.Error("expected error for control character")
	}
}

// ---------------------------------------------------------------------------
// validatePrintable
// ---------------------------------------------------------------------------

func TestValidatePrintable_ValidString(t *testing.T) {
	err := validatePrintable("field", "hello world 123")
	if err != nil {
		t.Errorf("normal printable string should pass, got %v", err)
	}
}

func TestValidatePrintable_ControlCharacter(t *testing.T) {
	err := validatePrintable("field", "hel\x00lo")
	if err == nil {
		t.Error("expected error for null byte")
	}
}

func TestValidatePrintable_Newline(t *testing.T) {
	err := validatePrintable("field", "hel\nlo")
	if err == nil {
		t.Error("expected error for newline (control character)")
	}
}

func TestValidatePrintable_Tab(t *testing.T) {
	err := validatePrintable("field", "hel\tlo")
	if err == nil {
		t.Error("expected error for tab (control character)")
	}
}

func TestValidatePrintable_ZeroWidthSpace(t *testing.T) {
	err := validatePrintable("field", "hel\u200Blo")
	if err == nil {
		t.Error("expected error for U+200B zero-width space")
	}
}

func TestValidatePrintable_ZeroWidthNonJoiner(t *testing.T) {
	err := validatePrintable("field", "hel\u200Clo")
	if err == nil {
		t.Error("expected error for U+200C zero-width non-joiner")
	}
}

func TestValidatePrintable_ZeroWidthJoiner(t *testing.T) {
	err := validatePrintable("field", "hel\u200Dlo")
	if err == nil {
		t.Error("expected error for U+200D zero-width joiner")
	}
}

func TestValidatePrintable_BOM(t *testing.T) {
	err := validatePrintable("field", "hel\uFEFFlo")
	if err == nil {
		t.Error("expected error for U+FEFF BOM")
	}
}

func TestValidatePrintable_WordJoiner(t *testing.T) {
	err := validatePrintable("field", "hel\u2060lo")
	if err == nil {
		t.Error("expected error for U+2060 word joiner")
	}
}

func TestValidatePrintable_InvisibleOperators(t *testing.T) {
	for _, r := range []rune{'\u2061', '\u2062', '\u2063', '\u2064'} {
		err := validatePrintable("field", "hel"+string(r)+"lo")
		if err == nil {
			t.Errorf("expected error for invisible operator U+%04X", r)
		}
	}
}

func TestValidatePrintable_NonBreakingSpace(t *testing.T) {
	err := validatePrintable("field", "hel\u00A0lo")
	if err == nil {
		t.Error("expected error for U+00A0 non-breaking space")
	}
}

func TestValidatePrintable_LineSeparator(t *testing.T) {
	err := validatePrintable("field", "hel\u2028lo")
	if err == nil {
		t.Error("expected error for U+2028 line separator")
	}
}

func TestValidatePrintable_ParagraphSeparator(t *testing.T) {
	err := validatePrintable("field", "hel\u2029lo")
	if err == nil {
		t.Error("expected error for U+2029 paragraph separator")
	}
}

func TestValidatePrintable_IdeographicSpace(t *testing.T) {
	err := validatePrintable("field", "hel\u3000lo")
	if err == nil {
		t.Error("expected error for U+3000 ideographic space")
	}
}

func TestValidatePrintable_RegularSpace(t *testing.T) {
	err := validatePrintable("field", "hello world")
	if err != nil {
		t.Errorf("regular space U+0020 should be allowed, got %v", err)
	}
}

func TestValidatePrintable_EmptyString(t *testing.T) {
	err := validatePrintable("field", "")
	if err != nil {
		t.Errorf("empty string should pass (no characters to reject), got %v", err)
	}
}

func TestValidatePrintable_UnicodeAllowed(t *testing.T) {
	err := validatePrintable("field", "café 🎉")
	if err != nil {
		t.Errorf("normal unicode characters should be allowed, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// trimString
// ---------------------------------------------------------------------------

func TestTrimString_Spaces(t *testing.T) {
	result := trimString("  hello  ")
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestTrimString_NoSpaces(t *testing.T) {
	result := trimString("hello")
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestTrimString_EmptyString(t *testing.T) {
	result := trimString("")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestTrimString_OnlySpaces(t *testing.T) {
	result := trimString("   ")
	if result != "" {
		t.Errorf("expected empty string for whitespace-only input, got %q", result)
	}
}