package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendNtfy(t *testing.T) {
	// Mock Ntfy Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}

		// Common assertions
		if got := r.Header.Get("Priority"); got != "3" && got != "3 test" {
			t.Errorf("Expected Priority '3', got '%s'", got)
		}
		if got := r.Header.Get("Tags"); got != "tag" && got != "tag " {
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
			msgStr := string(body)
			if msgStr != "Test Message" && msgStr != "Test Message\r\n" {
				t.Errorf("Expected body 'Test Message', got '%s'", msgStr)
			}
		}

		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer: ts.URL,
		NtfyTopic:  "test_topic",
	}

	// 1. Test standard notification (no auth)
	sendNtfy(cfg, "Test Title", "Test Message", "tag", "id", "3")

	// 2. Test authenticated notification
	cfg.NtfyTopic = "auth_topic"
	cfg.NtfyUser = "testuser"
	cfg.NtfyPass = "testpass"
	sendNtfy(cfg, "Auth Title", "Auth Message", "tag", "id", "3")

	// 3. Test Header Sanitization (Newline Injection Attack)
	cfg.NtfyTopic = "test_topic"
	cfg.NtfyUser = ""
	cfg.NtfyPass = ""
	sendNtfy(cfg, "Test\r\nTitle", "Test Message\r\n", "tag\n", "id\r", "3\r\ntest")
}

func TestSendNtfy_Error(t *testing.T) {
	cfg := &Config{NtfyServer: "http://127.0.0.1:0"} // Invalid URL triggers failure
	// This should log an error and return without panicking
	sendNtfy(cfg, "Fail", "Fail", "tag", "1", "3")
}

func TestSendNtfy_HTTPErrorCodes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintln(w, `{"error":"rate limit exceeded"}`)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer: ts.URL,
		NtfyTopic:  "test_topic",
	}

	// Should safely log status 429 without panicking
	sendNtfy(cfg, "Rate Limit", "Rate Limit Test", "tag", "id", "3")
}

func TestSendUpdate(t *testing.T) {
	var expectedFormat string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg := string(body)

		if expectedFormat == "percent" {
			if !strings.Contains(msg, "Progress: 50%") {
				t.Errorf("Expected percent format, got: %s", msg)
			}
		} else {
			if !strings.Contains(msg, "[█████░░░░░]") {
				t.Errorf("Expected bar format, got: %s", msg)
			}
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer:   ts.URL,
		NtfyTopic:    "test_topic",
		NtfyPrioProg: "2",
	}

	torrent := &Torrent{Hash: "123", Name: "Test", DlSpeed: 1048576, Eta: 60} // 1MB/s

	cfg.ProgressFormat = "bar"
	expectedFormat = "bar"
	sendUpdate(cfg, torrent, 50)

	cfg.ProgressFormat = "percent"
	expectedFormat = "percent"
	sendUpdate(cfg, torrent, 50)
}

func TestSendComplete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "finished downloading") {
			t.Errorf("Expected completion message, got %s", string(body))
		}
		if got := r.Header.Get("Title"); got != "Download Complete" {
			t.Errorf("Expected Title 'Download Complete', got %s", got)
		}
		if got := r.Header.Get("X-Message-ID"); got != "qbit-123" {
			t.Errorf("Expected X-Message-ID 'qbit-123', got %s", got)
		}
		if got := r.Header.Get("Message-ID"); got != "qbit-123" {
			t.Errorf("Expected Message-ID 'qbit-123', got %s", got)
		}
		if got := r.Header.Get("X-Sequence-ID"); got != "qbit-123" {
			t.Errorf("Expected X-Sequence-ID 'qbit-123', got %s", got)
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer:   ts.URL,
		NtfyTopic:    "test_topic",
		NtfyPrioComp: "3",
	}

	torrent := &Torrent{Hash: "123", Name: "Test"}
	sendComplete(cfg, torrent)
}

func TestFormatGroupedUpdate(t *testing.T) {
	cfg := &Config{ProgressFormat: "bar"}
	torrents := []Torrent{
		{Hash: "1", Name: "Ubuntu.iso", Progress: 0.5, DlSpeed: 10485760, Eta: 30}, // 10 MB/s
		{Hash: "2", Name: "Debian.iso", Progress: 0.8, DlSpeed: 5242880, Eta: 15},  // 5 MB/s
	}

	title, msg := formatGroupedUpdate(cfg, torrents)
	if !strings.Contains(title, "Downloading (2 items) • 15.0 MB/s") {
		t.Errorf("unexpected title: %q", title)
	}
	if !strings.Contains(msg, "Ubuntu.iso") || !strings.Contains(msg, "Debian.iso") {
		t.Errorf("missing torrent names in msg: %q", msg)
	}
	if !strings.Contains(msg, "[█████░░░░░]") {
		t.Errorf("expected bar in msg: %q", msg)
	}

	// Single torrent
	single := []Torrent{{Hash: "1", Name: "Ubuntu.iso", Progress: 0.5, DlSpeed: 10485760, Eta: 30}}
	titleSingle, _ := formatGroupedUpdate(cfg, single)
	if !strings.Contains(titleSingle, "Downloading (1 item) • 10.0 MB/s") {
		t.Errorf("unexpected single title: %q", titleSingle)
	}

	// Percent format
	cfg.ProgressFormat = "percent"
	_, msgPercent := formatGroupedUpdate(cfg, single)
	if !strings.Contains(msgPercent, "50% • 10.0 MB/s") {
		t.Errorf("unexpected percent msg: %q", msgPercent)
	}
}

func TestSendGroupedUpdateAndComplete(t *testing.T) {
	var receivedTitle, receivedID string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTitle = r.Header.Get("Title")
		receivedID = r.Header.Get("X-Message-ID")
		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer:     ts.URL,
		NtfyTopic:      "test_topic",
		NtfyLiveID:     "live-id-123",
		NtfyPrioProg:   "2",
		ProgressFormat: "bar",
	}

	// Test SendGroupedUpdate
	sendGroupedUpdate(cfg, []Torrent{{Hash: "1", Name: "Test.iso", Progress: 0.5, DlSpeed: 1024, Eta: 10}})
	if !strings.Contains(receivedTitle, "Downloading (1 item)") {
		t.Errorf("unexpected received title: %s", receivedTitle)
	}
	if receivedID != "live-id-123" {
		t.Errorf("expected ID 'live-id-123', got %s", receivedID)
	}

	// Test SendGroupedComplete
	sendGroupedComplete(cfg)
	if receivedTitle != "Downloads Finished" {
		t.Errorf("unexpected completion title: %s", receivedTitle)
	}
	if receivedID != "live-id-123" {
		t.Errorf("expected ID 'live-id-123', got %s", receivedID)
	}
}
