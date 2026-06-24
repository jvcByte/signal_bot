// Package wstrader implements IQ Option trading via WebSocket API.
// No browser required - runs fully headless, suitable for server deployment.
package wstrader

import (
	"encoding/json"
	"net/http/cookiejar"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"signal-bot/internal/config"
)

const (
	iqOptionAPIURL = "https://auth.iqoption.com/api/v1.0/login"
	iqOptionWSURL  = "wss://iqoption.com/echo/websocket"
)

// WSMessage is the envelope for all WebSocket messages
type WSMessage struct {
	Name      string          `json:"name"`
	RequestID string          `json:"request_id,omitempty"`
	LocalTime int64           `json:"local_time,omitempty"`
	Msg       json.RawMessage `json:"msg"`
}

// Trader communicates with IQ Option via WebSocket API
type Trader struct {
	cfg       *config.IQOptionConfig
	logger    zerolog.Logger
	conn      *websocket.Conn
	jar       *cookiejar.Jar
	ssid      string

	requestID  atomic.Int64
	mu         sync.Mutex

	// pending responses keyed by request_id
	pending    map[string]chan json.RawMessage
	pendingMu  sync.Mutex

	// profile data populated after auth
	balances   []Balance
	balancesMu sync.RWMutex

	// profits (payout %) per asset/option-type
	profits    map[string]map[string]float64
	profitsMu  sync.RWMutex

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
		cfg:     cfg,
		logger:  logger,
		jar:     jar,
		pending: make(map[string]chan json.RawMessage),
		profits: make(map[string]map[string]float64),
		done:    make(chan struct{}),
	}
}
