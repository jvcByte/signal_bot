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
		// profile push contains balances
		var profile struct {
			Balances []Balance `json:"balances"`
		}
		if err := json.Unmarshal(msg, &profile); err == nil && len(profile.Balances) > 0 {
			t.balancesMu.Lock()
			t.balances = profile.Balances
			t.balancesMu.Unlock()
			t.logger.Debug().Int("count", len(profile.Balances)).Msg("balances updated from profile push")
		}
	case "heartbeat", "timeSync", "timesync":
		// ignore
	default:
		t.logger.Debug().Str("name", name).Msg("← unhandled push message")
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
	case <-time.After(15 * time.Second):
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
