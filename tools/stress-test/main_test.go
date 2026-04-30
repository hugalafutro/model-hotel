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
