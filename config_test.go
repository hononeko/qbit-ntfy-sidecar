package main

import (
	"os"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	_ = os.Setenv("TEST_KEY", "value")
	defer func() { _ = os.Unsetenv("TEST_KEY") }()

	if val := getEnv("TEST_KEY", "fallback"); val != "value" {
		t.Errorf("expected 'value', got '%s'", val)
	}

	if val := getEnv("NON_EXISTING", "fallback"); val != "fallback" {
		t.Errorf("expected 'fallback', got '%s'", val)
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		val      string
		fallback bool
		expected bool
	}{
		{"true", false, true},
		{"false", true, false},
		{"1", false, true},
		{"0", true, false},
		{"invalid", true, true},
		{"invalid", false, false},
	}

	for _, tt := range tests {
		_ = os.Setenv("TEST_BOOL", tt.val)
		if val := getEnvBool("TEST_BOOL", tt.fallback); val != tt.expected {
			t.Errorf("for val=%q fallback=%v, expected %v got %v", tt.val, tt.fallback, tt.expected, val)
		}
	}

	_ = os.Unsetenv("TEST_BOOL")
	if val := getEnvBool("TEST_BOOL", true); !val {
		t.Errorf("expected fallback true, got %v", val)
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		val      string
		fallback int
		expected int
	}{
		{"10", 5, 10},
		{"-1", 5, -1},
		{"abc", 5, 5},
	}

	for _, tt := range tests {
		_ = os.Setenv("TEST_INT", tt.val)
		if val := getEnvInt("TEST_INT", tt.fallback); val != tt.expected {
			t.Errorf("for val=%q fallback=%d, expected %d got %d", tt.val, tt.fallback, tt.expected, val)
		}
	}

	_ = os.Unsetenv("TEST_INT")
	if val := getEnvInt("TEST_INT", 5); val != 5 {
		t.Errorf("expected fallback 5, got %d", val)
	}
}

func TestLoadConfig(t *testing.T) {
	// Backup and cleanup
	defer func() {
		_ = os.Unsetenv("NTFY_TOPIC")
		_ = os.Unsetenv("ALLOWED_SUBNETS")
		allowedSubnets = nil
	}()

	// Mock required env var
	_ = os.Setenv("NTFY_TOPIC", "test_topic")

	// Test 1: Empty Subnets ensures allowedSubnets is nil/empty and Defaults apply
	_ = os.Setenv("ALLOWED_SUBNETS", "")
	loadConfig()

	if ntfyTopic != "test_topic" {
		t.Errorf("expected topic 'test_topic', got '%s'", ntfyTopic)
	}
	if len(allowedSubnets) != 0 {
		t.Errorf("expected 0 subnets, got %d", len(allowedSubnets))
	}
	if pollInt != 5*time.Second {
		t.Errorf("expected 5s poll interval, got %v", pollInt)
	}

	// Test 2: Invalid Subnets are ignored, valid ones are parsed
	_ = os.Setenv("ALLOWED_SUBNETS", "invalid,192.168.1.0/24,10.0.0.1,fe80::1")
	allowedSubnets = nil // Reset state
	loadConfig()

	if len(allowedSubnets) != 3 {
		t.Errorf("expected 3 parsed subnets, got %d", len(allowedSubnets))
	}

	// Check if the single IPs were expanded correctly
	expectedMasks := []int{24, 32, 128} // /24, /32 (IPv4 single), /128 (IPv6 single)
	for i, subnet := range allowedSubnets {
		if subnet.Bits() != expectedMasks[i] {
			t.Errorf("subnet %d expected mask /%d, got /%d", i, expectedMasks[i], subnet.Bits())
		}
	}
}
