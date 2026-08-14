package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

type healthState struct {
	consecutiveErrors int
	alertSent         bool
}

func (h *healthState) recordSuccess(cfg *Config) {
	if h.alertSent {
		log.Println("Health: qBittorrent connection recovered.")
		if cfg.NotifyHealthErrors {
			sendHealthAlert(cfg, "qBittorrent Reconnected", "Connection to qBittorrent API has been restored.", "white_check_mark", "3")
		}
		h.alertSent = false
	}
	h.consecutiveErrors = 0
}

func (h *healthState) recordError(cfg *Config, errDesc string) {
	h.consecutiveErrors++
	if h.consecutiveErrors >= 5 && !h.alertSent {
		log.Printf("Health: qBittorrent unreachable for 5 consecutive attempts (%s)", errDesc)
		if cfg.NotifyHealthErrors {
			sendHealthAlert(cfg, "qBittorrent Unreachable", "Unable to reach qBittorrent API after 5 attempts: "+errDesc, "warning", "4")
		}
		h.alertSent = true
	}
}

func (a *App) startupScan() {
	defer a.Wg.Done()

	// Retry loop to wait for qBittorrent to be ready
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	for {
		// Check for shutdown
		select {
		case <-a.Ctx.Done():
			return
		default:
		}

		log.Println("Startup: Attempting to connect to qBittorrent...")

		// Helper for interruptible sleep
		sleepOrExit := func(d time.Duration) bool {
			select {
			case <-time.After(d):
				return false
			case <-a.Ctx.Done():
				return true
			}
		}

		// 1. Auth (if required)
		if a.Config.QbitUser != "" && a.Config.QbitPass != "" {
			if err := login(client, a.Config); err != nil {
				log.Printf("Startup: Auth failed (%v). Retrying in 10s...", err)
				if sleepOrExit(10 * time.Second) {
					return
				}
				continue
			}
		}

		// 2. Fetch Active Torrents
		resp, err := client.Get(a.Config.QbitHost + "/api/v2/torrents/info?filter=downloading")
		if err != nil {
			log.Printf("Startup: Connection failed (%v). Retrying in 10s...", err)
			if sleepOrExit(10 * time.Second) {
				return
			}
			continue
		}

		if resp.StatusCode != 200 {
			log.Printf("Startup: API returned %d. Retrying in 10s...", resp.StatusCode)
			_ = resp.Body.Close()
			if sleepOrExit(10 * time.Second) {
				return
			}
			continue
		}

		var torrents []Torrent
		if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
			log.Printf("Startup: JSON decode error (%v). Retrying in 10s...", err)
			_ = resp.Body.Close()
			if sleepOrExit(10 * time.Second) {
				return
			}
			continue
		}
		_ = resp.Body.Close()

		// 3. Sync
		log.Printf("Startup: Found %d active downloads. Syncing...", len(torrents))
		a.Mutex.Lock()
		if a.ActiveMonitors == nil {
			a.ActiveMonitors = make(map[string]bool)
		}
		var toTrack []string
		for _, t := range torrents {
			if !a.ActiveMonitors[t.Hash] {
				a.ActiveMonitors[t.Hash] = true
				toTrack = append(toTrack, t.Hash)
				log.Printf("Startup: Resuming monitor for %q (%q)", t.Name, t.Hash)
			}
		}
		a.Mutex.Unlock()

		if a.Config.NotificationMode == "grouped" {
			a.Wg.Add(1)
			go a.runGroupedCoordinator()
			a.wakeCoordinator()
		} else {
			for _, hash := range toTrack {
				a.Wg.Add(1)
				go a.trackTorrent(hash)
			}
		}

		log.Println("Startup: Sync complete.")
		return
	}
}

