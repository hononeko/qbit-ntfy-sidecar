package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

func getTorrentInfo(client *http.Client, cfg *Config, hash string) (*Torrent, error) {
	resp, err := client.Get(cfg.QbitHost + "/api/v2/torrents/info?hashes=" + url.QueryEscape(hash))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

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

func login(client *http.Client, cfg *Config) error {
	data := url.Values{}
	data.Set("username", cfg.QbitUser)
	data.Set("password", cfg.QbitPass)

	resp, err := client.PostForm(cfg.QbitHost+"/api/v2/auth/login", data)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %v", err)
	}

	if resp.StatusCode != 200 || strings.Contains(string(body), "Fails.") {
		return fmt.Errorf("bad credentials or connection failed")
	}
	return nil
}
