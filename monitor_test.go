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

func TestStartupScan_Grouped(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "login") {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, "Ok.")
			return
		}

		if strings.Contains(r.URL.Path, "torrents/info") {
			w.WriteHeader(200)
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
			QbitHost:         ts.URL,
			QbitUser:         "admin",
			QbitPass:         "adminadmin",
			PollInt:          100 * time.Millisecond,
			GroupUpdateInt:   100 * time.Millisecond,
			NotificationMode: "grouped",
		},
		ActiveMonitors: make(map[string]bool),
		Completed:      make(map[string]bool),
		WakeCh:         make(chan struct{}, 1),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel()

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

	app.Cancel()

	done := make(chan struct{})
	go func() {
		app.Wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("startupScan or coordinator failed to exit gracefully")
	}
}

func TestStartupScan_Individual(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "login") {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, "Ok.")
			return
		}

		if strings.Contains(r.URL.Path, "torrents/info") {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, `[
				{"hash":"hash1","name":"Torrent 1","progress":0.5,"eta":60,"dlspeed":1024,"state":"downloading"}
			]`)
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			QbitHost:         ts.URL,
			PollInt:          50 * time.Millisecond,
			NotificationMode: "individual",
		},
		ActiveMonitors: make(map[string]bool),
		Completed:      make(map[string]bool),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel()

	app.Wg.Add(1)
	app.startupScan()

	app.Mutex.Lock()
	hasHash1 := app.ActiveMonitors["hash1"]
	app.Mutex.Unlock()

	if !hasHash1 {
		t.Errorf("expected hash1 in activeMonitors")
	}

	app.Cancel()
	app.Wg.Wait()
}

func TestGroupedCoordinator_Flow(t *testing.T) {
	var stage int
	var stageMutex sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stageMutex.Lock()
		curStage := stage
		stageMutex.Unlock()

		w.WriteHeader(200)
		switch curStage {
		case 0:
			// Active downloading
			_, _ = fmt.Fprintln(w, `[
				{"hash":"t1","name":"Torrent 1","progress":0.4,"eta":50,"dlspeed":1048576,"state":"downloading"}
			]`)
		case 1:
			// Completed
			_, _ = fmt.Fprintln(w, `[
				{"hash":"t1","name":"Torrent 1","progress":1.0,"eta":0,"dlspeed":0,"state":"completed"}
			]`)
		case 2:
			// Empty (no active downloads)
			_, _ = fmt.Fprintln(w, `[]`)
		}
	}))
	defer ts.Close()

	var ntfyTitles []string
	var ntfyMutex sync.Mutex
	ntfyTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ntfyMutex.Lock()
		ntfyTitles = append(ntfyTitles, r.Header.Get("Title"))
		ntfyMutex.Unlock()
		w.WriteHeader(200)
	}))
	defer ntfyTs.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			NtfyServer:       ntfyTs.URL,
			NtfyTopic:        "test",
			QbitHost:         ts.URL,
			GroupUpdateInt:   20 * time.Millisecond,
			NotificationMode: "grouped",
			NotifyProgress:   true,
			NotifyComplete:   true,
			NtfyLiveID:       "qbit-live-test",
			ProgressFormat:   "bar",
		},
		ActiveMonitors: map[string]bool{"t1": true},
		Completed:      make(map[string]bool),
		WakeCh:         make(chan struct{}, 1),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel()

	app.Wg.Add(1)
	go app.runGroupedCoordinator()

	// Initial active progress notification
	time.Sleep(60 * time.Millisecond)

	// Move to stage 1 (completed)
	stageMutex.Lock()
	stage = 1
	stageMutex.Unlock()
	app.wakeCoordinator()
	time.Sleep(60 * time.Millisecond)

	// Move to stage 2 (drained)
	stageMutex.Lock()
	stage = 2
	stageMutex.Unlock()
	app.wakeCoordinator()
	time.Sleep(60 * time.Millisecond)

	app.Cancel()
	app.Wg.Wait()

	ntfyMutex.Lock()
	defer ntfyMutex.Unlock()

	hasGroupUpdate := false
	hasComplete := false
	for _, title := range ntfyTitles {
		if strings.Contains(title, "Downloading (1 item)") {
			hasGroupUpdate = true
		}
		if title == "Download Complete" {
			hasComplete = true
		}
	}

	if !hasGroupUpdate {
		t.Errorf("expected grouped live update title, got: %v", ntfyTitles)
	}
	if !hasComplete {
		t.Errorf("expected completion notification, got: %v", ntfyTitles)
	}
}

