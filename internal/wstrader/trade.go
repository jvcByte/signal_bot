package wstrader

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"signal-bot/pkg/models"
)

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
			return b.TotalAmount(), nil
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
		ID:        uuid.New().String(), // stable ID used for DB and result matching
		SignalID:  signal.ID,
		Asset:    signal.Asset,
		Direction: signal.Direction,
		Amount:   amount,
		Expiry:   signal.Expiry,
		Status:   models.StatusPending,
		Result:   models.ResultNone,
		PlacedAt: time.Now(),
	}

	// Get asset ID dynamically from IQ Option API (cached after first lookup)
	activeID, resolvedAsset, isOTC, found := t.getActiveIDFromAPI(signal.Asset)
	if !found {
		trade.Status = models.StatusFailed
		trade.ErrorMsg = fmt.Sprintf("asset not available: %s", signal.Asset)
		return trade, fmt.Errorf("asset %s not found in IQ Option's available instruments", signal.Asset)
	}

	t.logger.Info().
		Str("requested", signal.Asset).
		Str("resolved", resolvedAsset).
		Bool("is_otc", isOTC).
		Int("active_id", activeID).
		Str("direction", string(signal.Direction)).
		Int("expiry_sec", signal.Expiry).
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
		UserBalanceID  int64   `json:"user_balance_id"`
		ActiveID       int     `json:"active_id"`
		OptionTypeID   int     `json:"option_type_id"`
		Direction      string  `json:"direction"`
		Expired        int64   `json:"expired"`
		ExpirationSize int     `json:"expiration_size"`
		Price          float64 `json:"price"`
		RefundValue    int     `json:"refund_value"`
		ProfitPercent  int     `json:"profit_percent"`
	}

	body := openOptionMsg{
		Name:    "binary-options.open-option",
		Version: "2.0",
		Body: optionBody{
			UserBalanceID:  balanceID,
			ActiveID:       activeID,
			OptionTypeID:   12, // 12 = Blitz option
			Direction:      direction,
			Expired:        expiryTimestamp,
			ExpirationSize: signal.Expiry, // already in seconds
			Price:          amount,
			RefundValue:    0,
			ProfitPercent:  87,
		},
	}

	// IQ Option wraps commands in "sendMessage"
	// Retry once on profit rate change rejection (payout shifts dynamically)
	var resp json.RawMessage
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			// Recalculate expiry timestamp on retry
			expiryTimestamp = calcExpiryTimestamp(signal.Expiry)
			body.Body = optionBody{
				UserBalanceID:  balanceID,
				ActiveID:       activeID,
				OptionTypeID:   12,
				Direction:      direction,
				Expired:        expiryTimestamp,
				ExpirationSize: signal.Expiry,
				Price:          amount,
				RefundValue:    0,
				ProfitPercent:  87,
			}
			t.logger.Debug().Int("attempt", attempt+1).Msg("Retrying with fresh expiry timestamp...")
			time.Sleep(500 * time.Millisecond)
		}

		resp, err = t.sendAndWait("sendMessage", body, "option")
		if err != nil {
			continue
		}

		// Check if rejection is due to profit rate change - retry if so
		var quickCheck struct {
			Message string `json:"message"`
			Result  struct {
				Message string `json:"message"`
			} `json:"result"`
		}
		if json.Unmarshal(resp, &quickCheck) == nil {
			msg := quickCheck.Message
			if msg == "" {
				msg = quickCheck.Result.Message
			}
			if strings.Contains(msg, "profit rate") {
				err = fmt.Errorf("profit_rate_change")
				continue
			}
		}
		break // success or non-retryable error
	}

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
		
		// Add context for common errors
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "Active") {
			if !isOTC {
				errMsg = fmt.Sprintf("%s (market likely closed - %s has no OTC version for 24/7 trading)", errMsg, signal.Asset)
			}
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

	// Register open trade so we can match the result when it closes
	t.openTradesMu.Lock()
	t.openTrades[optionID] = openTrade{
		tradeID: trade.ID,
		amount:  amount,
		signal:  signal,
	}
	t.openTradesMu.Unlock()

	return trade, nil
}

// calcExpiryTimestamp returns the Unix timestamp when the blitz option expires.
// expirySeconds is already in seconds.
func calcExpiryTimestamp(expirySeconds int) int64 {
	return time.Now().Unix() + int64(expirySeconds)
}
