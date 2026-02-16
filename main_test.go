package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGetTorrentInfo(t *testing.T) {
	tests := []struct {
		name          string
		handler       func(w http.ResponseWriter, r *http.Request)
		expectError   bool
		expectTorrent bool
		expectedHash  string
	}{
		{
			name: "Success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `[{"hash":"123","name":"Test Torrent","progress":0.5,"eta":60,"dlspeed":1024,"state":"downloading"}]`)
			},
			expectError:   false,
			expectTorrent: true,
			expectedHash:  "123",
		},
		{
			name: "Torrent Not Found (Empty Array)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `[]`)
			},
			expectError:   false,
			expectTorrent: false,
		},
		{
			name: "API Error (500)",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				_, _ = fmt.Fprintln(w, `Internal Server Error`)
			},
			expectError:   true,
			expectTorrent: false,
		},
		{
			name: "Malformed JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `[{"hash":... invalid json ...`)
			},
			expectError:   true,
			expectTorrent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/api/v2/torrents/info") {
					tt.handler(w, r)
					return
				}
				w.WriteHeader(404)
			}))
			defer ts.Close()

			// Override global host
			oldHost := qbitHost
			qbitHost = ts.URL
			defer func() { qbitHost = oldHost }()

			client := ts.Client()
			torrent, err := getTorrentInfo(client, "123")

			if tt.expectError && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectTorrent && torrent == nil {
				t.Error("Expected torrent, got nil")
			}
			if !tt.expectTorrent && torrent != nil {
				t.Errorf("Expected nil torrent, got %v", torrent)
			}

			if tt.expectTorrent && torrent != nil && torrent.Hash != tt.expectedHash {
				t.Errorf("Expected hash %s, got %s", tt.expectedHash, torrent.Hash)
			}
		})
	}
}

func TestSendNtfy(t *testing.T) {
	// Mock Ntfy Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Check for specific header if provided (testing auth)
		if strings.Contains(r.URL.Path, "auth_topic") {
			user, pass, ok := r.BasicAuth()
			if !ok {
				t.Error("Expected Basic Auth header, got none")
			}
			if user != "testuser" || pass != "testpass" {
				t.Errorf("Expected user/pass 'testuser'/'testpass', got '%s'/'%s'", user, pass)
			}
		}

		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	// Override global config
	oldServer := ntfyServer
	oldTopic := ntfyTopic
	oldUser := ntfyUser
	oldPass := ntfyPass

	t.Cleanup(func() {
		ntfyServer = oldServer
		ntfyTopic = oldTopic
		ntfyUser = oldUser
		ntfyPass = oldPass
	})

	ntfyServer = ts.URL
	ntfyTopic = "test_topic"

	// 1. Test standard notification (no auth)
	sendNtfy("Test Title", "Test Message", "tag", "id", "3")

	// 2. Test authenticated notification
	ntfyTopic = "auth_topic"
	ntfyUser = "testuser"
	ntfyPass = "testpass"
	sendNtfy("Auth Title", "Auth Message", "tag", "id", "3")
}
