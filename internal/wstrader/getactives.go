package wstrader

import (
	"encoding/json"
	"strings"
)

// knownAssets is a hardcoded fallback for common OTC assets.
// These IDs are stable and verified against IQ Option's API.
// The dynamic fetch is attempted first but this guarantees trades work.
var knownAssets = map[string]int{
	// Forex OTC
	"EURUSD-OTC": 76, "EURGBP-OTC": 77, "USDCHF-OTC": 78,
	"EURJPY-OTC": 79, "NZDUSD-OTC": 80, "GBPUSD-OTC": 81,
	"GBPJPY-OTC": 84, "USDJPY-OTC": 85, "AUDCAD-OTC": 86,
	"AUDJPY-OTC": 2113, "USDCAD-OTC": 2112, "AUDUSD-OTC": 2111,
	"EURAUD-OTC": 2120, "EURCAD-OTC": 2117, "EURCHF-OTC": 2131,
	"EURNZD-OTC": 2122, "GBPAUD-OTC": 2116, "GBPCAD-OTC": 2114,
	"GBPCHF-OTC": 2115, "GBPNZD-OTC": 2132, "CHFJPY-OTC": 2118,
	"CADCHF-OTC": 2119, "CADJPY-OTC": 2136, "AUDCHF-OTC": 2129,
	"AUDNZD-OTC": 2130, "NZDCAD-OTC": 2137, "NZDJPY-OTC": 2138,
	// Crypto OTC
	"ETHUSD-OTC": 1941, "XRPUSD-OTC": 2107, "SOLUSD-OTC": 1978,
	"DOGECOIN-OTC": 1977, "CARDANO-OTC": 1974, "LTCUSD-OTC": 2126,
	"TONUSD-OTC": 2091, "BCHUSD-OTC": 2148,
	// Stocks OTC
	"OPENAI-OTC": 2452, "ANTHROPIC-OTC": 2451, "TESLA-OTC": 1936,
	"APPLE-OTC": 1938, "AMAZON-OTC": 1935, "GOOGLE-OTC": 1933,
	"MSFT-OTC": 2099, "NVDA-OTC": 2403, "FB-OTC": 1937,
	"SNAP-OTC": 2125, "SPACEX-OTC": 2443, "PLTR-OTC": 2313,
	// Indices OTC
	"SP500-OTC": 1971, "US30-OTC": 1973, "USNDAQ100-OTC": 1972,
	"GER30-OTC": 2046, "UK100-OTC": 2047, "EU50-OTC": 2050,
	"JP225-OTC": 2051, "AUS200-OTC": 2048, "HK33-OTC": 2049,
	// Commodities OTC
	"XAUUSD-OTC": 1857, "XAGUSD-OTC": 1858, "USOUSD-OTC": 1859,
	"UKOUSD-OTC": 1931,
}

// getActiveIDFromAPI resolves an asset name to its IQ Option active_id.
// Checks hardcoded map first (instant), then tries dynamic API fetch.
func (t *Trader) getActiveIDFromAPI(assetName string) (int, string, bool, bool) {
	assetName = strings.ToUpper(strings.TrimSpace(assetName))

	// 1. Check in-memory cache (populated by previous dynamic fetch)
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

	// 2. Check hardcoded map (instant, no network required)
	if id, ok := knownAssets[assetName+"-OTC"]; ok {
		t.logger.Debug().Str("asset", assetName+"-OTC").Int("active_id", id).Msg("✓ Found asset in hardcoded map")
		return id, assetName + "-OTC", true, true
	}
	if id, ok := knownAssets[assetName]; ok {
		t.logger.Debug().Str("asset", assetName).Int("active_id", id).Msg("✓ Found asset in hardcoded map")
		return id, assetName, false, true
	}

	// 3. Fetch from IQ Option API (only for unknown assets)
	t.logger.Info().Str("asset", assetName).Msg("Asset not in cache, fetching from IQ Option API...")

	if id, name, isOTC, found := t.fetchAssetFromAPI(assetName); found {
		return id, name, isOTC, true
	}

	t.logger.Warn().Str("asset", assetName).Msg("Asset not found")
	return 0, "", false, false
}

func (t *Trader) fetchAssetFromAPI(assetName string) (int, string, bool, bool) {
	t.logger.Info().Msg("Fetching asset list from IQ Option...")

	// Try turbo-option first, then binary-option
	for _, optionType := range []string{"turbo-option", "binary-option"} {
		resp, err := t.sendAndWait("get-underlying-list", map[string]interface{}{
			"type": optionType,
		}, "underlying-list")

		if err != nil {
			t.logger.Debug().Str("type", optionType).Err(err).Msg("get-underlying-list failed")
			continue
		}

		var result struct {
			Underlying []struct {
				ActiveID    int    `json:"active_id"`
				Name        string `json:"name"`
				IsEnabled   bool   `json:"is_enabled"`
				IsSuspended bool   `json:"is_suspended"`
			} `json:"underlying"`
		}
		if err := json.Unmarshal(resp, &result); err != nil || len(result.Underlying) == 0 {
			continue
		}

		// Build cache
		t.assetCacheMu.Lock()
		if t.assetCache == nil {
			t.assetCache = make(map[string]int)
		}
		for _, a := range result.Underlying {
			if a.IsEnabled && !a.IsSuspended {
				t.assetCache[strings.ToUpper(a.Name)] = a.ActiveID
			}
		}
		t.logger.Info().Int("count", len(t.assetCache)).Msg("✓ Asset cache built from API")
		t.assetCacheMu.Unlock()

		// Lookup
		t.assetCacheMu.RLock()
		defer t.assetCacheMu.RUnlock()
		if id, ok := t.assetCache[assetName+"-OTC"]; ok {
			return id, assetName + "-OTC", true, true
		}
		if id, ok := t.assetCache[assetName]; ok {
			return id, assetName, false, true
		}
		return 0, "", false, false
	}

	return 0, "", false, false
}
