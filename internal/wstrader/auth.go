package wstrader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Connect logs in via HTTP, grabs SSID, then opens the WebSocket
func (t *Trader) Connect() error {
	t.logger.Info().Msg("🔐 Logging in to IQ Option via HTTP...")
	if err := t.httpLogin(); err != nil {
		return fmt.Errorf("http login: %w", err)
	}
	t.logger.Info().Str("ssid", t.ssid[:8]+"...").Msg("✓ SSID obtained")

	t.logger.Info().Msg("🔌 Connecting to IQ Option WebSocket...")
	if err := t.dialWS(); err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	t.logger.Info().Msg("✓ WebSocket connected")

	go t.readLoop()
	go t.heartbeatLoop() // keep connection alive

	if err := t.wsAuth(); err != nil {
		return fmt.Errorf("ws auth: %w", err)
	}
	t.logger.Info().Msg("✓ WebSocket authenticated")

	if err := t.loadProfile(); err != nil {
		return fmt.Errorf("load profile: %w", err)
	}

	t.logger.Info().Msg("✅ IQ Option WebSocket trader ready")
	return nil
}

func (t *Trader) httpLogin() error {
	data := url.Values{}
	data.Set("email", t.cfg.Email)
	data.Set("password", t.cfg.Password)

	req, err := http.NewRequest("POST", iqOptionAPIURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Jar: t.jar}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST login: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return fmt.Errorf("login failed (status %d): %s", resp.StatusCode, string(body))
	}

	// Try JSON body for SSID
	var loginResp struct {
		Data struct {
			SSID string `json:"ssid"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &loginResp); err == nil && loginResp.Data.SSID != "" {
		t.ssid = loginResp.Data.SSID
		return nil
	}

	// Fall back to cookies
	u, _ := url.Parse("https://iqoption.com")
	for _, cookie := range t.jar.Cookies(u) {
		if cookie.Name == "ssid" {
			t.ssid = cookie.Value
			return nil
		}
	}

	return fmt.Errorf("SSID not found in login response. Body: %s", string(body))
}

func (t *Trader) dialWS() error {
	headers := http.Header{}
	headers.Set("Origin", "https://iqoption.com")
	headers.Set("User-Agent", "Mozilla/5.0")

	conn, _, err := websocket.DefaultDialer.Dial(iqOptionWSURL, headers)
	if err != nil {
		return err
	}
	t.conn = conn
	return nil
}

func (t *Trader) wsAuth() error {
	// IQ Option expects the SSID sent as the raw msg string (not a struct)
	resp, err := t.sendAndWait("ssid", t.ssid, "profile")
	if err != nil {
		return fmt.Errorf("ssid message: %w", err)
	}

	var errResp struct {
		IsSuccessful bool   `json:"isSuccessful"`
		Message      string `json:"message"`
	}
	if err := json.Unmarshal(resp, &errResp); err == nil {
		if !errResp.IsSuccessful && errResp.Message != "" {
			return fmt.Errorf("auth rejected: %s", errResp.Message)
		}
	}
	return nil
}

func (t *Trader) loadProfile() error {
	// Profile/balances come as push after auth - just read from t.balances
	// which gets populated by the profile push in readLoop
	// Wait up to 3 seconds for it
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		t.balancesMu.RLock()
		n := len(t.balances)
		t.balancesMu.RUnlock()
		if n > 0 {
			for _, b := range t.balances {
				label := "real"
				if b.Type == 4 {
					label = "practice"
				}
				t.logger.Info().
					Str("type", label).
					Float64("amount", b.Amount).
					Str("currency", b.Currency).
					Msg("💰 Balance loaded")
			}
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Non-fatal - continue without balance
	t.logger.Warn().Msg("could not load balances from profile push (non-fatal)")
	return nil
}

// heartbeatLoop sends a heartbeat every 15s to keep the connection alive
func (t *Trader) heartbeatLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.mu.Lock()
			err := t.conn.WriteMessage(websocket.TextMessage, []byte(`{"name":"heartbeat","msg":{"userTime":0,"heartbeatTime":0}}`))
			t.mu.Unlock()
			if err != nil {
				return
			}
			t.logger.Debug().Msg("♥ heartbeat sent")
		}
	}
}
