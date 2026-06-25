package wstrader

import (
	"encoding/json"
	"strings"
)

// getActiveIDFromAPI queries IQ Option for all available assets and returns the active_id for the requested asset
// Uses cached data if available, otherwise fetches fresh data and caches it.
// Returns: (activeID, resolvedName, isOTC, found)
func (t *Trader) getActiveIDFromAPI(assetName string) (int, string, bool, bool) {
	assetName = strings.ToUpper(strings.TrimSpace(assetName))
	
	// Check cache first (fast path)
	t.assetCacheMu.RLock()
	cacheSize := len(t.assetCache)
	if cacheSize > 0 {
		// Try OTC version first
		if id, ok := t.assetCache[assetName+"-OTC"]; ok {
			t.assetCacheMu.RUnlock()
			t.logger.Debug().Str("asset", assetName+"-OTC").Int("active_id", id).Msg("✓ Found OTC asset in cache")
			return id, assetName + "-OTC", true, true
		}
		// Try regular version
		if id, ok := t.assetCache[assetName]; ok {
			t.assetCacheMu.RUnlock()
			t.logger.Debug().Str("asset", assetName).Int("active_id", id).Msg("✓ Found asset in cache")
			return id, assetName, false, true
		}
	}
	t.assetCacheMu.RUnlock()
	
	// Cache miss - fetch from API
	t.logger.Info().Msg("Fetching asset list from IQ Option (first time or cache miss)...")
	
	// Try method 1: get-underlying-list (lists all trading instruments)
	resp, err := t.sendAndWait("get-underlying-list", map[string]interface{}{
		"type": "turbo-option",
	}, "underlying-list")
	
	if err != nil {
		t.logger.Debug().Err(err).Msg("get-underlying-list failed, trying sendMessage wrapper...")
		
		// Try method 2: sendMessage wrapper for get-underlying-list
		type getUnderlyingMsg struct {
			Name string      `json:"name"`
			Version string  `json:"version"`
			Body    interface{} `json:"body"`
		}
		
		msg := getUnderlyingMsg{
			Name:    "get-underlying-list",
			Version: "2.0",
			Body: map[string]interface{}{
				"type": "turbo-option",
			},
		}
		
		resp, err = t.sendAndWait("sendMessage", msg, "underlying-list")
		if err != nil {
			t.logger.Warn().Err(err).Msg("Both methods failed to query asset list")
			return 0, "", false, false
		}
	}
	
	// Parse response
	var result struct {
		Underlying []struct {
			ActiveID  int    `json:"active_id"`
			Name      string `json:"name"`
			IsEnabled bool   `json:"is_enabled"`
			IsSuspended bool `json:"is_suspended"`
		} `json:"underlying"`
	}
	
	if err := json.Unmarshal(resp, &result); err != nil {
		t.logger.Warn().Err(err).Msg("Failed to parse underlying-list")
		return 0, "", false, false
	}
	
	// Build cache
	t.assetCacheMu.Lock()
	t.assetCache = make(map[string]int)
	for _, asset := range result.Underlying {
		if asset.IsEnabled && !asset.IsSuspended {
			t.assetCache[strings.ToUpper(asset.Name)] = asset.ActiveID
		}
	}
	cacheSize = len(t.assetCache)
	t.assetCacheMu.Unlock()
	
	t.logger.Info().Int("count", cacheSize).Msg("✓ Asset cache built")
	
	// Now lookup in cache
	t.assetCacheMu.RLock()
	defer t.assetCacheMu.RUnlock()
	
	// Try OTC version first (24/7 availability)
	if id, ok := t.assetCache[assetName+"-OTC"]; ok {
		t.logger.Info().Str("asset", assetName+"-OTC").Int("active_id", id).Msg("✓ Found OTC asset")
		return id, assetName + "-OTC", true, true
	}
	
	// Fall back to regular version
	if id, ok := t.assetCache[assetName]; ok {
		t.logger.Info().Str("asset", assetName).Int("active_id", id).Msg("✓ Found asset")
		return id, assetName, false, true
	}
	
	t.logger.Warn().Str("asset", assetName).Msg("Asset not found or not enabled")
	return 0, "", false, false
}


// ListAllAssets returns all available assets from the cache (fetches if empty)
func (t *Trader) ListAllAssets() (map[string]int, error) {
	t.assetCacheMu.RLock()
	cacheSize := len(t.assetCache)
	t.assetCacheMu.RUnlock()
	
	// If cache is empty, fetch from API
	if cacheSize == 0 {
		// Trigger a fetch by querying for a dummy asset
		_, _, _, _ = t.getActiveIDFromAPI("_DUMMY_")
	}
	
	t.assetCacheMu.RLock()
	defer t.assetCacheMu.RUnlock()
	
	// Return a copy of the cache
	result := make(map[string]int, len(t.assetCache))
	for k, v := range t.assetCache {
		result[k] = v
	}
	
	return result, nil
}
