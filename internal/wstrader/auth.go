package wstrader

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

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

	if err := t.wsAuth(); err != nil {
		return fmt.Errorf("ws auth: %w", err)
	}
	t.logger.Info().Msg("✓ WebSocket authenticated")

	if err := t.loadProfile(); err != nil {
		return fmt.Errorf("load profile: %w", err)
	}

	if err := t.loadProfits(); err != nil {
		t.logger.Warn().Err(err).Msg("could not load profit data (non-fatal)")
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
	type authMsg struct {
		SSID string `json:"ssid"`
	}

	resp, err := t.sendAndWait("ssid", authMsg{SSID: t.ssid}, "profile")
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
	resp, err := t.sendAndWait("get-profile", struct{}{}, "profile")
	if err != nil {
		return err
	}

	var profile struct {
		IsSuccessful bool `json:"isSuccessful"`
		Result       struct {
			Balances []Balance `json:"balances"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &profile); err != nil {
		return fmt.Errorf("parse profile: %w", err)
	}

	t.balancesMu.Lock()
	t.balances = profile.Result.Balances
	t.balancesMu.Unlock()

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

func (t *Trader) loadProfits() error {
	resp, err := t.sendAndWait("get-initialization-data", struct{}{}, "initialization-data")
	if err != nil {
		return err
	}

	var initData struct {
		Result struct {
			TurboOptions []struct {
				ActiveID int     `json:"active_id"`
				Profit   float64 `json:"profit"`
			} `json:"turbo"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &initData); err != nil {
		return err
	}

	t.profitsMu.Lock()
	defer t.profitsMu.Unlock()
	for _, o := range initData.Result.TurboOptions {
		key := fmt.Sprintf("%d", o.ActiveID)
		if t.profits[key] == nil {
			t.profits[key] = make(map[string]float64)
		}
		t.profits[key]["turbo"] = o.Profit
	}
	return nil
}
