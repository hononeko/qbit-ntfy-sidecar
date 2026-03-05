package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

func sendUpdate(t *Torrent, pct int) {
	speed := float64(t.DlSpeed) / 1024 / 1024
	eta := formatDuration(t.Eta)

	var msg string
	if progressFormat == "percent" {
		msg = fmt.Sprintf("Progress: %d%%\nSpeed: %.1f MB/s\nETA: %s", pct, speed, eta)
	} else {
		bar := drawProgressBar(pct)
		msg = fmt.Sprintf("%d%% %s\nSpeed: %.1f MB/s\nETA: %s", pct, bar, speed, eta)
	}

	sendNtfy(t.Name, msg, "arrow_down", "qbit-"+t.Hash, ntfyPrioProg)
}

func sendComplete(t *Torrent) {
	sendNtfy("Download Complete", t.Name+" has finished downloading.", "white_check_mark", "qbit-"+t.Hash, ntfyPrioComp)
}

func sendNtfy(title, msg, tag, id, priority string) {
	url := fmt.Sprintf("%s/%s", ntfyServer, ntfyTopic)
	req, _ := http.NewRequest("POST", url, strings.NewReader(msg))
	req.Header.Set("Title", title)
	req.Header.Set("Tags", tag)
	req.Header.Set("Priority", priority)
	req.Header.Set("X-Sequence-ID", id)

	if ntfyUser != "" && ntfyPass != "" {
		req.SetBasicAuth(ntfyUser, ntfyPass)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Failed to send ntfy notification: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
}
