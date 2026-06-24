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

// resolveAssetID returns the active_id for a given asset name.
// Mexy signals are always OTC pairs so we always resolve to OTC first.
func resolveAssetID(asset string) (int, string, bool) {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	asset = strings.Replace(asset, "-OTC", "", 1) // normalize: strip OTC suffix if present

	// Always use OTC active_id (Mexy signals are OTC pairs, available 24/7)
	if id, ok := assetIDs[asset+"-OTC"]; ok {
		return id, asset + "-OTC", true
	}
	// Fall back to regular market only if no OTC version exists
	if id, ok := assetIDs[asset]; ok {
		return id, asset, true
	}
	return 0, "", false
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

	// Resolve asset ID - prefers OTC (24/7) over regular market
	activeID, resolvedAsset, ok := resolveAssetID(signal.Asset)
	if !ok {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = fmt.Sprintf("unknown asset: %s", signal.Asset)
		return trade, fmt.Errorf("unknown asset %s - add to assetIDs map in trade.go", signal.Asset)
	}

	t.logger.Info().
		Str("requested", signal.Asset).
		Str("resolved", resolvedAsset).
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
