package wstrader

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"signal-bot/pkg/models"
)

// Asset ID map - sourced from IQ Option API constants
// OTC pairs (76-86) are available 24/7. All others are regular market hours only.
var assetIDs = map[string]int{
	// ── OTC pairs (24/7, turbo binary options) ──
	"EURUSD-OTC": 76,
	"EURGBP-OTC": 77,
	"USDCHF-OTC": 78,
	"EURJPY-OTC": 79,
	"NZDUSD-OTC": 80,
	"GBPUSD-OTC": 81,
	"GBPJPY-OTC": 84,
	"USDJPY-OTC": 85,
	"AUDCAD-OTC": 86,

	// ── Regular forex (market hours only) ──
	"EURUSD": 1,
	"EURGBP": 2,
	"GBPJPY": 3,
	"EURJPY": 4,
	"GBPUSD": 5,
	"USDJPY": 6,
	"AUDCAD": 7,
	"NZDUSD": 8,
	"USDCHF": 72,
	"AUDUSD": 99,
	"USDCAD": 100,
	"AUDJPY": 101,
	"GBPCAD": 102,
	"GBPCHF": 103,
	"GBPAUD": 104,
	"EURCAD": 105,
	"CHFJPY": 106,
	"CADCHF": 107,
	"EURAUD": 108,
	"AUDCHF": 943,
	"AUDNZD": 944,
	"CADJPY": 945,
	"EURCHF": 946,
	"GBPNZD": 947,
	"NZDCAD": 948,
	"NZDJPY": 949,
	"EURNZD": 212,
	"USDNOK": 168,
	"USDSEK": 219,
}

// otcPairs is the set of pairs that have 24/7 OTC versions
var otcPairs = map[string]bool{
	"EURUSD": true, "EURGBP": true, "USDCHF": true,
	"EURJPY": true, "NZDUSD": true, "GBPUSD": true,
	"GBPJPY": true, "USDJPY": true, "AUDCAD": true,
}

// resolveAssetID returns the active_id for a given asset.
// OTC version is preferred (24/7). Only 9 pairs have OTC versions on IQ Option.
// Non-OTC pairs are market-hours only and will fail outside trading hours.
func resolveAssetID(asset string) (int, string, bool, bool) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	asset = strings.Replace(asset, "-OTC", "", 1) // normalize

	// Check if OTC version exists for this pair
	if otcPairs[asset] {
		if id, ok := assetIDs[asset+"-OTC"]; ok {
			return id, asset + "-OTC", true, true // id, name, found, isOTC
		}
	}

	// Fall back to regular market (may be closed outside trading hours)
	if id, ok := assetIDs[asset]; ok {
		return id, asset, true, false
	}
	return 0, "", false, false
}

// GetBalance returns the current balance for the configured account type
func (t *Trader) GetBalance() (float64, error) {
	t.balancesMu.RLock()
	defer t.balancesMu.RUnlock()

	targetType := 1 // real
	if t.cfg.DemoMode {
		targetType = 4 // practice
	}

	for _, b := range t.balances {
		if b.Type == targetType {
			return b.Amount, nil
		}
	}
	return 0, fmt.Errorf("balance not found for account type %d", targetType)
}

// getBalanceID returns the balance ID for the configured account type
func (t *Trader) getBalanceID() (int64, error) {
	t.balancesMu.RLock()
	balances := t.balances
	t.balancesMu.RUnlock()

	if len(balances) == 0 {
		t.logger.Warn().Msg("no balances cached - trying get-balances request...")
		resp, err := t.sendAndWait("get-balances", struct{}{}, "balances")
		if err == nil {
			var result struct {
				Balances []Balance `json:"balances"`
			}
			if json.Unmarshal(resp, &result) == nil && len(result.Balances) > 0 {
				t.balancesMu.Lock()
				t.balances = result.Balances
				t.balancesMu.Unlock()
				balances = result.Balances
			}
		}
	}

	targetType := 1 // real
	if t.cfg.DemoMode {
		targetType = 4 // practice
	}

	t.logger.Debug().Int("target_type", targetType).Int("balance_count", len(balances)).Msg("looking for balance ID")

	for _, b := range balances {
		t.logger.Debug().Int64("id", b.ID).Int("type", b.Type).Float64("amount", b.Amount).Msg("checking balance")
		if b.Type == targetType {
			return b.ID, nil
		}
	}
	return 0, fmt.Errorf("balance not found for account type %d (demo_mode=%v) - found %d balances", targetType, t.cfg.DemoMode, len(balances))
}

