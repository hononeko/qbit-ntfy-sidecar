package main

import (
	"testing"
)

func TestDrawProgressBar(t *testing.T) {
	tests := []struct {
		pct      int
		expected string
	}{
		{0, "[░░░░░░░░░░]"},
		{50, "[█████░░░░░]"},
		{100, "[██████████]"},
		{5, "[█░░░░░░░░░]"},   // Rounds up/down logic check
		{-10, "[░░░░░░░░░░]"}, // Edge case: underflow
		{150, "[██████████]"}, // Edge case: overflow
	}

	for _, tt := range tests {
		result := drawProgressBar(tt.pct)
		if result != tt.expected {
			t.Errorf("drawProgressBar(%d): expected %s, got %s", tt.pct, tt.expected, result)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		seconds  int
		expected string
	}{
		{60, "1m0s"},
		{3600, "1h0m0s"},
		{-1, "∞"},
		{-100, "∞"},
		{8640000, "∞"},
		{9999999, "∞"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.seconds)
		if result != tt.expected {
			t.Errorf("formatDuration(%d): expected %s, got %s", tt.seconds, tt.expected, result)
		}
	}
}

func TestIsValidHash(t *testing.T) {
	tests := []struct {
		hash  string
		valid bool
	}{
		{"1234567890abcdef1234567890abcdef12345678", true},                                 // 40 char v1
		{"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef", true},         // 64 char v2
		{"1234567890ABCDEF1234567890ABCDEF12345678", true},                                 // uppercase
		{"12345", false},                                                                     // too short
		{"1234567890abcdef1234567890abcdef1234567g", false},                                 // invalid char 'g'
		{"1234567890abcdef1234567890abcdef12345678;", false},                                // injection
		{"", false},
	}

	for _, tt := range tests {
		if got := IsValidHash(tt.hash); got != tt.valid {
			t.Errorf("IsValidHash(%q): expected %v, got %v", tt.hash, tt.valid, got)
		}
	}
}

func TestSanitizeHeader(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Normal Title", "Normal Title"},
		{"Line\r\nBreak", "Line Break"},
		{"\rLeading\nTrailing\r\n", "Leading Trailing"},
	}

	for _, tt := range tests {
		if got := sanitizeHeader(tt.input); got != tt.expected {
			t.Errorf("sanitizeHeader(%q): expected %q, got %q", tt.input, tt.expected, got)
		}
	}
}
