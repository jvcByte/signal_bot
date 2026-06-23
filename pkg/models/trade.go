package models

import "time"

type Trade struct {
	ID         string
	SignalID   string
	Asset      string
	Direction  Direction
	Amount     float64
	Expiry     int
	Status     TradeStatus
	Result     TradeResult
	Profit     float64
	PlacedAt   time.Time
	ClosedAt   *time.Time
	ErrorMsg   string
}

type TradeStatus string

const (
	StatusPending  TradeStatus = "PENDING"
	StatusOpen     TradeStatus = "OPEN"
	StatusClosed   TradeStatus = "CLOSED"
	StatusFailed   TradeStatus = "FAILED"
	StatusCanceled TradeStatus = "CANCELED"
)

type TradeResult string

const (
	ResultWin  TradeResult = "WIN"
	ResultLose TradeResult = "LOSE"
	ResultTie  TradeResult = "TIE"
	ResultNone TradeResult = "NONE"
)