func TestGroupedCoordinator_NetworkErrorResilience(t *testing.T) {
	var stage int
	var stageMutex sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stageMutex.Lock()
		curStage := stage
		stageMutex.Unlock()

		// Not in filter=downloading
		if strings.Contains(r.URL.Path, "filter=downloading") {
			w.WriteHeader(200)
			_, _ = fmt.Fprintln(w, `[]`)
			return
		}

		// Individual query for hash
		if strings.Contains(r.URL.Path, "torrents/info") {
			switch curStage {
			case 0:
				// Simulate transient error 500
				w.WriteHeader(500)
			case 1:
				// Success (completed)
				w.WriteHeader(200)
				_, _ = fmt.Fprintln(w, `[{"hash":"t1","name":"Torrent 1","progress":1.0,"eta":0,"dlspeed":0,"state":"completed"}]`)
			}
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	var completionSent bool
	var ntfyMutex sync.Mutex
	ntfyTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ntfyMutex.Lock()
		if r.Header.Get("Title") == "Download Complete" {
			completionSent = true
		}
		ntfyMutex.Unlock()
		w.WriteHeader(200)
	}))
	defer ntfyTs.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			NtfyServer:       ntfyTs.URL,
			NtfyTopic:        "test",
			QbitHost:         ts.URL,
			GroupUpdateInt:   20 * time.Millisecond,
			NotificationMode: "grouped",
			NotifyComplete:   true,
		},
		ActiveMonitors: map[string]bool{"t1": true},
		Completed:      make(map[string]bool),
		WakeCh:         make(chan struct{}, 1),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel()

	app.Wg.Add(1)
	go app.runGroupedCoordinator()

	// Stage 0: 500 error on hash query
	time.Sleep(50 * time.Millisecond)

	// Verify t1 is still tracked (not deleted due to 500 error)
	app.Mutex.Lock()
	stillTracked := app.ActiveMonitors["t1"]
	app.Mutex.Unlock()

	if !stillTracked {
		t.Error("Expected t1 to still be tracked after transient network error")
	}

	// Stage 1: Success on subsequent retry
	stageMutex.Lock()
	stage = 1
	stageMutex.Unlock()
	app.wakeCoordinator()
	time.Sleep(60 * time.Millisecond)

	app.Cancel()
	app.Wg.Wait()

	ntfyMutex.Lock()
	sent := completionSent
	ntfyMutex.Unlock()

	if !sent {
		t.Error("Expected completion notification after recovering from transient network error")
	}
}

