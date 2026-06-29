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

	// Extract asset - handles:
	// "📉  GBP/JPY (OTC)"
	// "📈  UK100 (OTC)"
	// "📉  🇬🇧 GBP/JPY 🇯🇵 (OTC)"
	// "AUDUSD (OTC)"
	assetRegex := regexp.MustCompile(`(?:📈|📉)\s+(?:[^\s/A-Z0-9]+\s+)?([A-Z0-9]+(?:/[A-Z0-9]+)?)\s`)
	if matches := assetRegex.FindStringSubmatch(text); matches != nil {
		signal.Asset = normalizeAsset(matches[1])
	} else {
		// fallback: plain asset before (OTC)
		assetRegex2 := regexp.MustCompile(`([A-Z0-9]+(?:/[A-Z0-9]+)?)\s*\(OTC\)`)
		if matches := assetRegex2.FindStringSubmatch(text); matches != nil {
			signal.Asset = normalizeAsset(matches[1])
		} else {
			return nil, fmt.Errorf("asset not found")
		}
	}

	// Extract timeframe - supports both minutes and seconds
	// "30-sec expiry", "2-min expiry", "1-minute expiry"
	timeframeRegex := regexp.MustCompile(`(?i)🕒\s+timeframe:\s*(\d+)-(sec|min)|timeframe:\s*(\d+)-(sec|min)`)
	if matches := timeframeRegex.FindStringSubmatch(text); matches != nil {
		val := matches[1]
		unit := matches[2]
		if val == "" {
			val = matches[3]
			unit = matches[4]
		}
		expiry, _ := strconv.Atoi(val)
		if strings.HasPrefix(strings.ToLower(unit), "min") {
			expiry *= 60 // convert to seconds
		}
		signal.Expiry = expiry
	} else {
		return nil, fmt.Errorf("timeframe not found")
	}

	// Extract AI confidence
	confidenceRegex := regexp.MustCompile(`(?i)🤖\s+ai\s*confidence:\s*(\d+)%|ai\s*confidence:\s*(\d+)%`)
	if matches := confidenceRegex.FindStringSubmatch(text); matches != nil {
		val := matches[1]
		if val == "" {
			val = matches[2]
		}
		conf, _ := strconv.Atoi(val)
		signal.Confidence = float64(conf) / 100.0
	} else {
		signal.Confidence = 0.8
	}
	// Clamp confidence to valid range
	if signal.Confidence < 0 {
		signal.Confidence = 0
	}
	if signal.Confidence > 1 {
		signal.Confidence = 1
	}

	// Extract direction
	directionRegex := regexp.MustCompile(`(?i)direction:\s*(?:🟢|🔴)?\s*(BUY|SELL|CALL|PUT)`)
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

	// Optional analyzer metadata (from JVCBYTE signal generator)
	metaRegex := regexp.MustCompile(`(?i)score:\s*([0-9.+-]+)\s*\|\s*regime:\s*(\w+)`)
	if matches := metaRegex.FindStringSubmatch(text); matches != nil {
		signal.AnalyzerScore, _ = strconv.ParseFloat(matches[1], 64)
		signal.AnalyzerRegime = matches[2]
	}

	// Extract entry window
	entryRegex := regexp.MustCompile(`(?i)(?:🕰️\s+)?entry\s*window:\s*(\d{1,2}):(\d{2})\s*(AM|PM)`)
	if matches := entryRegex.FindStringSubmatch(text); matches != nil {
		hour, _ := strconv.Atoi(matches[1])
		minute, _ := strconv.Atoi(matches[2])
		meridiem := strings.ToUpper(matches[3])

		if meridiem == "PM" && hour != 12 {
			hour += 12
		} else if meridiem == "AM" && hour == 12 {
			hour = 0
		}

		// Validate time bounds
		if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			// Skip invalid time, leave EntryWindow zero
		} else {
			now := time.Now()
			signal.EntryWindow = time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

			// If entry window is in the past, assume it's for tomorrow
			if signal.EntryWindow.Before(now) {
				signal.EntryWindow = signal.EntryWindow.Add(24 * time.Hour)
			}
		}
	}

	// Extract martingale levels - handles "→  11:17 AM" and "→ 11:17 AM"
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
