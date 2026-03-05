package main

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func parseSubnets(subnets []string) []netip.Prefix {
	var parsed []netip.Prefix
	for _, s := range subnets {
		if !strings.Contains(s, "/") {
			ip, err := netip.ParseAddr(s)
			if err != nil {
				continue
			}
			if ip.Is6() {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		prefix, err := netip.ParsePrefix(s)
		if err == nil {
			parsed = append(parsed, prefix)
		}
	}
	return parsed
}

func TestIPFilterMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		allowedSubnets []string
		remoteAddr     string
		expectedStatus int
	}{
		{
			name:           "Empty config (Deny All)",
			allowedSubnets: []string{},
			remoteAddr:     "192.168.1.5:1234",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv4 Allowed Subnet",
			allowedSubnets: []string{"192.168.1.0/24"},
			remoteAddr:     "192.168.1.100:4567",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv4 Denied Subnet",
			allowedSubnets: []string{"10.0.0.0/8"},
			remoteAddr:     "192.168.1.100:4567",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv4 Single IP Allowed",
			allowedSubnets: []string{"192.168.1.5"},
			remoteAddr:     "192.168.1.5:8080",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv4 Single IP Denied",
			allowedSubnets: []string{"192.168.1.5"},
			remoteAddr:     "192.168.1.6:8080",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv6 Allowed Subnet",
			allowedSubnets: []string{"2001:db8::/32"},
			remoteAddr:     "[2001:db8::1]:1234",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv6 Denied Subnet",
			allowedSubnets: []string{"2001:db8::/32"},
			remoteAddr:     "[2001:0db9::1]:1234", // Note the 9
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "IPv6 Single IP Allowed (with brackets from r.RemoteAddr)",
			allowedSubnets: []string{"fe80::1"},
			remoteAddr:     "[fe80::1]:1234",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "IPv6 Localhost Allowed",
			allowedSubnets: []string{"::1"},
			remoteAddr:     "[::1]:8080",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid RemoteAddr format",
			allowedSubnets: []string{"192.168.1.0/24"},
			remoteAddr:     "notanip",
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := &App{
				Config: &Config{
					AllowedSubnets: parseSubnets(tt.allowedSubnets),
				},
			}

			// Setup dummy handler to wrap
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			})

			middleware := app.ipFilterMiddleware(nextHandler)

			// Execute mock request
			req := httptest.NewRequest("POST", "/track?hash=123", nil)
			req.RemoteAddr = tt.remoteAddr

			rec := httptest.NewRecorder()
			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestHandleTrackRequest(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		hash           string
		preTrack       bool // whether to inject the hash into activeMonitors beforehand
		expectedStatus int
	}{
		{
			name:           "Valid POST",
			method:         "POST",
			hash:           "1234567890abcdef1234567890abcdef12345678",
			preTrack:       false,
			expectedStatus: 200,
		},
		{
			name:           "Invalid Method GET",
			method:         "GET",
			hash:           "1234567890abcdef1234567890abcdef12345678",
			preTrack:       false,
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "Missing Hash",
			method:         "POST",
			hash:           "",
			preTrack:       false,
			expectedStatus: 400,
		},
		{
			name:           "Already Tracking Hash",
			method:         "POST",
			hash:           "1234567890abcdef1234567890abcdef12345678",
			preTrack:       true,
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hack activeMonitors
			if tt.preTrack {
				mutex.Lock()
				activeMonitors[tt.hash] = true
				mutex.Unlock()
			} else {
				mutex.Lock()
				delete(activeMonitors, tt.hash)
				mutex.Unlock()
			}

			// Clean up worker threads when test finishes
			defer func() {
				mutex.Lock()
				delete(activeMonitors, tt.hash)
				mutex.Unlock()
			}()

			app := &App{Config: &Config{
				PollInt: 10 * time.Millisecond,
			}}

			req := httptest.NewRequest(tt.method, "/track?hash="+tt.hash, nil)
			rec := httptest.NewRecorder()

			app.handleTrackRequest(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}
