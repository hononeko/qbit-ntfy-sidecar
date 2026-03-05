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

func TestSendNtfy_Error(t *testing.T) {
	oldServer := ntfyServer
	ntfyServer = "http://127.0.0.1:0" // Invalid URL triggers failure
	t.Cleanup(func() { ntfyServer = oldServer })

	// This should log an error and return without panicking
	sendNtfy("Fail", "Fail", "tag", "1", "3")
}

func TestSendUpdate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg := string(body)

		if progressFormat == "percent" {
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

	oldServer := ntfyServer
	ntfyServer = ts.URL
	ntfyTopic = "test_topic"
	ntfyPrioProg = "2"
	t.Cleanup(func() { ntfyServer = oldServer })

	torrent := &Torrent{Hash: "123", Name: "Test", DlSpeed: 1048576, Eta: 60} // 1MB/s

	progressFormat = "bar"
	sendUpdate(torrent, 50)

	progressFormat = "percent"
	sendUpdate(torrent, 50)
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

	oldServer := ntfyServer
	ntfyServer = ts.URL
	ntfyTopic = "test_topic"
	ntfyPrioComp = "3"
	t.Cleanup(func() { ntfyServer = oldServer })

	torrent := &Torrent{Hash: "123", Name: "Test"}
	sendComplete(torrent)
}
