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

func TestFormatGroupedUpdate_TopNOverflow(t *testing.T) {
	cfg := &Config{
		ProgressFormat:     "bar",
		MaxDisplayTorrents: 2,
	}

	torrents := []Torrent{
		{Hash: "1", Name: "Torrent 1", Progress: 0.2, DlSpeed: 1048576, Eta: 60},
		{Hash: "2", Name: "Torrent 2", Progress: 0.5, DlSpeed: 2097152, Eta: 30},
		{Hash: "3", Name: "Torrent 3", Progress: 0.8, DlSpeed: 3145728, Eta: 10},
		{Hash: "4", Name: "Torrent 4", Progress: 0.9, DlSpeed: 4194304, Eta: 5},
	}

	title, msg := formatGroupedUpdate(cfg, torrents)
	if !strings.Contains(title, "Downloading (4 items) • 10.0 MB/s") {
		t.Errorf("unexpected title: %q", title)
	}
	if !strings.Contains(msg, "Torrent 1") || !strings.Contains(msg, "Torrent 2") {
		t.Errorf("missing top-2 torrents in msg: %q", msg)
	}
	if strings.Contains(msg, "Torrent 3") || strings.Contains(msg, "Torrent 4") {
		t.Errorf("overflow torrents should not appear in body list: %q", msg)
	}
	if !strings.Contains(msg, "... and 2 more active (Total: 10.0 MB/s)") {
		t.Errorf("missing or incorrect overflow footer: %q", msg)
	}

	// Exact boundary test (len == MaxDisplayTorrents)
	exactTorrents := torrents[:2]
	_, msgExact := formatGroupedUpdate(cfg, exactTorrents)
	if strings.Contains(msgExact, "... and") {
		t.Errorf("exact match should not contain overflow footer: %q", msgExact)
	}
}

func TestFormatTorrentStatusBadge(t *testing.T) {
	paused := &Torrent{State: "pausedDL"}
	if badge := formatTorrentStatusBadge(paused); badge != " [⏸ Paused]" {
		t.Errorf("expected paused badge, got %q", badge)
	}

	stalled := &Torrent{State: "stalledDL", DlSpeed: 0, Progress: 0.5}
	if badge := formatTorrentStatusBadge(stalled); badge != " [⏳ Stalled]" {
		t.Errorf("expected stalled badge, got %q", badge)
	}

	stalledHeuristic := &Torrent{State: "downloading", DlSpeed: 0, Progress: 0.5}
	if badge := formatTorrentStatusBadge(stalledHeuristic); badge != " [⏳ Stalled]" {
		t.Errorf("expected stalled heuristic badge, got %q", badge)
	}

	normal := &Torrent{State: "downloading", DlSpeed: 1024, Progress: 0.5}
	if badge := formatTorrentStatusBadge(normal); badge != "" {
		t.Errorf("expected empty badge for active download, got %q", badge)
	}
}

func TestSendNtfy_ActionsAndClick(t *testing.T) {
	var receivedClick, receivedActions string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedClick = r.Header.Get("Click")
		receivedActions = r.Header.Get("Actions")
		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer:    ts.URL,
		NtfyTopic:     "test_topic",
		QbitPublicURL: "https://qbit.lan:8080",
	}

	sendNtfy(cfg, "Test Title", "Test Message", "arrow_down", "id", "2")
	if receivedClick != "https://qbit.lan:8080" {
		t.Errorf("expected Click 'https://qbit.lan:8080', got %q", receivedClick)
	}
	if !strings.Contains(receivedActions, "view, Open WebUI, https://qbit.lan:8080") {
		t.Errorf("expected Actions header to contain Open WebUI, got %q", receivedActions)
	}
}

func TestSendHealthAlert(t *testing.T) {
	var receivedTitle, receivedID, receivedPriority string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTitle = r.Header.Get("Title")
		receivedID = r.Header.Get("X-Message-ID")
		receivedPriority = r.Header.Get("Priority")
		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)

	cfg := &Config{
		NtfyServer: ts.URL,
		NtfyTopic:  "test_topic",
	}

	sendHealthAlert(cfg, "Test Health", "Message", "warning", "4")
	if receivedTitle != "Test Health" {
		t.Errorf("expected Title 'Test Health', got %q", receivedTitle)
	}
	if receivedID != "qbit-health-alert" {
		t.Errorf("expected ID 'qbit-health-alert', got %q", receivedID)
	}
	if receivedPriority != "4" {
		t.Errorf("expected Priority '4', got %q", receivedPriority)
	}
}
