// Package wstrader implements IQ Option trading via WebSocket API.
// No browser required - runs fully headless, suitable for server deployment.
package wstrader

import (
	"encoding/json"
	"net/http/cookiejar"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"signal-bot/internal/config"
	"signal-bot/pkg/models"
)

const (
	iqOptionAPIURL = "https://auth.iqoption.com/api/v1.0/login"
	iqOptionWSURL  = "wss://iqoption.com/echo/websocket"
)

// TradeResult is delivered to the ResultHandler when a trade closes
type TradeResult struct {
	OptionID   int64
	TradeID    string          // our internal DB trade ID
	Win        bool
	Profit     float64         // net profit (positive=win, negative=loss)
	ClosedAt   time.Time
	Signal     *models.Signal  // original signal (for martingale)
	Amount     float64         // amount that was traded
}

// ResultHandler is called when a trade result is received
type ResultHandler func(result TradeResult)

// openTrade tracks a pending trade waiting for its result
type openTrade struct {
	tradeID string
	amount  float64
	signal  *models.Signal
}

// Trader communicates with IQ Option via WebSocket API
type Trader struct {
	cfg    *config.IQOptionConfig
	logger zerolog.Logger
	conn   *websocket.Conn
	jar    *cookiejar.Jar
	ssid   string

	requestID atomic.Int64
	mu        sync.Mutex

	// pending responses keyed by request_id or "name:<name>"
	pending   map[string]chan json.RawMessage
	pendingMu sync.Mutex

	// open trades waiting for result: optionID -> openTrade
	openTrades   map[int64]openTrade
	openTradesMu sync.Mutex

	// called when a trade result arrives
	onResult ResultHandler

	// profile data populated after auth
	balances   []Balance
	balancesMu sync.RWMutex

	profits   map[string]map[string]float64
	profitsMu sync.RWMutex

	done chan struct{}
}

type Balance struct {
	ID       int64   `json:"id"`
	Type     int     `json:"type"` // 1=real, 4=practice
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// realAmount converts IQ Option's internal amount (cents) to actual value
func (b Balance) realAmount() float64 {
	return b.Amount / 100.0
}

func New(cfg *config.IQOptionConfig, logger zerolog.Logger) *Trader {
	jar, _ := cookiejar.New(nil)
	return &Trader{
		cfg:        cfg,
		logger:     logger,
		jar:        jar,
		pending:    make(map[string]chan json.RawMessage),
		openTrades: make(map[int64]openTrade),
		profits:    make(map[string]map[string]float64),
		done:       make(chan struct{}),
	}
}

// SetResultHandler registers a callback for when trades close
func (t *Trader) SetResultHandler(h ResultHandler) {
	t.onResult = h
}
