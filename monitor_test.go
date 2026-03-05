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

	// Override global config
	oldHost := qbitHost
	oldUser := qbitUser
	oldPass := qbitPass
	oldPollInt := pollInt
	t.Cleanup(func() {
		qbitHost = oldHost
		qbitUser = oldUser
		qbitPass = oldPass
		pollInt = oldPollInt
	})

	qbitHost = ts.URL
	qbitUser = "admin"
	qbitPass = "adminadmin"
	pollInt = 100 * time.Millisecond // fast polling for tests

	// Reset global state
	activeMonitors = make(map[string]bool)
	appWg = sync.WaitGroup{}
	appCtx, appCancel = context.WithCancel(context.Background())
	defer appCancel() // Ensure cleanup

	// Run startupScan
	appWg.Add(1)
	startupScan()

	// Verify state
	mutex.Lock()
	count := len(activeMonitors)
	hasHash1 := activeMonitors["hash1"]
	hasHash2 := activeMonitors["hash2"]
	mutex.Unlock()

	if count != 2 {
		t.Errorf("expected 2 monitors, got %d", count)
	}
	if !hasHash1 || !hasHash2 {
		t.Errorf("missing expected hashes in activeMonitors map")
	}

	// Wait for workers to cleanly exit via context cancellation
	appCancel()

	// Use a channel to prevent testing deadlocks
	done := make(chan struct{})
	go func() {
		appWg.Wait()
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

	oldNtfy := ntfyServer
	ntfyServer = ntfyTs.URL
	oldHost := qbitHost
	qbitHost = ts.URL
	oldPollInt := pollInt
	pollInt = 10 * time.Millisecond // very fast trigger

	t.Cleanup(func() {
		qbitHost = oldHost
		ntfyServer = oldNtfy
		pollInt = oldPollInt
	})

	// Reset state
	activeMonitors = make(map[string]bool)
	activeMonitors["testhash"] = true
	appWg = sync.WaitGroup{}
	appCtx, appCancel = context.WithCancel(context.Background())
	defer appCancel()

	// Start tracking
	appWg.Add(1)
	go trackTorrent("testhash")

	// Allow first mock response to process, then advance stage
	time.Sleep(50 * time.Millisecond)

	stageMutex.Lock()
	stage = 1
	stageMutex.Unlock()

	// Wait for completion logic to kick in and exit the goroutine
	done := make(chan struct{})
	go func() {
		appWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if !ntfyCalled {
			t.Error("Expected Ntfy to be called on completion")
		}

		mutex.Lock()
		active := activeMonitors["testhash"]
		mutex.Unlock()
		if active {
			t.Error("Expected monitor to be removed from activeMonitors map")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("trackTorrent failed to exit upon completion")
	}
}
