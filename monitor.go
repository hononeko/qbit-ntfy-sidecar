package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

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
		for _, t := range torrents {
			a.ActiveMonitors[t.Hash] = true
			log.Printf("Startup: Resuming monitor for %q (%q)", t.Name, t.Hash)
		}
		a.Mutex.Unlock()

		if a.Config.NotificationMode == "grouped" {
			a.Wg.Add(1)
			go a.runGroupedCoordinator()
			a.wakeCoordinator()
		} else {
			for _, t := range torrents {
				a.Wg.Add(1)
				go a.trackTorrent(t.Hash)
			}
		}

		log.Println("Startup: Sync complete.")
		return
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
			log.Printf("Coordinator: API returned %d", resp.StatusCode)
			continue
		}

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
				if err == nil && tInfo != nil {
					pct := int(tInfo.Progress * 100)
					if pct >= 100 || strings.Contains(tInfo.State, "up") || tInfo.State == "completed" {
						a.handleTorrentCompleted(tInfo)
					} else {
						activeList = append(activeList, *tInfo)
					}
				} else {
					a.Mutex.Lock()
					delete(a.ActiveMonitors, h)
					a.Mutex.Unlock()
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
			continue
		}
		if t == nil {
			log.Printf("[%q] Torrent removed. Stopping.", hash)
			return
		}

		pct := int(t.Progress * 100)

		// Check Completion
		if pct >= 100 || strings.Contains(t.State, "up") || t.State == "completed" {
			log.Printf("[%q] Torrent finished (%q). Stopping monitor.", hash, t.Name)
			if a.Config.NotifyComplete {
				sendComplete(a.Config, t)
			}
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
