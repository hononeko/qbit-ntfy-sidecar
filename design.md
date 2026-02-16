Design Document: qBit-Ntfy Sidecar Monitor
1. Executive Summary
This document outlines the design and implementation of a lightweight, event-driven sidecar application to monitor qBittorrent downloads and send real-time progress notifications to ntfy.

Unlike traditional polling solutions that constantly query the API for all torrents, this solution uses an Event-Driven Sidecar Pattern. The Go application sits idle in the same Pod as qBittorrent and only spins up a monitoring routine when explicitly triggered by qBittorrent's "On Torrent Added" hook.

2. Architecture
2.1 Component Diagram
The solution utilizes the Kubernetes Sidecar Pattern. Both containers share the same network namespace (localhost) and volume mounts (optional, but not needed for this API-based approach).

2.2 Workflow
Trigger: User adds a torrent to qBittorrent.

Hook: qBittorrent executes an external command: curl -X POST http://localhost:9090/track?hash=%I.

Sidecar: The Go app receives the request and spawns a lightweight goroutine dedicated to tracking that specific InfoHash.

Monitor Loop:

The goroutine polls http://localhost:8080/api/v2/torrents/info?hashes=XYZ every 5 seconds.

If progress > last_progress: It sends a PUT to ntfy with the new percentage and an ASCII progress bar.

If complete (100%): It sends a "Download Complete" alert (High Priority) and terminates the goroutine.

3. Implementation Details
3.1 Go Application (main.go)
This source code is designed to be compiled into a static binary. It handles concurrency safely and uses a "Check-and-Set" mutex to prevent double-tracking the same hash.

Go
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
	qbitHost   = "http://localhost:8080" // Shared Pod Network
	qbitUser   string
	qbitPass   string
	ntfyTopic  string
	pollInt    = 5 * time.Second
)

// --- State ---
var (
	activeMonitors = make(map[string]bool)
	mutex          sync.Mutex
)

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
	
	// 1. Strict Config Check
	qbitUser = mustGetEnv("QBIT_USER")
	qbitPass = mustGetEnv("QBIT_PASS")
	ntfyTopic = mustGetEnv("NTFY_TOPIC")

	// 2. Start Trigger Server
	http.HandleFunc("/track", handleTrackRequest)
	
	log.Println("Sidecar listening on :9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}

func handleTrackRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing hash param", 400)
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
	// Cleanup on exit
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
		if pct >= 100 || t.State == "uploading" || t.State == "pausedUP" || t.State == "completed" {
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
	
	http.DefaultClient.Do(req)
}

func getTorrentInfo(client *http.Client, hash string) (*Torrent, error) {
	resp, err := client.Get(qbitHost + "/api/v2/torrents/info?hashes=" + hash)
	if err != nil { return nil, err }
	defer resp.Body.Close()

	var torrents []Torrent
	if err := json.NewDecoder(resp.Body).Decode(&torrents); err != nil { return nil, err }
	
	if len(torrents) == 0 { return nil, nil }
	return &torrents[0], nil
}

func login(client *http.Client) error {
	data := url.Values{}
	data.Set("username", qbitUser)
	data.Set("password", qbitPass)
	
	resp, err := client.PostForm(qbitHost+"/api/v2/auth/login", data)
	if err != nil { return err }
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || strings.Contains(string(body), "Fails.") {
		return fmt.Errorf("bad credentials")
	}
	return nil
}

func drawProgressBar(pct int) string {
	width := 10
	filled := int(math.Round(float64(pct) / 10.0))
	if filled > width { filled = width }
	empty := width - filled
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

func formatDuration(sec int) string {
	if sec >= 8640000 { return "∞" }
	return (time.Duration(sec) * time.Second).String()
}

func mustGetEnv(k string) string {
	v := os.Getenv(k)
	if v == "" { log.Fatalf("Missing ENV: %s", k) }
	return v
}
3.2 Dockerfile
Multi-stage build to create a minimal container image (<15MB).

Dockerfile
# Build Stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY main.go .
# -ldflags="-w -s" strips debug info for smaller binary
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o sidecar main.go

# Runtime Stage
# "static-debian12" includes root CA certs needed for HTTPS (ntfy.sh)
FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/sidecar /sidecar
CMD ["/sidecar"]
4. Kubernetes Integration
4.1 Deployment YAML
This manifest injects the sidecar into the qBittorrent pod.

YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: qbittorrent
  labels:
    app: qbittorrent
spec:
  replicas: 1
  template:
    spec:
      containers:
        # --- Main Container: qBittorrent ---
        - name: qbittorrent
          image: lscr.io/linuxserver/qbittorrent:latest
          ports:
            - containerPort: 8080
          env:
            - name: WEBUI_PORT
              value: "8080"
          volumeMounts:
            - name: config
              mountPath: /config
            - name: downloads
              mountPath: /data

        # --- Sidecar Container: Ntfy Monitor ---
        - name: ntfy-sidecar
          image: ghcr.io/vehkiya/qbit-ntfy-sidecar:latest
          imagePullPolicy: Always
          ports:
            - containerPort: 9090
          env:
            # localhost works because they share the Pod network
            - name: QBIT_HOST
              value: "http://localhost:8080"
            - name: QBIT_USER
              value: "admin"
            - name: QBIT_PASS
              valueFrom:
                secretKeyRef:
                  name: qbittorrent-secrets
                  key: password
            - name: NTFY_TOPIC
              value: "https://ntfy.kerrlab.app/downloads"
5. Configuration
5.1 qBittorrent Settings
Once deployed, configure the "Run external program" hook in qBittorrent to trigger the sidecar.

Open qBittorrent Web UI.

Navigate to Tools > Options > Downloads.

Check "Run external program on torrent added".

Enter the command:

Bash
curl -X POST "http://localhost:9090/track?hash=%I"
Note: %I is the InfoHash variable.

6. Future Considerations / Limitations
Restart Persistence: If the Pod restarts, active monitoring loops are lost. However, qBittorrent usually persists the "downloading" state. A future improvement could be a startup check: "On boot, query all downloading torrents and start monitoring them."

Security: The sidecar API (:9090) is unauthenticated. Since it is only exposed on localhost (not via a Service/Ingress), it is secure from outside the Pod, but theoretically accessible by the qBittorrent container (which is the intended behavior).