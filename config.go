package main

import (
	"log"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the application configuration
type Config struct {
	QbitHost           string
	QbitUser           string
	QbitPass           string
	QbitPublicURL      string
	NtfyServer         string
	NtfyUser           string
	NtfyPass           string
	NtfyTopic          string
	NtfyPrioProg       string
	NtfyPrioComp       string
	NotifyComplete     bool
	NotifyProgress     bool
	NotificationMode   string // "grouped", "individual", "completion_only"
	GroupUpdateInt     time.Duration
	ProgressStep       int
	MinUpdateInt       time.Duration
	MaxDisplayTorrents int
	NtfyLiveID         string
	ProgressFormat     string
	PollInt            time.Duration
	AutoDiscoveryInt   time.Duration
	NotifyHealthErrors bool
	AllowedSubnets     []netip.Prefix
}

func loadConfig() *Config {
	cfg := &Config{}
	cfg.QbitHost = getEnv("QBIT_HOST", "http://localhost:8080")
	cfg.QbitUser = getEnv("QBIT_USER", "")
	cfg.QbitPass = getEnv("QBIT_PASS", "")
	cfg.QbitPublicURL = strings.TrimRight(strings.TrimSpace(getEnv("QBIT_PUBLIC_URL", "")), "/")

	cfg.NtfyServer = strings.TrimRight(getEnv("NTFY_SERVER", "https://ntfy.sh"), "/")
	cfg.NtfyUser = getEnv("NTFY_USER", "")
	cfg.NtfyPass = getEnv("NTFY_PASS", "")
	cfg.NtfyTopic = mustGetEnv("NTFY_TOPIC")
	cfg.NtfyPrioProg = getEnv("NTFY_PRIORITY_PROGRESS", "2") // Default: Low (no sound/vibe)
	cfg.NtfyPrioComp = getEnv("NTFY_PRIORITY_COMPLETE", "3") // Default: Default (sound/vibe)

	cfg.NotifyComplete = getEnvBool("NOTIFY_COMPLETE", true)
	cfg.NotifyProgress = getEnvBool("NOTIFY_PROGRESS", true)
	cfg.NotifyHealthErrors = getEnvBool("NOTIFY_HEALTH_ERRORS", false)

	mode := strings.ToLower(getEnv("NOTIFICATION_MODE", "grouped"))
	if mode != "grouped" && mode != "individual" && mode != "completion_only" {
		log.Printf("Warning: Invalid NOTIFICATION_MODE %q. Defaulting to 'grouped'.", mode)
		mode = "grouped"
	}
	cfg.NotificationMode = mode

	groupIntVal := getEnvInt("GROUP_UPDATE_INTERVAL", 15)
	if groupIntVal <= 0 {
		log.Printf("Warning: Invalid GROUP_UPDATE_INTERVAL %d. Using default 15s.", groupIntVal)
		groupIntVal = 15
	}
	cfg.GroupUpdateInt = time.Duration(groupIntVal) * time.Second

	stepVal := getEnvInt("PROGRESS_STEP", 25)
	if stepVal < 1 || stepVal > 100 {
		log.Printf("Warning: Invalid PROGRESS_STEP %d. Using default 25%%.", stepVal)
		stepVal = 25
	}
	cfg.ProgressStep = stepVal

	minIntVal := getEnvInt("MIN_UPDATE_INTERVAL", 60)
	if minIntVal < 0 {
		log.Printf("Warning: Invalid MIN_UPDATE_INTERVAL %d. Using default 60s.", minIntVal)
		minIntVal = 60
	}
	cfg.MinUpdateInt = time.Duration(minIntVal) * time.Second

	maxDisplayVal := getEnvInt("MAX_DISPLAY_TORRENTS", 5)
	if maxDisplayVal <= 0 {
		log.Printf("Warning: Invalid MAX_DISPLAY_TORRENTS %d. Using default 5.", maxDisplayVal)
		maxDisplayVal = 5
	}
	cfg.MaxDisplayTorrents = maxDisplayVal

	cfg.NtfyLiveID = getEnv("NTFY_LIVE_ID", "qbit-live-downloads")
	cfg.ProgressFormat = getEnv("PROGRESS_FORMAT", "bar") // "bar" or "percent"
	pollIntVal := getEnvInt("POLL_INTERVAL", 5)
	if pollIntVal <= 0 {
		log.Fatalf("Invalid POLL_INTERVAL: %d. Must be > 0", pollIntVal)
	}
	cfg.PollInt = time.Duration(pollIntVal) * time.Second

	autoDiscVal := getEnvInt("AUTO_DISCOVERY_INTERVAL", 300)
	if autoDiscVal < 0 {
		log.Printf("Warning: Invalid AUTO_DISCOVERY_INTERVAL %d. Using default 300s.", autoDiscVal)
		autoDiscVal = 300
	}
	cfg.AutoDiscoveryInt = time.Duration(autoDiscVal) * time.Second

	// Parse ALLOWED_SUBNETS
	subnetEnv := getEnv("ALLOWED_SUBNETS", "")
	if subnetEnv != "" {
		for _, s := range strings.Split(subnetEnv, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			// Format single IPs as CIDRs using robust validation
			if !strings.Contains(s, "/") {
				ip, err := netip.ParseAddr(s)
				if err != nil {
					log.Printf("Warning: Invalid IP format %q: %v. Ignoring.", s, err)
					continue
				}
				if ip.Is6() {
					s = s + "/128"
				} else {
					s = s + "/32"
				}
			}
			prefix, err := netip.ParsePrefix(s)
			if err != nil {
				log.Printf("Warning: Invalid subnet format %q: %v. Ignoring.", s, err)
				continue
			}
			cfg.AllowedSubnets = append(cfg.AllowedSubnets, prefix)
		}
	} else {
		log.Println("WARNING: ALLOWED_SUBNETS is not set. The /track endpoint will deny all requests.")
	}
	return cfg
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

func getEnvBool(k string, fallback bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getEnvInt(k string, fallback int) int {
	v := os.Getenv(k)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("Warning: Invalid value for %s: %q. Using fallback: %d", k, v, fallback)
		return fallback
	}
	return i
}
