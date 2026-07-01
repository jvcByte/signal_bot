package wstrader

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
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

// connectOnce performs a single connect attempt.
// On reconnects, reuses the cached SSID to avoid triggering IQ Option's
// suspicious login detection from repeated HTTP logins.
func (t *Trader) connectOnce() error {
	// Only do HTTP login if we don't have a valid SSID yet
	if t.ssid == "" {
		t.logger.Info().Msg("🔐 Logging in to IQ Option via HTTP...")
		var loginErr error
		for attempt := 1; attempt <= 3; attempt++ {
			loginErr = t.httpLogin()
			if loginErr == nil {
				break
			}
			t.logger.Warn().Err(loginErr).Int("attempt", attempt).Msg("HTTP login failed, retrying...")
			time.Sleep(time.Duration(attempt*5) * time.Second)
		}
		if loginErr != nil {
			return fmt.Errorf("http login: %w", loginErr)
		}
		t.logger.Info().Str("ssid", t.ssid[:8]+"...").Msg("✓ SSID obtained")
	} else {
		t.logger.Info().Str("ssid", t.ssid[:8]+"...").Msg("🔑 Reusing cached SSID (no new HTTP login)")
	}

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

// reconnectLoop watches for disconnection and reconnects with backoff.
// Stops after maxReconnectAttempts to avoid infinite loops when service is down.
func (t *Trader) reconnectLoop() {
	const maxReconnectAttempts = 20 // ~5 minutes total with backoff
	backoff := []time.Duration{3 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, 60 * time.Second}

	for {
		<-t.done

		t.logger.Warn().Msg("⚠️  WebSocket disconnected - reconnecting...")
		attempt := 0

		for {
			if attempt >= maxReconnectAttempts {
				t.logger.Error().Int("attempts", attempt).Msg("❌ Max reconnect attempts reached - giving up")
				return
			}
			delay := backoff[min(attempt, len(backoff)-1)]
			t.logger.Info().Dur("wait", delay).Int("attempt", attempt+1).Msg("🔄 Reconnect attempt...")
			time.Sleep(delay)

			if err := t.connectOnce(); err != nil {
				t.logger.Error().Err(err).Msg("reconnect failed, will retry")
				attempt++
				continue
			}

			t.logger.Info().Msg("✅ Reconnected to IQ Option WebSocket")
			break
		}
	}
}

func (t *Trader) httpLogin() error {
	// Use curl for HTTP login - Go's TLS fingerprint is blocked by IQ Option's WAF
	// curl mimics a real browser's TLS handshake
	args := []string{
		"-s", "-X", "POST",
		"https://auth.iqoption.com/api/v1.0/login",
		"--max-time", "30",
		"-H", "Content-Type: application/x-www-form-urlencoded",
		"-H", "User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"-H", "Accept: application/json",
		"-H", "Origin: https://iqoption.com",
		"-H", "Referer: https://iqoption.com/",
		"-d", "email=" + url.QueryEscape(t.cfg.Email) + "&password=" + url.QueryEscape(t.cfg.Password),
		"-c", "-", // output cookies to stdout
	}

	// Add proxy if set
	if pURL := proxyFromEnv(); pURL != nil {
		args = append(args, "--proxy", pURL.String())
		t.logger.Info().Str("proxy", pURL.Host).Msg("🔀 Using proxy for HTTP login")
	}

	cmd := exec.Command("curl", args...)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("curl login failed: %w", err)
	}

	body := string(out)

	// Try JSON body for SSID
	var loginResp struct {
		Data struct {
			SSID string `json:"ssid"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &loginResp); err == nil && len(loginResp.Data.SSID) > 8 {
		t.ssid = loginResp.Data.SSID
		t.logger.Info().Str("ssid", t.ssid[:8]+"...").Msg("✓ SSID obtained from curl")
		return nil
	}

	// Parse cookies from curl -c - output for SSID
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		// Netscape cookie format: domain flag path secure expiry name value
		if len(fields) == 7 && fields[5] == "ssid" && len(fields[6]) > 8 {
			t.ssid = fields[6]
			t.logger.Info().Str("ssid", t.ssid[:8]+"...").Msg("✓ SSID obtained from cookie")
			return nil
		}
	}

	// Fallback: standard HTTP client (when not blocked)
	t.logger.Debug().Str("curl_output", body[:min(200, len(body))]).Msg("curl login fallback")
	return t.httpLoginStandard()
}

func (t *Trader) httpLoginStandard() error {
	data := url.Values{}
	data.Set("email", t.cfg.Email)
	data.Set("password", t.cfg.Password)

	req, err := http.NewRequest("POST", iqOptionAPIURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://iqoption.com")
	req.Header.Set("Referer", "https://iqoption.com/")
	req.Close = true

	// Use proxy if configured via HTTPS_PROXY or HTTP_PROXY env var
	// Go's http.ProxyFromEnvironment handles this automatically
	pURL := proxyFromEnv()
	var transport http.RoundTripper
	if pURL != nil {
		t.logger.Info().Str("proxy", pURL.Host).Str("scheme", pURL.Scheme).Msg("🔀 Using proxy for HTTP login")
		if pURL.Scheme == "socks5" || pURL.Scheme == "socks5h" {
			dialer, err := proxy.FromURL(pURL, proxy.Direct)
			if err == nil {
				if dc, ok := dialer.(interface {
					DialContext(ctx context.Context, network, addr string) (net.Conn, error)
				}); ok {
					transport = &http.Transport{DialContext: dc.DialContext}
				} else {
					transport = &http.Transport{Dial: dialer.Dial} //nolint:staticcheck
				}
			}
		} else {
			// HTTP/HTTPS proxy - use env-aware transport
			transport = &http.Transport{
				Proxy:              http.ProxyURL(pURL),
				ForceAttemptHTTP2: false, // force HTTP/1.1 to avoid WAF issues
				TLSHandshakeTimeout: 10 * time.Second,
			}
		}
	} else {
		transport = http.DefaultTransport
	}

	client := &http.Client{
		Jar:       t.jar,
		Timeout:   30 * time.Second,
		Transport: transport,
	}
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

func proxyFromEnv() *url.URL {
	for _, env := range []string{"HTTPS_PROXY", "HTTP_PROXY", "https_proxy", "http_proxy"} {
		if val := os.Getenv(env); val != "" {
			if u, err := url.Parse(val); err == nil {
				return u
			}
		}
	}
	return nil
}

// buildTransport returns an http.RoundTripper with proxy support.
// Handles both HTTP and SOCKS5 proxies correctly.
func buildTransport(proxyURL *url.URL) http.RoundTripper {
	if proxyURL == nil {
		return http.DefaultTransport
	}
	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err == nil {
			// Use DialContext for proper connection handling
			if dc, ok := dialer.(interface {
				DialContext(ctx context.Context, network, addr string) (net.Conn, error)
			}); ok {
				return &http.Transport{DialContext: dc.DialContext}
			}
			return &http.Transport{Dial: dialer.Dial} //nolint:staticcheck
		}
	}
	return &http.Transport{Proxy: http.ProxyURL(proxyURL)}
}

func (t *Trader) dialWS() error {
	headers := http.Header{}
	headers.Set("Origin", "https://iqoption.com")
	headers.Set("User-Agent", "Mozilla/5.0")

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		ReadBufferSize:   4096,
		WriteBufferSize:  4096,
	}
	// Note: WebSocket connects directly (not through proxy)
	// Only HTTP login needs proxy for TLS fingerprint bypass
	conn, _, err := dialer.Dial(iqOptionWSURL, headers)
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
				// If SSID expired, clear it so next connectOnce does a fresh HTTP login
				if strings.Contains(strings.ToLower(profile.Message), "ssid") ||
					strings.Contains(strings.ToLower(profile.Message), "unauthorized") {
					t.ssid = ""
					t.logger.Warn().Msg("SSID expired - will re-login on next attempt")
				}
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
