package wstrader

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Candle represents OHLCV data for technical analysis
type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// CandleStore maintains historical candles for analysis
type CandleStore struct {
	mu      sync.RWMutex
	candles map[string][]Candle // map[asset]candles
}

// SubscribeCandles subscribes to real-time candle updates for an asset
func (t *Trader) SubscribeCandles(asset string, timeframe int) error {
	// Get the active_id for the asset
	activeID, _, _, found := t.getActiveIDFromAPI(asset)
	if !found {
		return fmt.Errorf("asset %s not found", asset)
	}

	// Subscribe to candles
	type subscribeCandlesMsg struct {
		Name    string      `json:"name"`
		Version string      `json:"version"`
		Body    interface{} `json:"body"`
	}

	msg := subscribeCandlesMsg{
		Name:    "subscribeMessage",
		Version: "2.0",
		Body: map[string]interface{}{
			"name": "candle-generated",
			"params": map[string]interface{}{
				"routingFilters": map[string]interface{}{
					"active_id": activeID,
					"size":      timeframe, // 60 = 1 minute candles
				},
			},
		},
	}

	t.mu.Lock()
	err := t.conn.WriteJSON(msg)
	t.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to subscribe to candles: %w", err)
	}

	t.logger.Info().
		Str("asset", asset).
		Int("active_id", activeID).
		Int("timeframe", timeframe).
		Msg("✓ Subscribed to candle updates")

	return nil
}

// GetHistoricalCandles fetches historical candles for backtesting/analysis
func (t *Trader) GetHistoricalCandles(asset string, timeframe int, count int) ([]Candle, error) {
	activeID, _, _, found := t.getActiveIDFromAPI(asset)
	if !found {
		return nil, fmt.Errorf("asset %s not found", asset)
	}

	// Request historical candles
	type getCandlesMsg struct {
		Name    string      `json:"name"`
		Version string      `json:"version"`
		Body    interface{} `json:"body"`
	}

	endTime := time.Now().Unix()
	startTime := endTime - int64(count*timeframe)

	msg := getCandlesMsg{
		Name:    "get-candles",
		Version: "2.0",
		Body: map[string]interface{}{
			"active_id": activeID,
			"size":      timeframe,
			"from":      startTime,
			"to":        endTime,
			"count":     count,
		},
	}

	resp, err := t.sendAndWait("sendMessage", msg, "candles")
	if err != nil {
		return nil, fmt.Errorf("failed to get candles: %w", err)
	}

	// Parse candles response
	var result struct {
		Candles []struct {
			From   int64   `json:"from"`
			To     int64   `json:"to"`
			Open   float64 `json:"open"`
			Close  float64 `json:"close"`
			Min    float64 `json:"min"`
			Max    float64 `json:"max"`
			Volume float64 `json:"volume"`
		} `json:"candles"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		t.logger.Debug().RawJSON("raw_response", resp).Msg("Failed to parse candles")
		return nil, fmt.Errorf("failed to parse candles: %w", err)
	}

	candles := make([]Candle, len(result.Candles))
	for i, c := range result.Candles {
		candles[i] = Candle{
			Time:   time.Unix(c.From, 0),
			Open:   c.Open,
			High:   c.Max,
			Low:    c.Min,
			Close:  c.Close,
			Volume: c.Volume,
		}
	}

	if len(candles) == 0 {
		return nil, fmt.Errorf("no candles returned for %s (WebSocket may have disconnected)", asset)
	}

	t.logger.Info().
		Str("asset", asset).
		Int("count", len(candles)).
		Time("from", candles[0].Time).
		Time("to", candles[len(candles)-1].Time).
		Msg("✓ Historical candles loaded")

	return candles, nil
}

// NewCandleStore creates a new candle store
func NewCandleStore() *CandleStore {
	return &CandleStore{
		candles: make(map[string][]Candle),
	}
}

// Add adds a candle to the store
func (cs *CandleStore) Add(asset string, candle Candle) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.candles[asset] == nil {
		cs.candles[asset] = []Candle{}
	}

	cs.candles[asset] = append(cs.candles[asset], candle)

	// Keep only last 200 candles per asset (saves memory)
	if len(cs.candles[asset]) > 200 {
		cs.candles[asset] = cs.candles[asset][1:]
	}
}

// GetRecent returns the most recent N candles for an asset
func (cs *CandleStore) GetRecent(asset string, count int) []Candle {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	candles := cs.candles[asset]
	if len(candles) == 0 {
		return []Candle{}
	}

	if count > len(candles) {
		count = len(candles)
	}

	return candles[len(candles)-count:]
}

// GetAll returns all stored candles for an asset
func (cs *CandleStore) GetAll(asset string) []Candle {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	return cs.candles[asset]
}
