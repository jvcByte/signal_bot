package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"signal-bot/pkg/models"
)

// MexySignal represents the full parsed Mexy Binary signal
type MexySignal struct {
	*models.Signal
	EntryWindow      time.Time
	MartingaleLevels []MartingaleLevel
}

type MartingaleLevel struct {
	Level int
	Time  time.Time
}

// ParseMexyDetailed extracts full Mexy signal with martingale levels
func ParseMexyDetailed(text string) (*MexySignal, error) {
	signal := &MexySignal{
		Signal: &models.Signal{
			Raw:        text,
			ReceivedAt: time.Now(),
			Source:     "telegram",
		},
		MartingaleLevels: []MartingaleLevel{},
	}

	// Extract asset (handle emoji flags around currency pairs)
	assetRegex := regexp.MustCompile(`(?:📈|📉|🇨🇦|🇯🇵|🇺🇸|🇪🇺|🇬🇧|🇦🇺|🇳🇿|🇨🇭)?\s*([A-Z]{3}/[A-Z]{3})\s*(?:🇨🇦|🇯🇵|🇺🇸|🇪🇺|🇬🇧|🇦🇺|🇳🇿|🇨🇭)?\s*(?:\(OTC\))?`)
	if matches := assetRegex.FindStringSubmatch(text); matches != nil {
		signal.Asset = normalizeAsset(matches[1])
	} else {
		return nil, fmt.Errorf("asset not found")
	}

	// Extract timeframe (handle emoji clock)
	timeframeRegex := regexp.MustCompile(`(?i)(?:🕒\s*)?timeframe:\s*(\d+)-min`)
	if matches := timeframeRegex.FindStringSubmatch(text); matches != nil {
		expiry, _ := strconv.Atoi(matches[1])
		signal.Expiry = expiry
	} else {
		return nil, fmt.Errorf("timeframe not found")
	}

	// Extract AI confidence (handle emoji robot)
	confidenceRegex := regexp.MustCompile(`(?i)(?:🤖\s*)?ai\s*confidence:\s*(\d+)%`)
	if matches := confidenceRegex.FindStringSubmatch(text); matches != nil {
		conf, _ := strconv.Atoi(matches[1])
		signal.Confidence = float64(conf) / 100.0
	} else {
		signal.Confidence = 0.8 // default
	}

	// Extract direction (handle emojis before direction word)
	directionRegex := regexp.MustCompile(`(?i)direction:\s*(?:🟢|🔴|📈|📉)?\s*(BUY|SELL|CALL|PUT)`)
	if matches := directionRegex.FindStringSubmatch(text); matches != nil {
		dirStr := strings.ToUpper(matches[1])
		if dirStr == "SELL" || dirStr == "PUT" {
			signal.Direction = models.DirectionPut
		} else {
			signal.Direction = models.DirectionCall
		}
	} else {
		return nil, fmt.Errorf("direction not found")
	}

	// Extract entry window (optional, handle emoji clock)
	entryRegex := regexp.MustCompile(`(?i)(?:🕰️\s*)?entry\s*window:\s*(\d{1,2}):(\d{2})\s*(AM|PM)`)
	if matches := entryRegex.FindStringSubmatch(text); matches != nil {
		hour, _ := strconv.Atoi(matches[1])
		minute, _ := strconv.Atoi(matches[2])
		meridiem := strings.ToUpper(matches[3])

		if meridiem == "PM" && hour != 12 {
			hour += 12
		} else if meridiem == "AM" && hour == 12 {
			hour = 0
		}

		now := time.Now()
		signal.EntryWindow = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		
		// If entry window is in the past, assume it's for tomorrow
		if signal.EntryWindow.Before(now) {
			signal.EntryWindow = signal.EntryWindow.Add(24 * time.Hour)
		}
	}

	// Extract martingale levels (optional)
	martingaleRegex := regexp.MustCompile(`(?i)level\s*(\d+)\s*→\s*(\d{1,2}):(\d{2})\s*(AM|PM)`)
	for _, matches := range martingaleRegex.FindAllStringSubmatch(text, -1) {
		level, _ := strconv.Atoi(matches[1])
		hour, _ := strconv.Atoi(matches[2])
		minute, _ := strconv.Atoi(matches[3])
		meridiem := strings.ToUpper(matches[4])

		if meridiem == "PM" && hour != 12 {
			hour += 12
		} else if meridiem == "AM" && hour == 12 {
			hour = 0
		}

		now := time.Now()
		levelTime := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		
		if levelTime.Before(now) {
			levelTime = levelTime.Add(24 * time.Hour)
		}

		signal.MartingaleLevels = append(signal.MartingaleLevels, MartingaleLevel{
			Level: level,
			Time:  levelTime,
		})
	}

	return signal, nil
}

// ShouldExecuteNow checks if the signal should be executed based on entry window
func (m *MexySignal) ShouldExecuteNow() bool {
	if m.EntryWindow.IsZero() {
		return true // No entry window specified, execute immediately
	}

	now := time.Now()
	// Allow execution within 2 minutes of entry window
	windowStart := m.EntryWindow.Add(-1 * time.Minute)
	windowEnd := m.EntryWindow.Add(2 * time.Minute)

	return now.After(windowStart) && now.Before(windowEnd)
}

// GetNextMartingaleLevel returns the next martingale level to execute, if any
func (m *MexySignal) GetNextMartingaleLevel() *MartingaleLevel {
	if len(m.MartingaleLevels) == 0 {
		return nil
	}

	now := time.Now()
	for i := range m.MartingaleLevels {
		levelTime := m.MartingaleLevels[i].Time
		windowStart := levelTime.Add(-1 * time.Minute)
		windowEnd := levelTime.Add(2 * time.Minute)

		if now.After(windowStart) && now.Before(windowEnd) {
			return &m.MartingaleLevels[i]
		}
	}

	return nil
}
