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
		// balances updated push
		var profile struct {
			Balances []Balance `json:"balances"`
		}
		if err := json.Unmarshal(msg, &profile); err == nil && len(profile.Balances) > 0 {
			t.balancesMu.Lock()
			t.balances = profile.Balances
			t.balancesMu.Unlock()
		}
	case "timeSync":
		// server time sync - ignore
	default:
		t.logger.Debug().Str("name", name).Msg("← unhandled push message")
	}
}

// send sends a message and returns the request_id
func (t *Trader) send(name string, body interface{}) (string, error) {
	reqID := strconv.FormatInt(t.requestID.Add(1), 10)

	msgBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	envelope := map[string]interface{}{
		"name":       name,
		"request_id": reqID,
		"local_time": time.Now().UnixMilli(),
		"msg":        json.RawMessage(msgBody),
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}

	t.logger.Debug().Str("name", name).Str("request_id", reqID).Msg("→ WS send")

	t.mu.Lock()
	err = t.conn.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()

	return reqID, err
}

// sendAndWait sends a message and waits for a response matching by request_id or name
func (t *Trader) sendAndWait(name string, body interface{}, waitForName string) (json.RawMessage, error) {
	reqID := strconv.FormatInt(t.requestID.Add(1), 10)

	msgBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	envelope := map[string]interface{}{
		"name":       name,
		"request_id": reqID,
		"local_time": time.Now().UnixMilli(),
		"msg":        json.RawMessage(msgBody),
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}

	ch := make(chan json.RawMessage, 1)

	// Register under both request_id AND name so we catch either routing
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

	t.logger.Debug().Str("name", name).Str("request_id", reqID).Msg("→ WS send+wait")

	t.mu.Lock()
	err = t.conn.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response to '%s'", name)
	}
}

// Close cleanly shuts down the WebSocket connection
func (t *Trader) Close() error {
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}
