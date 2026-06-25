package models

import "time"

type Signal struct {
	ID               string
	Asset            string
	Direction        Direction
	Expiry           int
	Amount           float64
	Confidence       float64
	Source           string
	EntryWindow      time.Time        // when to place the initial trade
	MartingaleLevels []MartingaleTime // re-entry times if trade loses
	ReceivedAt       time.Time
	ProcessedAt      *time.Time
	Raw              string
}

// MartingaleTime represents a martingale re-entry level
type MartingaleTime struct {
	Level int
	Time  time.Time
}

type Direction string

const (
	DirectionCall Direction = "CALL"
	DirectionPut  Direction = "PUT"
)

func (d Direction) String() string {
	return string(d)
}

func (d Direction) IsValid() bool {
	return d == DirectionCall || d == DirectionPut
}
