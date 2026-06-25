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

// Connect logs in via HTTP, grabs SSID, then opens the WebSocket.
// Starts a background goroutine that auto-reconnects on disconnect.
func (t *Trader) Connect() error {
	if err := t.connectOnce(); err != nil {
		return err
	}
	// Start reconnect supervisor
	go t.reconnectLoop()
	return nil
}

// connectOnce performs a single connect attempt
func (t *Trader) connectOnce() error {
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

	// Reset done channel for new connection
	t.done = make(chan struct{})

	go t.readLoop()
	go t.heartbeatLoop()

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

// reconnectLoop watches for disconnection and reconnects with backoff
func (t *Trader) reconnectLoop() {
	backoff := []time.Duration{3 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}
	attempt := 0

	for {
		// Wait for current connection to drop
		<-t.done

		t.logger.Warn().Msg("⚠️  WebSocket disconnected - reconnecting...")

		for {
			delay := backoff[min(attempt, len(backoff)-1)]
			t.logger.Info().Dur("wait", delay).Int("attempt", attempt+1).Msg("🔄 Reconnect attempt...")
			time.Sleep(delay)

			if err := t.connectOnce(); err != nil {
				t.logger.Error().Err(err).Msg("reconnect failed, will retry")
				attempt++
				continue
			}

			t.logger.Info().Msg("✅ Reconnected to IQ Option WebSocket")
			attempt = 0 // reset backoff on success
			break
		}
	}
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

	// Log raw profile response to see exact balance format
	t.logger.Debug().RawJSON("profile_response", resp).Msg("← raw profile data")

	// Parse balances - try both top-level and nested formats
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp, &raw); err == nil {
		if balancesJSON, ok := raw["balances"]; ok {
			t.logger.Debug().RawJSON("balances_raw", balancesJSON).Msg("← raw balances field")
		}
	}

	// Parse balances directly from the profile auth response
	var profile struct {
		IsSuccessful bool      `json:"isSuccessful"`
		Message      string    `json:"message"`
		Balances     []Balance `json:"balances"`
	}
	if err := json.Unmarshal(resp, &profile); err == nil {
		if !profile.IsSuccessful && profile.Message != "" {
			return fmt.Errorf("auth rejected: %s", profile.Message)
		}
		if len(profile.Balances) > 0 {
			t.balancesMu.Lock()
			t.balances = profile.Balances
			t.balancesMu.Unlock()
			t.logger.Info().Int("count", len(profile.Balances)).Msg("✓ balances loaded from auth response")
			
			// Log each balance in detail
			for i, b := range profile.Balances {
				t.logger.Info().
					Int("index", i).
					Int64("id", b.ID).
					Int("type", b.Type).
					Float64("amount", b.Amount).
					Str("currency", b.Currency).
					Msg("📊 balance detail")
			}
		}
	}
	return nil
}

func (t *Trader) loadProfile() error {
	// If balances already populated from auth response, log and return
	t.balancesMu.RLock()
	existing := len(t.balances)
	t.balancesMu.RUnlock()

	if existing == 0 {
		// Explicitly request balances via get-balances
		t.logger.Info().Msg("requesting balances explicitly...")
		resp, err := t.sendAndWait("get-balances", struct{}{}, "balances")
		if err == nil {
			t.logger.Debug().RawJSON("get_balances_response", resp).Msg("← raw get-balances response")
			
			var result struct {
				Balances []Balance `json:"balances"`
			}
			if json.Unmarshal(resp, &result) == nil && len(result.Balances) > 0 {
				t.balancesMu.Lock()
				t.balances = result.Balances
				t.balancesMu.Unlock()
				
				t.logger.Info().Int("count", len(result.Balances)).Msg("✓ loaded balances from get-balances")
			}
		} else {
			t.logger.Warn().Err(err).Msg("get-balances request failed")
		}
	}

	t.balancesMu.RLock()
	balances := t.balances
	t.balancesMu.RUnlock()

	if len(balances) == 0 {
		t.logger.Warn().Msg("no balances loaded - will retry at trade time")
		return nil
	}

	t.logger.Info().Int("total_balances", len(balances)).Msg("📊 All balances from IQ Option:")
	for i, b := range balances {
		label := fmt.Sprintf("type_%d", b.Type)
		if b.Type == 1 {
			label = "real"
		} else if b.Type == 4 {
			label = "practice"
		}
		t.logger.Info().
			Int("index", i).
			Str("type", label).
			Int("type_id", b.Type).
			Float64("amount", b.Amount).
			Float64("bonus_amount", b.BonusAmount).
			Float64("total", b.TotalAmount()).
			Str("currency", b.Currency).
			Int64("id", b.ID).
			Msg("💰 Balance loaded")
	}
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
