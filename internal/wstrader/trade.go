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
			return b.realAmount(), nil
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
		t.logger.Debug().Int64("id", b.ID).Int("type", b.Type).Float64("amount", b.realAmount()).Msg("checking balance")
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

	// option_type_id: 3 = turbo (expiry 1-5 min), 1 = binary (expiry 5+ min)
	optionTypeID := 3 // turbo
	if signal.Expiry > 5 {
		optionTypeID = 1 // binary
	}

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
		Expired       int     `json:"expired"`
		Price         float64 `json:"price"`
	}

	body := openOptionMsg{
		Name:    "binary-options.open-option",
		Version: "1.0",
		Body: optionBody{
			UserBalanceID: balanceID,
			ActiveID:      activeID,
			OptionTypeID:  optionTypeID,
			Direction:     direction,
			Expired:       signal.Expiry,
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
