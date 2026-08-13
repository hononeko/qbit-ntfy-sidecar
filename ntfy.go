package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

var ntfyClient = &http.Client{Timeout: 5 * time.Second}

func formatGroupedUpdate(cfg *Config, torrents []Torrent) (string, string) {
	totalSpeed := 0
	for _, t := range torrents {
		totalSpeed += t.DlSpeed
	}
	totalSpeedMB := float64(totalSpeed) / 1024 / 1024

	var title string
	if len(torrents) == 1 {
		title = fmt.Sprintf("Downloading (1 item) • %.1f MB/s", totalSpeedMB)
	} else {
		title = fmt.Sprintf("Downloading (%d items) • %.1f MB/s", len(torrents), totalSpeedMB)
	}

	var sb strings.Builder
	for i, t := range torrents {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		pct := int(t.Progress * 100)
		speed := float64(t.DlSpeed) / 1024 / 1024
		eta := formatDuration(t.Eta)

		sb.WriteString(t.Name)
		sb.WriteString("\n")
		if cfg.ProgressFormat == "percent" {
			fmt.Fprintf(&sb, "%d%% • %.1f MB/s • ETA: %s", pct, speed, eta)
		} else {
			bar := drawProgressBar(pct)
			fmt.Fprintf(&sb, "%s %d%% • %.1f MB/s • ETA: %s", bar, pct, speed, eta)
		}
	}

	return title, sb.String()
}

func sendGroupedUpdate(cfg *Config, torrents []Torrent) {
	if len(torrents) == 0 {
		return
	}
	title, msg := formatGroupedUpdate(cfg, torrents)
	sendNtfy(cfg, title, msg, "arrow_down", cfg.NtfyLiveID, cfg.NtfyPrioProg)
}

func sendGroupedComplete(cfg *Config) {
	sendNtfy(cfg, "Downloads Finished", "All active downloads have finished.", "white_check_mark", cfg.NtfyLiveID, cfg.NtfyPrioProg)
}

func sendUpdate(cfg *Config, t *Torrent, pct int) {
	speed := float64(t.DlSpeed) / 1024 / 1024
	eta := formatDuration(t.Eta)

	var msg string
	if cfg.ProgressFormat == "percent" {
		msg = fmt.Sprintf("Progress: %d%%\nSpeed: %.1f MB/s\nETA: %s", pct, speed, eta)
	} else {
		bar := drawProgressBar(pct)
		msg = fmt.Sprintf("%d%% %s\nSpeed: %.1f MB/s\nETA: %s", pct, bar, speed, eta)
	}

	sendNtfy(cfg, t.Name, msg, "arrow_down", "qbit-"+t.Hash, cfg.NtfyPrioProg)
}

func sendComplete(cfg *Config, t *Torrent) {
	sendNtfy(cfg, "Download Complete", t.Name+" has finished downloading.", "white_check_mark", "qbit-"+t.Hash, cfg.NtfyPrioComp)
}

func sendNtfy(cfg *Config, title, msg, tag, id, priority string) {
	if cfg.NtfyServer == "" || cfg.NtfyTopic == "" {
		return
	}

	url := fmt.Sprintf("%s/%s", cfg.NtfyServer, cfg.NtfyTopic)
	req, err := http.NewRequest("POST", url, strings.NewReader(msg))
	if err != nil {
		log.Printf("Failed to create ntfy request: %v", err)
		return
	}
	req.Header.Set("Title", sanitizeHeader(title))
	req.Header.Set("Tags", sanitizeHeader(tag))
	req.Header.Set("Priority", sanitizeHeader(priority))
	if id != "" {
		sanitizedID := sanitizeHeader(id)
		req.Header.Set("X-Message-ID", sanitizedID)
		req.Header.Set("Message-ID", sanitizedID)
		req.Header.Set("X-Sequence-ID", sanitizedID)
	}

	if cfg.NtfyUser != "" && cfg.NtfyPass != "" {
		req.SetBasicAuth(cfg.NtfyUser, cfg.NtfyPass)
	}

	resp, err := ntfyClient.Do(req)
	if err != nil {
		log.Printf("Failed to send ntfy notification: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("ntfy server returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}