func TestTrackTorrent_IndividualThrottling(t *testing.T) {
	var stage int
	var stageMutex sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stageMutex.Lock()
		curStage := stage
		stageMutex.Unlock()

		w.WriteHeader(200)
		switch curStage {
		case 0:
			_, _ = fmt.Fprintln(w, `[{"hash":"testhash","name":"Test Torrent","progress":0.10,"eta":60,"dlspeed":1024,"state":"downloading"}]`)
		case 1:
			// Small step (+5%), step threshold is 25%, so should not send
			_, _ = fmt.Fprintln(w, `[{"hash":"testhash","name":"Test Torrent","progress":0.15,"eta":50,"dlspeed":1024,"state":"downloading"}]`)
		case 2:
			// Big step (+30%), should send
			_, _ = fmt.Fprintln(w, `[{"hash":"testhash","name":"Test Torrent","progress":0.45,"eta":30,"dlspeed":1024,"state":"downloading"}]`)
		case 3:
			// Complete
			_, _ = fmt.Fprintln(w, `[{"hash":"testhash","name":"Test Torrent","progress":1.0,"eta":0,"dlspeed":0,"state":"completed"}]`)
		}
	}))
	defer ts.Close()

	var updateCount int
	var completeCount int
	var countMutex sync.Mutex
	ntfyTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		countMutex.Lock()
		if r.Header.Get("Title") == "Download Complete" {
			completeCount++
		} else {
			updateCount++
		}
		countMutex.Unlock()
		w.WriteHeader(200)
	}))
	defer ntfyTs.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			NtfyServer:       ntfyTs.URL,
			NtfyTopic:        "test",
			QbitHost:         ts.URL,
			PollInt:          10 * time.Millisecond,
			NotificationMode: "individual",
			NotifyProgress:   true,
			ProgressStep:     25,
			MinUpdateInt:     0,
			NotifyComplete:   true,
		},
		ActiveMonitors: map[string]bool{"testhash": true},
		Ctx:            appCtx,
		Cancel:         appCancel,
	}
	defer app.Cancel()

	app.Wg.Add(1)
	go app.trackTorrent("testhash")

	time.Sleep(30 * time.Millisecond) // Stage 0 sends initial (updateCount = 1)

	stageMutex.Lock()
	stage = 1
	stageMutex.Unlock()
	time.Sleep(30 * time.Millisecond) // Stage 1 skips step (< 25%)

	stageMutex.Lock()
	stage = 2
	stageMutex.Unlock()
	time.Sleep(30 * time.Millisecond) // Stage 2 sends (+30% >= 25%, updateCount = 2)

	stageMutex.Lock()
	stage = 3
	stageMutex.Unlock()
	time.Sleep(50 * time.Millisecond) // Stage 3 completes

	app.Cancel()
	app.Wg.Wait()

	countMutex.Lock()
	defer countMutex.Unlock()

	if updateCount != 2 {
		t.Errorf("expected 2 progress updates, got %d", updateCount)
	}
	if completeCount != 1 {
		t.Errorf("expected 1 completion notification, got %d", completeCount)
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
				w.WriteHeader(401)
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

			go func() {
				time.Sleep(50 * time.Millisecond)
				app.Cancel()
			}()

			app.Wg.Add(1)
			app.startupScan()
		})
	}

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
		app.trackTorrent("hash")
	})

	t.Run("Torrent Removed", func(t *testing.T) {
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
		app.trackTorrent("hash")
	})

	t.Run("API Error Loop Break", func(t *testing.T) {
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
			app.Cancel()
		}()

		app.Wg.Add(1)
		app.trackTorrent("hash")
	})
}

func TestAutoDiscovery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprintln(w, `[{"hash":"auto1","name":"Auto Torrent","progress":0.3,"state":"downloading"}]`)
	}))
	defer ts.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			QbitHost:         ts.URL,
			AutoDiscoveryInt: 20 * time.Millisecond,
			NotificationMode: "grouped",
		},
		ActiveMonitors: make(map[string]bool),
		Completed:      make(map[string]bool),
		WakeCh:         make(chan struct{}, 1),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}

	app.Wg.Add(1)
	go app.runAutoDiscovery()

	time.Sleep(60 * time.Millisecond)

	app.Mutex.Lock()
	found := app.ActiveMonitors["auto1"]
	app.Mutex.Unlock()

	app.Cancel()
	app.Wg.Wait()

	if !found {
		t.Error("Expected auto-discovery to track 'auto1'")
	}
}

