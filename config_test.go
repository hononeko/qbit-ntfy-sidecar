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
	defer func() {
		_ = os.Unsetenv("NTFY_TOPIC")
		_ = os.Unsetenv("ALLOWED_SUBNETS")
	}()

	// Mock required env var
	_ = os.Setenv("NTFY_TOPIC", "test_topic")

	// Test 1: Empty Subnets ensures allowedSubnets is nil/empty and Defaults apply
	_ = os.Setenv("ALLOWED_SUBNETS", "")
	cfg := loadConfig()

	if cfg.NtfyTopic != "test_topic" {
		t.Errorf("expected topic 'test_topic', got '%s'", cfg.NtfyTopic)
	}
	if len(cfg.AllowedSubnets) != 0 {
		t.Errorf("expected 0 subnets, got %d", len(cfg.AllowedSubnets))
	}
	if cfg.PollInt != 5*time.Second {
		t.Errorf("expected 5s poll interval, got %v", cfg.PollInt)
	}

	// Test 2: Invalid Subnets are ignored, valid ones are parsed
	_ = os.Setenv("ALLOWED_SUBNETS", "invalid,192.168.1.0/24,10.0.0.1,fe80::1")
	cfg = loadConfig()

	if len(cfg.AllowedSubnets) != 3 {
		t.Errorf("expected 3 parsed subnets, got %d", len(cfg.AllowedSubnets))
	}

	expectedMasks := []int{24, 32, 128} // /24, /32 (IPv4 single), /128 (IPv6 single)
	for i, subnet := range cfg.AllowedSubnets {
		if subnet.Bits() != expectedMasks[i] {
			t.Errorf("subnet %d expected mask /%d, got /%d", i, expectedMasks[i], subnet.Bits())
		}
	}

	// Test 3: New configuration options parsing and defaults
	_ = os.Setenv("NOTIFICATION_MODE", "grouped")
	_ = os.Setenv("GROUP_UPDATE_INTERVAL", "30")
	_ = os.Setenv("PROGRESS_STEP", "10")
	_ = os.Setenv("MIN_UPDATE_INTERVAL", "45")
	_ = os.Setenv("NTFY_LIVE_ID", "custom-live-id")
	_ = os.Setenv("NOTIFY_PROGRESS", "false")
	_ = os.Setenv("QBIT_PUBLIC_URL", "https://qbit.example.com/")
	_ = os.Setenv("MAX_DISPLAY_TORRENTS", "8")
	defer func() {
		_ = os.Unsetenv("NOTIFICATION_MODE")
		_ = os.Unsetenv("GROUP_UPDATE_INTERVAL")
		_ = os.Unsetenv("PROGRESS_STEP")
		_ = os.Unsetenv("MIN_UPDATE_INTERVAL")
		_ = os.Unsetenv("NTFY_LIVE_ID")
		_ = os.Unsetenv("NOTIFY_PROGRESS")
		_ = os.Unsetenv("QBIT_PUBLIC_URL")
		_ = os.Unsetenv("MAX_DISPLAY_TORRENTS")
	}()

	cfg = loadConfig()
	if cfg.NotificationMode != "grouped" {
		t.Errorf("expected NotificationMode 'grouped', got '%s'", cfg.NotificationMode)
	}
	if cfg.GroupUpdateInt != 30*time.Second {
		t.Errorf("expected GroupUpdateInt 30s, got %v", cfg.GroupUpdateInt)
	}
	if cfg.ProgressStep != 10 {
		t.Errorf("expected ProgressStep 10, got %d", cfg.ProgressStep)
	}
	if cfg.MinUpdateInt != 45*time.Second {
		t.Errorf("expected MinUpdateInt 45s, got %v", cfg.MinUpdateInt)
	}
	if cfg.NtfyLiveID != "custom-live-id" {
		t.Errorf("expected NtfyLiveID 'custom-live-id', got '%s'", cfg.NtfyLiveID)
	}
	if cfg.NotifyProgress != false {
		t.Errorf("expected NotifyProgress false, got %v", cfg.NotifyProgress)
	}
	if cfg.QbitPublicURL != "https://qbit.example.com" {
		t.Errorf("expected QbitPublicURL 'https://qbit.example.com', got '%s'", cfg.QbitPublicURL)
	}
	if cfg.MaxDisplayTorrents != 8 {
		t.Errorf("expected MaxDisplayTorrents 8, got %d", cfg.MaxDisplayTorrents)
	}

	// Test 4: Invalid values fall back correctly
	_ = os.Setenv("NOTIFICATION_MODE", "invalid_mode")
	_ = os.Setenv("GROUP_UPDATE_INTERVAL", "-5")
	_ = os.Setenv("PROGRESS_STEP", "150")
	_ = os.Setenv("MIN_UPDATE_INTERVAL", "-10")
	_ = os.Setenv("MAX_DISPLAY_TORRENTS", "-3")
	cfg = loadConfig()
	if cfg.NotificationMode != "grouped" {
		t.Errorf("expected fallback NotificationMode 'grouped', got '%s'", cfg.NotificationMode)
	}
	if cfg.GroupUpdateInt != 15*time.Second {
		t.Errorf("expected fallback GroupUpdateInt 15s, got %v", cfg.GroupUpdateInt)
	}
	if cfg.ProgressStep != 25 {
		t.Errorf("expected fallback ProgressStep 25, got %d", cfg.ProgressStep)
	}
	if cfg.MinUpdateInt != 60*time.Second {
		t.Errorf("expected fallback MinUpdateInt 60s, got %v", cfg.MinUpdateInt)
	}
	if cfg.MaxDisplayTorrents != 5 {
		t.Errorf("expected fallback MaxDisplayTorrents 5, got %d", cfg.MaxDisplayTorrents)
	}
}