func (a *App) runAutoDiscovery() {
	defer a.Wg.Done()

	if a.Config.AutoDiscoveryInt <= 0 {
		return
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	if a.Config.QbitUser != "" && a.Config.QbitPass != "" {
		_ = login(client, a.Config)
	}

	ticker := time.NewTicker(a.Config.AutoDiscoveryInt)
	defer ticker.Stop()

	for {
		select {
		case <-a.Ctx.Done():
			return
		case <-ticker.C:
		}

		resp, err := client.Get(a.Config.QbitHost + "/api/v2/torrents/info?filter=downloading")
		if err != nil {
			log.Printf("Auto-Discovery: Connection failed: %v", err)
			continue
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			_ = resp.Body.Close()
			if a.Config.QbitUser != "" && a.Config.QbitPass != "" {
				_ = login(client, a.Config)
			}
			continue
		}
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			continue
		}

		var downloading []Torrent
		if err := json.NewDecoder(resp.Body).Decode(&downloading); err != nil {
			_ = resp.Body.Close()
			continue
		}
		_ = resp.Body.Close()

		a.Mutex.Lock()
		if a.ActiveMonitors == nil {
			a.ActiveMonitors = make(map[string]bool)
		}
		var newlyDiscovered []string
		for _, t := range downloading {
			if !a.ActiveMonitors[t.Hash] && (a.Completed == nil || !a.Completed[t.Hash]) {
				a.ActiveMonitors[t.Hash] = true
				newlyDiscovered = append(newlyDiscovered, t.Hash)
				log.Printf("Auto-Discovery: Discovered untracked torrent %q (%q)", t.Name, t.Hash)
			}
		}
		a.Mutex.Unlock()

		if len(newlyDiscovered) > 0 {
			if a.Config.NotificationMode == "grouped" {
				a.wakeCoordinator()
			} else {
				for _, hash := range newlyDiscovered {
					a.Wg.Add(1)
					go a.trackTorrent(hash)
				}
			}
		}
	}
}

func (a *App) handleTorrentCompleted(t *Torrent) {
	a.Mutex.Lock()
	delete(a.ActiveMonitors, t.Hash)
	if a.Completed == nil {
		a.Completed = make(map[string]bool)
	}
	alreadyNotified := a.Completed[t.Hash]
	if !alreadyNotified {
		a.Completed[t.Hash] = true
	}
	a.Mutex.Unlock()

	if !alreadyNotified {
		log.Printf("[%q] Torrent finished (%q).", t.Hash, t.Name)
		if a.Config.NotifyComplete {
			sendComplete(a.Config, t)
		}
	}
}

func (a *App) runGroupedCoordinator() {
	defer a.Wg.Done()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	if a.Config.QbitUser != "" && a.Config.QbitPass != "" {
		if err := login(client, a.Config); err != nil {
			log.Printf("Coordinator: Auth failed: %v", err)
		}
	}

	ticker := time.NewTicker(a.Config.GroupUpdateInt)
	defer ticker.Stop()

	liveActive := false
	health := &healthState{}

	for {
		select {
		case <-a.Ctx.Done():
			log.Println("Coordinator: Shutting down...")
			return
		case <-a.WakeCh:
		case <-ticker.C:
		}

		a.Mutex.Lock()
		activeCount := len(a.ActiveMonitors)
		a.Mutex.Unlock()

		if activeCount == 0 {
			if liveActive {
				sendGroupedComplete(a.Config)
				liveActive = false
			}
			continue
		}

		resp, err := client.Get(a.Config.QbitHost + "/api/v2/torrents/info?filter=downloading")
		if err != nil {
			log.Printf("Coordinator: Connection error: %v", err)
			health.recordError(a.Config, err.Error())
			continue
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			_ = resp.Body.Close()
			if a.Config.QbitUser != "" && a.Config.QbitPass != "" {
				_ = login(client, a.Config)
			}
			health.recordError(a.Config, fmt.Sprintf("HTTP %d", resp.StatusCode))
			continue
		}
		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			log.Printf("Coordinator: API returned %d", resp.StatusCode)
			health.recordError(a.Config, fmt.Sprintf("HTTP %d", resp.StatusCode))
			continue
		}

		health.recordSuccess(a.Config)

		var downloading []Torrent
		if err := json.NewDecoder(resp.Body).Decode(&downloading); err != nil {
			_ = resp.Body.Close()
			log.Printf("Coordinator: JSON decode error: %v", err)
			continue
		}
		_ = resp.Body.Close()

		downloadingMap := make(map[string]Torrent, len(downloading))
		for _, t := range downloading {
			downloadingMap[t.Hash] = t
		}

		a.Mutex.Lock()
		trackedHashes := make([]string, 0, len(a.ActiveMonitors))
		for h := range a.ActiveMonitors {
			trackedHashes = append(trackedHashes, h)
		}
		a.Mutex.Unlock()

		var activeList []Torrent
		for _, h := range trackedHashes {
			t, isDownloading := downloadingMap[h]
			if isDownloading {
				pct := int(t.Progress * 100)
				if pct >= 100 || strings.Contains(t.State, "up") || t.State == "completed" {
					a.handleTorrentCompleted(&t)
				} else {
					activeList = append(activeList, t)
				}
			} else {
				tInfo, err := getTorrentInfo(client, a.Config, h)
				if err != nil {
					log.Printf("Coordinator: Error fetching info for %q: %v (will retry)", h, err)
					continue
				}
				if tInfo == nil {
					log.Printf("Coordinator: Torrent %q removed from qBittorrent. Stopping monitor.", h)
					a.Mutex.Lock()
					delete(a.ActiveMonitors, h)
					a.Mutex.Unlock()
					continue
				}

				pct := int(tInfo.Progress * 100)
				if pct >= 100 || strings.Contains(tInfo.State, "up") || tInfo.State == "completed" {
					a.handleTorrentCompleted(tInfo)
				} else {
					activeList = append(activeList, *tInfo)
				}
			}
		}

		if len(activeList) > 0 {
			if a.Config.NotifyProgress {
				sendGroupedUpdate(a.Config, activeList)
				liveActive = true
			}
		} else {
			if liveActive {
				sendGroupedComplete(a.Config)
				liveActive = false
			}
		}
	}
}

