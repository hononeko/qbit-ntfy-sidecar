package main

import (
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
}

func TestSendNtfy_Error(t *testing.T) {
	cfg := &Config{NtfyServer: "http://127.0.0.1:0"} // Invalid URL triggers failure
	// This should log an error and return without panicking
	sendNtfy(cfg, "Fail", "Fail", "tag", "1", "3")
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