func TestHealthAlerts(t *testing.T) {
	var alertTitles []string
	var alertMutex sync.Mutex
	ntfyTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alertMutex.Lock()
		alertTitles = append(alertTitles, r.Header.Get("Title"))
		alertMutex.Unlock()
		w.WriteHeader(200)
	}))
	defer ntfyTs.Close()

	cfg := &Config{
		NtfyServer:         ntfyTs.URL,
		NtfyTopic:          "test_health",
		NotifyHealthErrors: true,
	}

	h := &healthState{}

	// 4 errors: no alert sent
	for i := 0; i < 4; i++ {
		h.recordError(cfg, "connection timeout")
	}
	alertMutex.Lock()
	if len(alertTitles) != 0 {
		t.Errorf("expected 0 alerts after 4 errors, got %d", len(alertTitles))
	}
	alertMutex.Unlock()

	// 5th error: should trigger warning alert
	h.recordError(cfg, "connection timeout")
	alertMutex.Lock()
	if len(alertTitles) != 1 || alertTitles[0] != "qBittorrent Unreachable" {
		t.Errorf("expected unreachable alert on 5th error, got %v", alertTitles)
	}
	alertMutex.Unlock()

	// 6th error: no duplicate alert
	h.recordError(cfg, "connection timeout")
	alertMutex.Lock()
	if len(alertTitles) != 1 {
		t.Errorf("expected no duplicate unreachable alert, got %d", len(alertTitles))
	}
	alertMutex.Unlock()

	// Recovery: should trigger reconnected alert
	h.recordSuccess(cfg)
	alertMutex.Lock()
	if len(alertTitles) != 2 || alertTitles[1] != "qBittorrent Reconnected" {
		t.Errorf("expected reconnected alert on success, got %v", alertTitles)
	}
	alertMutex.Unlock()
}

func TestAutoDiscovery_Individual(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprintln(w, `[{"hash":"indiv1","name":"Individual Torrent","progress":0.5,"state":"downloading"}]`)
	}))
	defer ts.Close()

	appCtx, appCancel := context.WithCancel(context.Background())
	app := &App{
		Config: &Config{
			QbitHost:         ts.URL,
			AutoDiscoveryInt: 20 * time.Millisecond,
			NotificationMode: "individual",
			PollInt:          10 * time.Millisecond,
		},
		ActiveMonitors: make(map[string]bool),
		Completed:      make(map[string]bool),
		Ctx:            appCtx,
		Cancel:         appCancel,
	}

	app.Wg.Add(1)
	go app.runAutoDiscovery()

	time.Sleep(60 * time.Millisecond)

	app.Mutex.Lock()
	found := app.ActiveMonitors["indiv1"]
	app.Mutex.Unlock()

	app.Cancel()
	app.Wg.Wait()

	if !found {
		t.Error("Expected auto-discovery to track 'indiv1' in individual mode")
	}
}

func TestHealthAlerts_ResetBeforeThreshold(t *testing.T) {
	var alertTitles []string
	var alertMutex sync.Mutex
	ntfyTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		alertMutex.Lock()
		alertTitles = append(alertTitles, r.Header.Get("Title"))
		alertMutex.Unlock()
		w.WriteHeader(200)
	}))
	defer ntfyTs.Close()

	cfg := &Config{
		NtfyServer:         ntfyTs.URL,
		NtfyTopic:          "test_health",
		NotifyHealthErrors: true,
	}

	h := &healthState{}

	// 4 errors (not reaching 5)
	for i := 0; i < 4; i++ {
		h.recordError(cfg, "error")
	}

	// 1 success resets consecutive errors
	h.recordSuccess(cfg)

	// Another 4 errors (should not trigger because consecutiveErrors was reset)
	for i := 0; i < 4; i++ {
		h.recordError(cfg, "error")
	}

	alertMutex.Lock()
	count := len(alertTitles)
	alertMutex.Unlock()

	if count != 0 {
		t.Errorf("expected 0 alerts when counter is reset before reaching threshold, got %d", count)
	}
}