func (a *App) trackTorrent(hash string) {
	defer a.Wg.Done()
	defer func() {
		a.Mutex.Lock()
		delete(a.ActiveMonitors, hash)
		a.Mutex.Unlock()
	}()

	// Per-routine client to handle independent auth sessions cleanly
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	// Login only if credentials are provided
	if a.Config.QbitUser != "" && a.Config.QbitPass != "" {
		if err := login(client, a.Config); err != nil {
			log.Printf("[%q] Auth failed: %v", hash, err)
			return
		}
	}

	ticker := time.NewTicker(a.Config.PollInt)
	defer ticker.Stop()

	// Fetch info immediately to get the name for logging
	startInfo, err := getTorrentInfo(client, a.Config, hash)
	if err == nil && startInfo != nil {
		log.Printf("[%q] Monitor started for: %q", hash, startInfo.Name)
	} else {
		log.Printf("[%q] Monitor started (name pending...)", hash)
	}

	lastPct := -1
	var lastSentTime time.Time
	health := &healthState{}

	for {
		select {
		case <-a.Ctx.Done():
			log.Printf("[%q] Shutting down monitor...", hash)
			return
		case <-ticker.C:
		}

		t, err := getTorrentInfo(client, a.Config, hash)
		if err != nil {
			log.Printf("[%q] Error: %v", hash, err)
			health.recordError(a.Config, err.Error())
			if a.Config.QbitUser != "" && a.Config.QbitPass != "" && (strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "403")) {
				_ = login(client, a.Config)
			}
			continue
		}
		if t == nil {
			log.Printf("[%q] Torrent removed. Stopping.", hash)
			return
		}

		health.recordSuccess(a.Config)

		pct := int(t.Progress * 100)

		// Check Completion
		if pct >= 100 || strings.Contains(t.State, "up") || t.State == "completed" {
			a.handleTorrentCompleted(t)
			return
		}

		// Update Notification if progress changed
		if a.Config.NotifyProgress && a.Config.NotificationMode == "individual" {
			shouldUpdate := false
			if lastPct == -1 {
				shouldUpdate = true
			} else if pct-lastPct >= a.Config.ProgressStep {
				shouldUpdate = true
			}

			if shouldUpdate && (lastSentTime.IsZero() || time.Since(lastSentTime) >= a.Config.MinUpdateInt) {
				lastPct = pct
				lastSentTime = time.Now()
				sendUpdate(a.Config, t, pct)
			}
		}
	}
}
