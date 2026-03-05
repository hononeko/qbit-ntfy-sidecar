package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

	t.Run("Client Error", func(t *testing.T) {
		qbitHost = "http://127.0.0.1:0" // Invalid port causes immediate connection refused
		_, err := getTorrentInfo(http.DefaultClient, "123")
		if err == nil {
			t.Error("Expected error, got nil")
		}
	})
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name        string
		handler     func(w http.ResponseWriter, r *http.Request)
		expectError bool
	}{
		{
			name: "Success OK",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `Ok.`)
			},
			expectError: false,
		},
		{
			name: "Auth Fails",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `Fails.`)
			},
			expectError: true,
		},
		{
			name: "Server Error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer ts.Close()

			oldHost := qbitHost
			qbitHost = ts.URL
			defer func() { qbitHost = oldHost }()

			qbitUser = "admin"
			qbitPass = "adminadmin"

			err := login(ts.Client())
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}

	t.Run("Client Error", func(t *testing.T) {
		qbitHost = "http://127.0.0.1:0"
		err := login(http.DefaultClient)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}
