package trader

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/go-rod/rod/lib/proto"
)

func (t *Trader) loadCookies() bool {
	if t.cfg.CookiesFile == "" {
		return false
	}

	data, err := os.ReadFile(t.cfg.CookiesFile)
	if err != nil {
		t.logger.Debug().Err(err).Msg("no saved cookies found")
		return false
	}

	var cookies []*proto.NetworkCookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		t.logger.Error().Err(err).Msg("failed to parse cookies file")
		return false
	}

	if len(cookies) == 0 {
		t.logger.Debug().Msg("cookies file is empty")
		return false
	}

	t.logger.Info().Int("count", len(cookies)).Msg("loading saved cookies...")
	
	// Set cookies for IQ Option domain
	for _, cookie := range cookies {
		if err := t.page.SetCookies([]*proto.NetworkCookieParam{
			{
				Name:     cookie.Name,
				Value:    cookie.Value,
				Domain:   cookie.Domain,
				Path:     cookie.Path,
				Secure:   cookie.Secure,
				HTTPOnly: cookie.HTTPOnly,
				SameSite: cookie.SameSite,
				Expires:  cookie.Expires,
			},
		}); err != nil {
			t.logger.Warn().Err(err).Str("cookie", cookie.Name).Msg("failed to set cookie")
		}
	}

	t.logger.Debug().Msg("cookies loaded successfully")
	return true
}

func (t *Trader) saveCookies() {
	if t.cfg.CookiesFile == "" {
		return
	}

	t.logger.Info().Msg("saving cookies for future sessions...")

	cookies, err := t.page.Cookies([]string{t.cfg.BaseURL})
	if err != nil {
		t.logger.Error().Err(err).Msg("failed to get cookies")
		return
	}

	// Create session directory if needed
	if err := os.MkdirAll(filepath.Dir(t.cfg.CookiesFile), 0755); err != nil {
		t.logger.Error().Err(err).Msg("failed to create cookies directory")
		return
	}

	data, err := json.MarshalIndent(cookies, "", "  ")
	if err != nil {
		t.logger.Error().Err(err).Msg("failed to marshal cookies")
		return
	}

	if err := os.WriteFile(t.cfg.CookiesFile, data, 0600); err != nil {
		t.logger.Error().Err(err).Msg("failed to write cookies file")
		return
	}

	t.logger.Info().
		Str("file", t.cfg.CookiesFile).
		Int("count", len(cookies)).
		Msg("✓ cookies saved successfully")
}
