package wstrader

import (
	"encoding/json"
	"strings"
)

// getActiveIDFromAPI resolves an asset name to its IQ Option active_id.
// Tries OTC version first (24/7), falls back to regular.
// Results are cached after the first API call.
func (t *Trader) getActiveIDFromAPI(assetName string) (int, string, bool, bool) {
	assetName = strings.ToUpper(strings.TrimSpace(assetName))

	// Cache hit (fast path)
	t.assetCacheMu.RLock()
	if len(t.assetCache) > 0 {
		if id, ok := t.assetCache[assetName+"-OTC"]; ok {
			t.assetCacheMu.RUnlock()
			return id, assetName + "-OTC", true, true
		}
		if id, ok := t.assetCache[assetName]; ok {
			t.assetCacheMu.RUnlock()
			return id, assetName, false, true
		}
	}
	t.assetCacheMu.RUnlock()

	// Fetch full asset list from IQ Option
	t.logger.Info().Msg("Fetching asset list from IQ Option (first time or cache miss)...")

	resp, err := t.sendAndWait("get-underlying-list", map[string]interface{}{
		"type": "turbo-option",
	}, "underlying-list")

	if err != nil {
		// Fallback: sendMessage wrapper
		type msg struct {
			Name    string      `json:"name"`
			Version string      `json:"version"`
			Body    interface{} `json:"body"`
		}
		resp, err = t.sendAndWait("sendMessage", msg{
			Name:    "get-underlying-list",
			Version: "2.0",
			Body:    map[string]interface{}{"type": "turbo-option"},
		}, "underlying-list")
		if err != nil {
			t.logger.Warn().Err(err).Msg("Failed to fetch asset list")
			return 0, "", false, false
		}
	}

	var result struct {
		Underlying []struct {
			ActiveID    int    `json:"active_id"`
			Name        string `json:"name"`
			IsEnabled   bool   `json:"is_enabled"`
			IsSuspended bool   `json:"is_suspended"`
		} `json:"underlying"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		t.logger.Warn().Err(err).Msg("Failed to parse asset list")
		return 0, "", false, false
	}

	// Rebuild cache
	t.assetCacheMu.Lock()
	t.assetCache = make(map[string]int)
	for _, a := range result.Underlying {
		if a.IsEnabled && !a.IsSuspended {
			t.assetCache[strings.ToUpper(a.Name)] = a.ActiveID
		}
	}
	t.logger.Info().Int("count", len(t.assetCache)).Msg("✓ Asset cache built")
	t.assetCacheMu.Unlock()

	// Lookup in fresh cache
	t.assetCacheMu.RLock()
	defer t.assetCacheMu.RUnlock()

	if id, ok := t.assetCache[assetName+"-OTC"]; ok {
		t.logger.Info().Str("asset", assetName+"-OTC").Int("active_id", id).Msg("✓ Found OTC asset")
		return id, assetName + "-OTC", true, true
	}
	if id, ok := t.assetCache[assetName]; ok {
		t.logger.Info().Str("asset", assetName).Int("active_id", id).Msg("✓ Found asset")
		return id, assetName, false, true
	}

	t.logger.Warn().Str("asset", assetName).Msg("Asset not found or not enabled")
	return 0, "", false, false
}
