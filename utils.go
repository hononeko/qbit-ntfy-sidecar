package main

import (
	"math"
	"regexp"
	"strings"
	"time"
)

var validHashRx = regexp.MustCompile(`^([a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)

func IsValidHash(hash string) bool {
	return validHashRx.MatchString(hash)
}

func drawProgressBar(pct int) string {
	width := 10
	filled := int(math.Round(float64(pct) / 10.0))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
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