// PlaceTrade places a binary/turbo option trade via WebSocket
func (t *Trader) PlaceTrade(signal *models.Signal, amount float64) (*models.Trade, error) {
	trade := &models.Trade{
		ID:       signal.ID,
		SignalID: signal.ID,
		Asset:    signal.Asset,
		Direction: signal.Direction,
		Amount:   amount,
		Expiry:   signal.Expiry,
		Status:   models.StatusPending,
		Result:   models.ResultNone,
		PlacedAt: time.Now(),
	}

	// Resolve asset ID - OTC preferred (24/7), falls back to regular market
	activeID, resolvedAsset, ok, isOTC := resolveAssetID(signal.Asset)
	if !ok {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = fmt.Sprintf("unknown asset: %s", signal.Asset)
		return trade, fmt.Errorf("unknown asset %s - add to assetIDs map in trade.go", signal.Asset)
	}

	if !isOTC {
		t.logger.Warn().
			Str("asset", resolvedAsset).
			Msg("⚠️  No OTC version for this pair - regular market may be closed outside trading hours")
	}

	t.logger.Info().
		Str("requested", signal.Asset).
		Str("resolved", resolvedAsset).
		Bool("otc_24_7", isOTC).
		Int("active_id", activeID).
		Str("direction", string(signal.Direction)).
		Int("expiry_min", signal.Expiry).
		Float64("amount", amount).
		Msg("📡 Placing trade via WebSocket API...")

	// Get balance ID
	balanceID, err := t.getBalanceID()
	if err != nil {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, err
	}

	// direction string
	direction := "call"
	if signal.Direction == models.DirectionPut {
		direction = "put"
	}

	// For turbo options, `expired` is the Unix timestamp of the candle close.
	// Calculate the next candle boundary that gives us enough time to enter.
	// IQ Option closes the purchase window ~30 seconds before candle close.
	// Formula: find the next N-minute boundary from now + buffer.
	expiryTimestamp := calcExpiryTimestamp(signal.Expiry)

	t.logger.Info().
		Int64("expiry_ts", expiryTimestamp).
		Str("expiry_time", time.Unix(expiryTimestamp, 0).Format("15:04:05")).
		Msg("⏰ Calculated expiry timestamp")

	type openOptionMsg struct {
		Name    string      `json:"name"`
		Version string      `json:"version"`
		Body    interface{} `json:"body"`
	}

	type optionBody struct {
		UserBalanceID int64   `json:"user_balance_id"`
		ActiveID      int     `json:"active_id"`
		OptionTypeID  int     `json:"option_type_id"`
		Direction     string  `json:"direction"`
		Expired       int64   `json:"expired"`
		Price         float64 `json:"price"`
	}

	body := openOptionMsg{
		Name:    "binary-options.open-option",
		Version: "1.0",
		Body: optionBody{
			UserBalanceID: balanceID,
			ActiveID:      activeID,
			OptionTypeID:  3, // turbo option (1-5 min)
			Direction:     direction,
			Expired:       expiryTimestamp,
			Price:         amount,
		},
	}

	// IQ Option wraps commands in "sendMessage"
	resp, err := t.sendAndWait("sendMessage", body, "option")
	if err != nil {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, fmt.Errorf("open-option: %w", err)
	}

	// Parse response - server responds with name=option
	t.logger.Debug().RawJSON("raw_response", resp).Msg("← option response")

	var openResp struct {
		IsSuccessful bool   `json:"isSuccessful"`
		Message      string `json:"message"`
		ID           int64  `json:"id"`
		Result       struct {
			ID      int64  `json:"id"`
			Message string `json:"message"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &openResp); err != nil {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = fmt.Sprintf("parse response: %v", err)
		return trade, fmt.Errorf("parse open-option response: %w", err)
	}

	// A non-zero ID means success regardless of other fields
	optionID := openResp.ID
	if optionID == 0 {
		optionID = openResp.Result.ID
	}

	if optionID == 0 {
		// No ID = rejected - find the error message
		errMsg := openResp.Message
		if errMsg == "" {
			errMsg = openResp.Result.Message
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("unknown rejection (raw: %s)", string(resp))
		}
		trade.Status = models.StatusFailed
		trade.ErrorMsg = errMsg
		return trade, fmt.Errorf("trade rejected: %s", errMsg)
	}

	trade.Status = models.StatusOpen
	t.logger.Info().
		Int64("option_id", optionID).
		Str("asset", resolvedAsset).
		Str("direction", string(signal.Direction)).
		Float64("amount", amount).
		Msg("✅ Trade placed successfully!")

	return trade, nil
}

// calcExpiryTimestamp returns the Unix timestamp for the next candle close
// that gives at least 30 seconds of purchase window remaining.
// IQ Option's turbo `expired` field must be a candle-boundary timestamp.
//
// For a 2-minute expiry:
//   now=12:47:10 → next 2-min boundary is 12:48:00 → but that's only 50s away
//   if < 30s buffer remains before the boundary, use the NEXT one (12:50:00)
func calcExpiryTimestamp(expiryMinutes int) int64 {
	now := time.Now()
	periodSec := int64(expiryMinutes * 60)
	bufferSec := int64(30) // IQ Option requires ~30s before candle close

	// Find the next candle boundary
	nowUnix := now.Unix()
	// Round up to the next period boundary
	nextBoundary := (nowUnix/periodSec+1)*periodSec

	// If we have less than buffer time before that boundary, use the one after
	timeToNext := nextBoundary - nowUnix
	if timeToNext < bufferSec {
		nextBoundary += periodSec
	}

	return nextBoundary
}
