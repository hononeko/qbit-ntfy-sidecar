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
		for _, t := range torrents {
			a.Mutex.Lock()
			if !a.ActiveMonitors[t.Hash] {
				a.ActiveMonitors[t.Hash] = true
				a.Mutex.Unlock()
				log.Printf("Startup: Resuming monitor for %q (%q)", t.Name, t.Hash)
				a.Wg.Add(1)
				go a.trackTorrent(t.Hash)
			} else {
				a.Mutex.Unlock()
			}
		}

		log.Println("Startup: Sync complete.")
		return
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
	// We'll retry in the loop if this fails, but it's nice to log early if possible
	startInfo, err := getTorrentInfo(client, a.Config, hash)
	if err == nil && startInfo != nil {
		log.Printf("[%q] Monitor started for: %q", hash, startInfo.Name)
	} else {
		log.Printf("[%q] Monitor started (name pending...)", hash)
	}

	lastPct := -1

	for {
		select {
		case <-a.Ctx.Done():
			log.Printf("[%q] Shutting down monitor...", hash)
			return
		case <-ticker.C:
			// Continue with logic below
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

		// Update Notification if progress changed
		if pct > lastPct {
			lastPct = pct
			sendUpdate(a.Config, t, pct)
		}

		// Check Completion
		// qBittorrent states: upload, uploading, upLO, pausedUP, completed, etc.
		if pct >= 100 || strings.Contains(t.State, "up") || t.State == "completed" {
			log.Printf("[%q] Torrent finished (%q). Stopping monitor.", hash, t.Name)
			if a.Config.NotifyComplete {
				sendComplete(a.Config, t)
			}
			return
		}
	}
}
