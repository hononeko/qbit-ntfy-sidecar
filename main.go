package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// --- Configuration ---
var (
	qbitHost  string
	qbitUser  string
	qbitPass  string
	ntfyTopic string
	pollInt   = 5 * time.Second
)

// --- State ---
var (
	activeMonitors = make(map[string]bool)
	mutex          sync.Mutex
)

// Torrent struct for JSON parsing
type Torrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	Progress float64 `json:"progress"`
	Eta      int     `json:"eta"`
	DlSpeed  int     `json:"dlspeed"`
	State    string  `json:"state"`
}

func main() {
	log.SetFlags(0) // K8s handles timestamps

	// 1. Config Check
	qbitHost = getEnv("QBIT_HOST", "http://localhost:8080")
	qbitUser = mustGetEnv("QBIT_USER")
	qbitPass = mustGetEnv("QBIT_PASS")
	ntfyTopic = mustGetEnv("NTFY_TOPIC")

	// 2. Start Trigger Server
	http.HandleFunc("/track", handleTrackRequest)

	port := "9090"
	log.Printf("Sidecar listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleTrackRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", 405)
		return
	}

	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing 'hash' query parameter", 400)
		return
	}

	mutex.Lock()
	if activeMonitors[hash] {
		mutex.Unlock()
		fmt.Fprintf(w, "Already tracking %s", hash)
		return
	}
	activeMonitors[hash] = true
	mutex.Unlock()

	go trackTorrent(hash)

	w.WriteHeader(200)
	fmt.Fprintf(w, "Tracking started for %s", hash)
}

func trackTorrent(hash string) {
	defer func() {
		mutex.Lock()
		delete(activeMonitors, hash)
		mutex.Unlock()
	}()

	log.Printf("[%s] Monitor started", hash)

	// Per-routine client to handle independent auth sessions cleanly
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	if err := login(client); err != nil {
		log.Printf("[%s] Auth failed: %v", hash, err)
		return
	}

	ticker := time.NewTicker(pollInt)
	defer ticker.Stop()

	lastPct := -1

	for range ticker.C {
		t, err := getTorrentInfo(client, hash)
		if err != nil {
			log.Printf("[%s] Error: %v", hash, err)
			continue
		}
		if t == nil {
			log.Printf("[%s] Torrent removed. Stopping.", hash)
			return
		}

		pct := int(t.Progress * 100)

		// Update Notification if progress changed
		if pct > lastPct {
			lastPct = pct
			sendUpdate(t, pct)
		}

		// Check Completion
		// qBittorrent states: upload, uploading, upLO, pausedUP, completed, etc.
		if pct >= 100 || strings.Contains(t.State, "up") || t.State == "completed" {
			sendComplete(t)
			return
		}
	}
}

func sendUpdate(t *Torrent, pct int) {
	bar := drawProgressBar(pct)
	speed := float64(t.DlSpeed) / 1024 / 1024
	eta := formatDuration(t.Eta)

	msg := fmt.Sprintf("%d%% %s\nSpeed: %.1f MB/s\nETA: %s", pct, bar, speed, eta)

	// Priority 'default' (3) is silent on most clients
	sendNtfy(t.Name, msg, "arrow_down", "qbit-"+t.Hash, "default")
}

func sendComplete(t *Torrent) {
	// Priority 'high' (4) triggers vibration/sound
	sendNtfy("Download Complete", t.Name+" has finished downloading.", "white_check_mark", "qbit-"+t.Hash, "high")
}

func sendNtfy(title, msg, tag, id, priority string) {
	req, _ := http.NewRequest("POST", ntfyTopic, strings.NewReader(msg))
	req.Header.Set("Title", title)
	req.Header.Set("Tags", tag)
	req.Header.Set("Priority", priority)
	req.Header.Set("X-Notification-ID", id) // Updates in-place

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Failed to send ntfy notification: %v", err)
		return
	}
	defer resp.Body.Close()
}

func getTorrentInfo(client *http.Client, hash string) (*Torrent, error) {
	resp, err := client.Get(qbitHost + "/api/v2/torrents/info?hashes=" + hash)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("qBit API returned status: %d", resp.StatusCode)
	}

	var torrents []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil {
		return nil, err
	}

	if len(torrents) == 0 {
		return nil, nil
	}
	return &torrents[0], nil
}

func login(client *http.Client) error {
	data := url.Values{}
	data.Set("username", qbitUser)
	data.Set("password", qbitPass)

	resp, err := client.PostForm(qbitHost+"/api/v2/auth/login", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || strings.Contains(string(body), "Fails.") {
		return fmt.Errorf("bad credentials or connection failed")
	}
	return nil
}

func drawProgressBar(pct int) string {
	width := 10
	filled := int(math.Round(float64(pct) / 10.0))
	if filled > width {
		filled = width
	}
	empty := width - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

func formatDuration(sec int) string {
	if sec >= 8640000 {
		return "∞"
	}
	return (time.Duration(sec) * time.Second).String()
}

func mustGetEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("Missing ENV: %s", k)
	}
	return v
}

func getEnv(k, fallback string) string {
	v := os.Getenv(k)
	if v == "" {
		return fallback
	}
	return v
}
