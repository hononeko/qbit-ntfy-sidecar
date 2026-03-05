package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartupScan(t *testing.T) {
	// Mock qBittorrent active downloads
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "login") {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, "Ok.")
			return
		}

		if strings.Contains(r.URL.Path, "torrents/info") {
			w.WriteHeader(200)
			// Return two active torrents
			_, _ = fmt.Fprintln(w, `[
				{"hash":"hash1","name":"Torrent 1","progress":0.5,"eta":60,"dlspeed":1024,"state":"downloading"},
				{"hash":"hash2","name":"Torrent 2","progress":0.8,"eta":120,"dlspeed":2048,"state":"downloading"}
			]`)
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			QbitHost: ts.URL,
			QbitUser: "admin",
			QbitPass: "adminadmin",
			PollInt:  100 * time.Millisecond,
		},
		ActiveMonitors: make(map[string]bool),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel() // Ensure cleanup

	// Run startupScan
	app.Wg.Add(1)
	app.startupScan()

	// Verify state
	app.Mutex.Lock()
	count := len(app.ActiveMonitors)
	hasHash1 := app.ActiveMonitors["hash1"]
	hasHash2 := app.ActiveMonitors["hash2"]
	app.Mutex.Unlock()

	if count != 2 {
		t.Errorf("expected 2 monitors, got %d", count)
	}
	if !hasHash1 || !hasHash2 {
		t.Errorf("missing expected hashes in activeMonitors map")
	}

	// Wait for workers to cleanly exit via context cancellation
	app.Cancel()

	// Use a channel to prevent testing deadlocks
	done := make(chan struct{})
	go func() {
		app.Wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("startupScan or workers failed to exit gracefully")
	}
}

func TestTrackTorrent(t *testing.T) {
	// Track state internally
	var stage int
	var stageMutex sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "login") {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, "Ok.")
			return
		}

		stageMutex.Lock()
		currentStage := stage
		stageMutex.Unlock()

		w.WriteHeader(200)
		switch currentStage {
		case 0:
			// Initial fetch (50% progress)
			_, _ = fmt.Fprintln(w, `[{"hash":"testhash","name":"Test Torrent","progress":0.5,"eta":60,"dlspeed":1024,"state":"downloading"}]`)
		case 1:
			// Second fetch (100% progress -> complete)
			_, _ = fmt.Fprintln(w, `[{"hash":"testhash","name":"Test Torrent","progress":1.0,"eta":0,"dlspeed":0,"state":"completed"}]`)
		}
	}))
	defer ts.Close()

	// Mock Ntfy to capture completion
	ntfyCalled := false
	ntfyTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ntfyCalled = true
		w.WriteHeader(200)
	}))
	defer ntfyTs.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			NtfyServer:     ntfyTs.URL,
			NtfyTopic:      "test",
			QbitHost:       ts.URL,
			PollInt:        10 * time.Millisecond,
			NotifyComplete: true,
		},
		ActiveMonitors: map[string]bool{"testhash": true},
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel()

	// Start tracking
	app.Wg.Add(1)
	go app.trackTorrent("testhash")

	// Allow first mock response to process, then advance stage
	time.Sleep(50 * time.Millisecond)

	stageMutex.Lock()
	stage = 1
	stageMutex.Unlock()

	// Wait for completion logic to kick in and exit the goroutine
	done := make(chan struct{})
	go func() {
		app.Wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if !ntfyCalled {
			t.Error("Expected Ntfy to be called on completion")
		}

		app.Mutex.Lock()
		active := app.ActiveMonitors["testhash"]
		app.Mutex.Unlock()
		if active {
			t.Error("Expected monitor to be removed from activeMonitors map")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("trackTorrent failed to exit upon completion")
	}
}

func TestStartupScan_Errors(t *testing.T) {
	tests := []struct {
		name    string
		handler func(w http.ResponseWriter, r *http.Request)
		auth    bool
	}{
		{
			name: "Auth Failure",
			auth: true,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(401) // mock auth fail string check
				_, _ = fmt.Fprintln(w, "Fails.")
			},
		},
		{
			name: "API Error 500",
			auth: false,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
			},
		},
		{
			name: "JSON Decode Error",
			auth: false,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `[{"hash": invalid_json`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(tt.handler))
			defer ts.Close()

			appCtx, appCancel := context.WithCancel(context.Background())
			app := &App{
				Config: &Config{
					QbitHost: ts.URL,
				},
				Ctx:    appCtx,
				Cancel: appCancel,
			}
			if tt.auth {
				app.Config.QbitUser = "admin"
				app.Config.QbitPass = "admin"
			}

			// We will cancel the context shortly after starting, which causes sleepOrExit to trigger the exit path naturally
			go func() {
				time.Sleep(50 * time.Millisecond) // Give it enough time to hit the error and enter sleepOrExit
				app.Cancel()
			}()

			app.Wg.Add(1)
			app.startupScan() // this will block until appCancel fires inside sleepOrExit
		})
	}

	// Test Connection Failed
	t.Run("Connection Failed", func(t *testing.T) {
		appCtx, appCancel := context.WithCancel(context.Background())
		app := &App{
			Config: &Config{
				QbitHost: "http://127.0.0.1:0",
			},
			Ctx:    appCtx,
			Cancel: appCancel,
		}

		go func() {
			time.Sleep(50 * time.Millisecond)
			app.Cancel()
		}()

		app.Wg.Add(1)
		app.startupScan()
	})
}

func TestTrackTorrent_Errors(t *testing.T) {
	t.Run("Auth Failure", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(401)
			_, _ = fmt.Fprintln(w, "Fails.")
		}))
		defer ts.Close()

		appCtx, appCancel := context.WithCancel(context.Background())
		app := &App{
			Config: &Config{
				QbitHost: ts.URL,
				QbitUser: "admin",
				QbitPass: "admin",
			},
			ActiveMonitors: make(map[string]bool),
			Ctx:            appCtx,
			Cancel:         appCancel,
		}

		app.Wg.Add(1)
		app.trackTorrent("hash") // should return immediately after login fails
	})

	t.Run("Torrent Removed", func(t *testing.T) {
		// Mock API returns empty array (not found)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, "[]")
		}))
		defer ts.Close()

		appCtx, appCancel := context.WithCancel(context.Background())
		app := &App{
			Config: &Config{
				QbitHost: ts.URL,
				PollInt:  10 * time.Millisecond,
			},
			ActiveMonitors: make(map[string]bool),
			Ctx:            appCtx,
			Cancel:         appCancel,
		}
		defer app.Cancel()

		app.Wg.Add(1)
		app.trackTorrent("hash") // should fetch once, get nil, log removed and return
	})

	t.Run("API Error Loop Break", func(t *testing.T) {
		// Mock API returning 500, simulating error in track loop
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		defer ts.Close()

		appCtx, appCancel := context.WithCancel(context.Background())
		app := &App{
			Config: &Config{
				QbitHost: ts.URL,
				PollInt:  10 * time.Millisecond,
			},
			ActiveMonitors: make(map[string]bool),
			Ctx:            appCtx,
			Cancel:         appCancel,
		}

		go func() {
			time.Sleep(50 * time.Millisecond)
			app.Cancel() // Break the loop
		}()

		app.Wg.Add(1)
		app.trackTorrent("hash") // fetch fails, continues loop, then cancel triggers break
	})
}
