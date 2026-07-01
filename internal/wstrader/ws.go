package wstrader

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// readLoop continuously reads messages from the WebSocket and routes them
func (t *Trader) readLoop() {
	defer close(t.done)
	for {
		_, data, err := t.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				t.logger.Info().Msg("WebSocket closed normally")
			} else {
				t.logger.Error().Err(err).Msg("WebSocket read error")
			}
			return
		}

		var msg struct {
			Name      string          `json:"name"`
			RequestID string          `json:"request_id"`
			Msg       json.RawMessage `json:"msg"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		t.logger.Debug().Str("name", msg.Name).Str("request_id", msg.RequestID).Msg("← WS received")

		// Try to route by request_id first
		if msg.RequestID != "" {
			t.pendingMu.Lock()
			ch, ok := t.pending[msg.RequestID]
			t.pendingMu.Unlock()
			if ok {
				select {
				case ch <- msg.Msg:
				default:
				}
				continue
			}
		}

		// Route by message name - server often responds without request_id
		t.pendingMu.Lock()
		ch, ok := t.pending["name:"+msg.Name]
		t.pendingMu.Unlock()
		if ok {
			select {
			case ch <- msg.Msg:
			default:
			}
			continue
		}

		// Handle push messages
		t.routeByName(msg.Name, msg.Msg)
	}
}

func (t *Trader) routeByName(name string, msg json.RawMessage) {
	switch name {
	case "profile":
		var profile struct {
			Balances []Balance `json:"balances"`
		}
		if err := json.Unmarshal(msg, &profile); err == nil && len(profile.Balances) > 0 {
			t.balancesMu.Lock()
			t.balances = profile.Balances
			t.balancesMu.Unlock()
			t.logger.Debug().Int("count", len(profile.Balances)).Msg("balances updated from profile push")
		}

	case "option":
		// Trade open confirmation - ignore (no result yet)

	case "option-closed", "socket-option-closed":
		t.handleOptionPush(msg)

	case "option-changed", "option-archived":
		// Intermediate updates - ignore to avoid double-counting

	case "heartbeat", "timeSync", "timesync":
		// ignore

	default:
		t.logger.Debug().Str("name", name).Msg("← unhandled push message")
	}
}

// handleOptionPush processes option result pushes from IQ Option.
// Two different message formats arrive - we handle both.
func (t *Trader) handleOptionPush(msg json.RawMessage) {
	// Try format 2: socket-option-closed (win field, amount in micro-units)
	var f2 struct {
		ID        int64  `json:"id"`
		OptionID  int64  `json:"option_id"` // format 1 fallback
		Win       string `json:"win"`        // "win" or "loose"
		Result    string `json:"result"`     // format 1: "win" or "loose"
		Amount    int64  `json:"amount"`     // micro-units in format 2, dollars in format 1
		WinAmount string `json:"win_amount"` // string in format 2
		Stake     float64 `json:"enrolled_amount"` // format 1 stake in dollars
	}

	if err := json.Unmarshal(msg, &f2); err != nil {
		return
	}

	// Determine option ID
	optionID := f2.ID
	if optionID == 0 {
		optionID = f2.OptionID
	}
	if optionID == 0 {
		return
	}

	// Determine result
	result := f2.Win
	if result == "" {
		result = f2.Result
	}
	if result == "" {
		return // no result yet (trade still open)
	}

	t.openTradesMu.Lock()
	trade, exists := t.openTrades[optionID]
	if exists {
		delete(t.openTrades, optionID)
	}
	t.openTradesMu.Unlock()

	if !exists {
		return // not our trade
	}

	win := result == "win"

	// Calculate profit
	// Format 2: amount is micro-units
	// Format 1: amount is dollars directly
	var stake float64
	if f2.Amount > 1000 {
		stake = float64(f2.Amount) / 1_000_000
	} else if f2.Stake > 0 {
		stake = f2.Stake
	} else {
		stake = trade.amount
	}

	var profit float64
	if win {
		// Parse win_amount (string in format 2)
		var winAmt float64
		fmt.Sscanf(f2.WinAmount, "%f", &winAmt)
		if winAmt > 0 {
			profit = winAmt - stake
		} else {
			profit = stake * 0.86 // fallback: 86% payout
		}
	} else {
		profit = -stake
	}

	resultStr := "LOSS ❌"
	if win {
		resultStr = "WIN  ✅"
	}

	t.logger.Info().
		Int64("option_id", optionID).
		Str("result", resultStr).
		Float64("stake", stake).
		Float64("profit", profit).
		Msg("🏁 Trade result received")

	if t.onResult != nil {
		t.onResult(TradeResult{
			OptionID: optionID,
			TradeID:  trade.tradeID,
			Win:      win,
			Profit:   profit,
			ClosedAt: time.Now(),
			Signal:   trade.signal,
			Amount:   stake,
		})
	}
}

// send sends a message and returns the request_id
func (t *Trader) send(name string, body interface{}) (string, error) {
	reqID := strconv.FormatInt(t.requestID.Add(1), 10)
	return reqID, t.writeEnvelope(name, body, reqID)
}

// sendAndWait sends a message and waits for a response matching by request_id or name
func (t *Trader) sendAndWait(name string, body interface{}, waitForName string) (json.RawMessage, error) {
	reqID := strconv.FormatInt(t.requestID.Add(1), 10)

	ch := make(chan json.RawMessage, 1)

	t.pendingMu.Lock()
	t.pending[reqID] = ch
	t.pending["name:"+waitForName] = ch
	t.pendingMu.Unlock()

	defer func() {
		t.pendingMu.Lock()
		delete(t.pending, reqID)
		delete(t.pending, "name:"+waitForName)
		t.pendingMu.Unlock()
	}()

	if err := t.writeEnvelope(name, body, reqID); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	t.logger.Debug().Str("name", name).Str("request_id", reqID).Msg("→ WS send+wait")

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(45 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response to '%s'", name)
	}
}

// writeEnvelope marshals and sends the WebSocket message
func (t *Trader) writeEnvelope(name string, body interface{}, reqID string) error {
	var msgRaw json.RawMessage
	var err error

	// body can be a plain string (e.g. ssid value) or a struct
	switch v := body.(type) {
	case string:
		msgRaw, err = json.Marshal(v)
	case json.RawMessage:
		msgRaw = v
	default:
		msgRaw, err = json.Marshal(body)
	}
	if err != nil {
		return err
	}

	envelope := map[string]interface{}{
		"name":       name,
		"request_id": reqID,
		"local_time": time.Now().UnixMilli(),
		"msg":        msgRaw,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}

	t.mu.Lock()
	err = t.conn.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	return err
}

// Close cleanly shuts down the WebSocket connection
func (t *Trader) Close() error {
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}
