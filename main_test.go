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
		{5, "[█░░░░░░░░░]"}, // Rounds up/down logic check
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
	// Mock qBittorrent Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/v2/torrents/info") {
			w.WriteHeader(200)
			fmt.Fprintln(w, `[{"hash":"123","name":"Test Torrent","progress":0.5,"eta":60,"dlspeed":1024,"state":"downloading"}]`)
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
	if err != nil {
		t.Fatalf("getTorrentInfo failed: %v", err)
	}

	if torrent == nil {
		t.Fatal("Expected torrent, got nil")
	}
	if torrent.Hash != "123" {
		t.Errorf("Expected hash 123, got %s", torrent.Hash)
	}
	if torrent.Progress != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", torrent.Progress)
	}
}

func TestSendNtfy(t *testing.T) {
	// Mock Ntfy Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.Header.Get("Title") != "Test Title" {
			t.Errorf("Expected Title 'Test Title', got '%s'", r.Header.Get("Title"))
		}
		if r.Header.Get("Priority") != "3" {
			t.Errorf("Expected Priority '3', got '%s'", r.Header.Get("Priority"))
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	// Override global config
	oldServer := ntfyServer
	oldTopic := ntfyTopic
	ntfyServer = ts.URL
	ntfyTopic = "test_topic"
	defer func() {
		ntfyServer = oldServer
		ntfyTopic = oldTopic
	}()

	sendNtfy("Test Title", "Test Message", "tag", "id", "3")
}
