package proxy

import "testing"

func TestTruncateString_ShortString(t *testing.T) {
	result := truncateString("hello", 10)
	if result != "hello" {
		t.Errorf("Expected 'hello', got %q", result)
	}
}

func TestTruncateString_ExactLength(t *testing.T) {
	result := truncateString("hello", 5)
	if result != "hello" {
		t.Errorf("Expected 'hello', got %q", result)
	}
}

func TestTruncateString_TooLong(t *testing.T) {
	result := truncateString("hello world", 5)
	if result != "hello..." {
		t.Errorf("Expected 'hello...', got %q", result)
	}
}

func TestTruncateString_UnicodeRunes(t *testing.T) {
	result := truncateString("héllo wörld", 6)
	if result != "héllo ..." {
		t.Errorf("Expected 'héllo ...', got %q", result)
	}
}

func TestTruncateString_EmptyString(t *testing.T) {
	result := truncateString("", 5)
	if result != "" {
		t.Errorf("Expected '', got %q", result)
	}
}

func TestTruncateString_ZeroMaxLen(t *testing.T) {
	result := truncateString("hello", 0)
	if result != "..." {
		t.Errorf("Expected '...', got %q", result)
	}
}

func TestTruncateString_CJKRunes(t *testing.T) {
	result := truncateString("你好世界", 2)
	if result != "你好..." {
		t.Errorf("Expected '你好...', got %q", result)
	}
}
