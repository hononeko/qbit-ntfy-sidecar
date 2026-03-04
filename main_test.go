package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
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

		// Common assertions
		if got := r.Header.Get("Priority"); got != "3" {
			t.Errorf("Expected Priority '3', got '%s'", got)
		}
		if got := r.Header.Get("Tags"); got != "tag" {
			t.Errorf("Expected Tags 'tag', got '%s'", got)
		}

		// Check body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("Failed to read request body: %v", err)
		}

		// Check for specific header if provided (testing auth)
		if strings.Contains(r.URL.Path, "auth_topic") {
			// Auth Case
			user, pass, ok := r.BasicAuth()
			if !ok {
				t.Error("Expected Basic Auth header, got none")
			}
			if user != "testuser" || pass != "testpass" {
				t.Errorf("Expected user/pass 'testuser'/'testpass', got '%s'/'%s'", user, pass)
			}
			if got := r.Header.Get("Title"); got != "Auth Title" {
				t.Errorf("Expected Title 'Auth Title', got '%s'", got)
			}
			if string(body) != "Auth Message" {
				t.Errorf("Expected body 'Auth Message', got '%s'", string(body))
			}
		} else {
			// Standard Case
			if got := r.Header.Get("Title"); got != "Test Title" {
				t.Errorf("Expected Title 'Test Title', got '%s'", got)
			}
			if string(body) != "Test Message" {
				t.Errorf("Expected body 'Test Message', got '%s'", string(body))
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

func parseSubnets(subnets []string) []netip.Prefix {
	var parsed []netip.Prefix
	for _, s := range subnets {
		if !strings.Contains(s, "/") {
			if strings.Contains(s, ":") {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		prefix, err := netip.ParsePrefix(s)
		if err == nil {
			parsed = append(parsed, prefix)
		}
	}
	return parsed
}

func TestIPFilterMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		allowedSubnets []string
		remoteAddr     string
		expectedStatus int
	}{
		{
			name:           "Empty config (Deny All)",
			allowedSubnets: []string{},
			remoteAddr:     "192.168.1.5:1234",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv4 Allowed Subnet",
			allowedSubnets: []string{"192.168.1.0/24"},
			remoteAddr:     "192.168.1.100:4567",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv4 Denied Subnet",
			allowedSubnets: []string{"10.0.0.0/8"},
			remoteAddr:     "192.168.1.100:4567",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv4 Single IP Allowed",
			allowedSubnets: []string{"192.168.1.5"},
			remoteAddr:     "192.168.1.5:8080",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv4 Single IP Denied",
			allowedSubnets: []string{"192.168.1.5"},
			remoteAddr:     "192.168.1.6:8080",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv6 Allowed Subnet",
			allowedSubnets: []string{"2001:db8::/32"},
			remoteAddr:     "[2001:db8::1]:1234",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv6 Denied Subnet",
			allowedSubnets: []string{"2001:db8::/32"},
			remoteAddr:     "[2001:0db9::1]:1234", // Note the 9
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv6 Single IP Allowed (with brackets from r.RemoteAddr)",
			allowedSubnets: []string{"fe80::1"},
			remoteAddr:     "[fe80::1]:1234",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv6 Localhost Allowed",
			allowedSubnets: []string{"::1"},
			remoteAddr:     "[::1]:8080",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid RemoteAddr format",
			allowedSubnets: []string{"192.168.1.0/24"},
			remoteAddr:     "notanip",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Backup global subnets
			oldSubnets := allowedSubnets
			t.Cleanup(func() { allowedSubnets = oldSubnets })

			// Populate allowedSubnets
			allowedSubnets = parseSubnets(tt.allowedSubnets)

			// Setup dummy handler to wrap
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			middleware := ipFilterMiddleware(nextHandler)

			// Execute mock request
			req := httptest.NewRequest("POST", "/track?hash=123", nil)
			req.RemoteAddr = tt.remoteAddr

			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}
