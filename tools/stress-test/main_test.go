package main

import (
	"reflect"
	"testing"
)

func TestParseIntList(t *testing.T) {
	tests := []struct {
		input    string
		expected []int
	}{
		{"1,2,3", []int{1, 2, 3}},
		{"10, 20 ,30", []int{10, 20, 30}},
	}

	for _, tt := range tests {
		result := parseIntList(tt.input)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("parseIntList(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestParseBoolList(t *testing.T) {
	tests := []struct {
		input    string
		expected []bool
	}{
		{"true,false,true", []bool{true, false, true}},
		{"1,0", []bool{true, false}},
	}

	for _, tt := range tests {
		result := parseBoolList(tt.input)
		if !reflect.DeepEqual(result, tt.expected) {
			t.Errorf("parseBoolList(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestMaxInt(t *testing.T) {
	tests := []struct {
		input    []int
		expected int
	}{
		{[]int{1, 3, 2}, 3},

		{[]int{5}, 5},
	}

	for _, tt := range tests {
		result := maxInt(tt.input)
		if result != tt.expected {
			t.Errorf("maxInt(%v) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}

func TestReplacePortInURL(t *testing.T) {
	tests := []struct {
		input    string
		newPort  int
		expected string
	}{
		{"http://host.docker.internal:9090/v1", 9091, "http://host.docker.internal:9091/v1"},
		{"http://localhost:8080/v1", 9090, "http://localhost:9090/v1"},
		{"http://example.com:3000/api", 4000, "http://example.com:4000/api"},
		{"http://noport.com/v1", 9090, "http://noport.com:9090/v1"}, // adds port
		{"nocolon", 9090, "nocolon"},                                // no scheme
		{"http://[::1]:9090/v1", 9091, "http://[::1]:9091/v1"},      // IPv6
	}

	for _, tt := range tests {
		result := replacePortInURL(tt.input, tt.newPort)
		if result != tt.expected {
			t.Errorf("replacePortInURL(%q, %d) = %q, want %q", tt.input, tt.newPort, result, tt.expected)
		}
	}
}

func TestParseDurationRange(t *testing.T) {
	tests := []struct {
		input   string
		min     int
		max     int
		wantErr bool
	}{
		{"3-13", 3, 13, false},
		{"1-5", 1, 5, false},
		{"5-5", 5, 5, false},
		{"10-30", 10, 30, false},
		{"3", 0, 0, true},     // missing dash
		{"0-5", 0, 0, true},   // min must be positive
		{"5-0", 0, 0, true},   // max must be positive
		{"10-3", 0, 0, true},  // min > max
		{"abc-5", 0, 0, true}, // non-numeric min
		{"3-xyz", 0, 0, true}, // non-numeric max
	}

	for _, tt := range tests {
		min, max, err := parseDurationRange(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseDurationRange(%q) expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseDurationRange(%q) unexpected error: %v", tt.input, err)
			} else if min != tt.min || max != tt.max {
				t.Errorf("parseDurationRange(%q) = (%d, %d), want (%d, %d)", tt.input, min, max, tt.min, tt.max)
			}
		}
	}
}
