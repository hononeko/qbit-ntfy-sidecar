package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func (a *App) handleTrackRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	hash := r.URL.Query().Get("hash")
	if hash == "" || !IsValidHash(hash) {
		http.Error(w, "Missing or invalid 'hash' query parameter", 400)
		return
	}

	mutex.Lock()
	if activeMonitors[hash] {
		mutex.Unlock()
		_, _ = fmt.Fprintf(w, "Already tracking %q", hash) // %q escapes newlines to prevent response manipulation
		return
	}
	activeMonitors[hash] = true
	mutex.Unlock()

	appWg.Add(1)
	go a.trackTorrent(hash)

	w.WriteHeader(200)
	_, _ = fmt.Fprintf(w, "Tracking started for %q", hash) // %q escapes input
}

func (a *App) ipFilterMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIPStr, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			clientIPStr = r.RemoteAddr
		}

		// Remove potential IPv6 brackets
		clientIPStr = strings.Trim(clientIPStr, "[]")

		clientIP, err := netip.ParseAddr(clientIPStr)
		if err != nil {
			log.Printf("Warning: Invalid RemoteAddr %q: %v", r.RemoteAddr, err)
			http.Error(w, "Forbidden: Invalid IP Address", http.StatusForbidden)
			return
		}

		// Ensure IPv4 mapped IPv6 addresses (e.g., ::ffff:192.168.1.1) map back properly
		clientIP = clientIP.Unmap()

		allowed := false
		for _, subnet := range a.Config.AllowedSubnets {
			if subnet.Contains(clientIP) {
				allowed = true
				break
			}
		}

		if !allowed {
			log.Printf("Blocked unauthorized request from %q", r.RemoteAddr) // %q escapes newlines to prevent log injection
			http.Error(w, "Forbidden: Unauthorized IP", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	}
}
