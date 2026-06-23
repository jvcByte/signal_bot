package models

import "time"

type Signal struct {
	ID          string
	Asset       string
	Direction   Direction
	Expiry      int
	Amount      float64
	Confidence  float64
	Source      string
	ReceivedAt  time.Time
	ProcessedAt *time.Time
	Raw         string
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
