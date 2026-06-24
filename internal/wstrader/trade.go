package wstrader

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"signal-bot/pkg/models"
)

// Asset ID map for common pairs - IQ Option uses integer IDs
// These are the active_id values used in the WebSocket API
var assetIDs = map[string]int{
	// Forex OTC (available 24/7)
	"EURUSD-OTC":  76,
	"GBPUSD-OTC":  86,
	"USDJPY-OTC":  85,
	"AUDUSD-OTC":  272,
	"EURJPY-OTC":  273,
	"GBPJPY-OTC":  274,
	"NZDUSD-OTC":  284,
	"USDCAD-OTC":  283,
	"USDCHF-OTC":  282,
	"AUDCAD-OTC":  185,
	"EURCAD-OTC":  188,
	"EURGBP-OTC":  189,
	"GBPCAD-OTC":  199,
	"GBPNZD-OTC":  201,
	"EURAUD-OTC":  186,
	"AUDJPY-OTC":  184,
	"CADJPY-OTC":  187,
	"CHFJPY-OTC":  275,
	"GBPAUD-OTC":  198,
	"GBPCHF-OTC":  200,
	"NZDJPY-OTC":  202,
	"AUDNZD-OTC":  276,
	"CADCHF-OTC":  277,
	"EURCHF-OTC":  278,
	"EURNZD-OTC":  279,
	"AUDCHF-OTC":  280,
	"NZDCAD-OTC":  281,
	"USDNOK-OTC":  285,
	"USDSEK-OTC":  286,
	// Forex (market hours)
	"EURUSD": 1,
	"GBPUSD": 2,
	"USDJPY": 3,
	"AUDUSD": 4,
	"NZDUSD": 5,
	"USDCAD": 6,
	"USDCHF": 7,
	"EURJPY": 8,
	"GBPJPY": 9,
	"EURGBP": 10,
	"EURAUD": 11,
	"EURCAD": 12,
	"EURCHF": 13,
	"GBPAUD": 14,
	"GBPCAD": 15,
	"GBPCHF": 16,
	"AUDJPY": 17,
	"CADJPY": 18,
	"CHFJPY": 19,
	"AUDCAD": 20,
	"AUDCHF": 21,
	"AUDNZD": 22,
	"CADCHF": 23,
	"NZDJPY": 24,
	"NZDCAD": 25,
	"NZDCHF": 26,
}

// resolveAssetID returns the active_id for a given asset name
// Tries OTC first (available 24/7), then regular
func resolveAssetID(asset string) (int, bool) {
	asset = strings.ToUpper(strings.TrimSpace(asset))

	// Try exact match first
	if id, ok := assetIDs[asset]; ok {
		return id, true
	}

	// Try with -OTC suffix
	if id, ok := assetIDs[asset+"-OTC"]; ok {
		return id, true
	}

	// Try without -OTC suffix
	asset = strings.Replace(asset, "-OTC", "", 1)
	if id, ok := assetIDs[asset]; ok {
		return id, true
	}

	return 0, false
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
	defer t.balancesMu.RUnlock()

	targetType := 1 // real
	if t.cfg.DemoMode {
		targetType = 4 // practice
	}

	for _, b := range t.balances {
		if b.Type == targetType {
			return b.ID, nil
		}
	}
	return 0, fmt.Errorf("balance ID not found for account type %d", targetType)
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

	// Resolve asset ID
	activeID, ok := resolveAssetID(signal.Asset)
	if !ok {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = fmt.Sprintf("unknown asset: %s", signal.Asset)
		return trade, fmt.Errorf("unknown asset %s - add to assetIDs map", signal.Asset)
	}

	t.logger.Info().
		Str("asset", signal.Asset).
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
	resp, err := t.sendAndWait("sendMessage", body, "binary-options.open-option")
	if err != nil {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, fmt.Errorf("open-option: %w", err)
	}

	// Parse response
	var openResp struct {
		IsSuccessful bool   `json:"isSuccessful"`
		Message      string `json:"message"`
		Result       struct {
			ID        int64   `json:"id"`
			Asset     string  `json:"active"`
			Direction string  `json:"direction"`
			Price     float64 `json:"price"`
			ExpireAt  int64   `json:"expired"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &openResp); err != nil {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = fmt.Sprintf("parse response: %v", err)
		return trade, fmt.Errorf("parse open-option response: %w", err)
	}

	if !openResp.IsSuccessful {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = openResp.Message
		return trade, fmt.Errorf("trade rejected: %s", openResp.Message)
	}

	trade.Status = models.StatusOpen
	t.logger.Info().
		Int64("option_id", openResp.Result.ID).
		Str("asset", signal.Asset).
		Str("direction", string(signal.Direction)).
		Float64("amount", amount).
		Msg("✅ Trade placed successfully!")

	return trade, nil
}
