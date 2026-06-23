package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"signal-bot/pkg/models"
)

type Parser struct {
	patterns []*Pattern
}

type Pattern struct {
	regex      *regexp.Regexp
	extractor  func(matches []string) (*models.Signal, error)
}

func New() *Parser {
	return &Parser{
		patterns: []*Pattern{
			newMexyPattern(),
			newPattern1(),
			newPattern2(),
			newPattern3(),
		},
	}
}

func (p *Parser) Parse(text string) (*models.Signal, error) {
	text = strings.TrimSpace(text)
	
	// First try the detailed Mexy parser (handles full format with emojis)
	if strings.Contains(strings.ToUpper(text), "MEXY") || 
	   (strings.Contains(strings.ToUpper(text), "TIMEFRAME") && strings.Contains(strings.ToUpper(text), "CONFIDENCE")) {
		mexySignal, err := ParseMexyDetailed(text)
		if err == nil {
			// Copy EntryWindow into base signal so the bot can use it
			mexySignal.Signal.EntryWindow = mexySignal.EntryWindow
			return mexySignal.Signal, nil
		}
	}
	
	// Fall back to simple pattern matching
	textUpper := strings.ToUpper(text)
	for _, pattern := range p.patterns {
		if matches := pattern.regex.FindStringSubmatch(textUpper); matches != nil {
			signal, err := pattern.extractor(matches)
			if err != nil {
				continue
			}
			signal.Raw = text
			signal.ReceivedAt = time.Now()
			if signal.Confidence == 0 {
				signal.Confidence = 0.8 // default if not specified
			}
			return signal, nil
		}
	}

	return nil, fmt.Errorf("no pattern matched")
}

// Pattern 1: "EUR/USD CALL 5MIN"
func newPattern1() *Pattern {
	regex := regexp.MustCompile(`([A-Z]{3}/?[A-Z]{3})\s+(CALL|PUT)\s+(\d+)\s*MIN`)
	return &Pattern{
		regex: regex,
		extractor: func(matches []string) (*models.Signal, error) {
			asset := normalizeAsset(matches[1])
			direction := models.Direction(matches[2])
			expiry, _ := strconv.Atoi(matches[3])
			
			return &models.Signal{
				Asset:     asset,
				Direction: direction,
				Expiry:    expiry,
			}, nil
		},
	}
}

// Pattern 2: "EURUSD - CALL - 5M"
func newPattern2() *Pattern {
	regex := regexp.MustCompile(`([A-Z]{6})\s*-\s*(CALL|PUT)\s*-\s*(\d+)\s*M`)
	return &Pattern{
		regex: regex,
		extractor: func(matches []string) (*models.Signal, error) {
			asset := normalizeAsset(matches[1])
			direction := models.Direction(matches[2])
			expiry, _ := strconv.Atoi(matches[3])
			
			return &models.Signal{
				Asset:     asset,
				Direction: direction,
				Expiry:    expiry,
			}, nil
		},
	}
}

// Pattern 3: "BUY EURUSD 5 MINUTES"
func newPattern3() *Pattern {
	regex := regexp.MustCompile(`(BUY|SELL)\s+([A-Z]{3}/?[A-Z]{3})\s+(\d+)\s*MINUTES?`)
	return &Pattern{
		regex: regex,
		extractor: func(matches []string) (*models.Signal, error) {
			direction := models.DirectionCall
			if matches[1] == "SELL" {
				direction = models.DirectionPut
			}
			asset := normalizeAsset(matches[2])
			expiry, _ := strconv.Atoi(matches[3])
			
			return &models.Signal{
				Asset:     asset,
				Direction: direction,
				Expiry:    expiry,
			}, nil
		},
	}
}

// Mexy Binary Pattern: Multi-line format with AI confidence and martingale levels
func newMexyPattern() *Pattern {
	regex := regexp.MustCompile(`(?s).*?([A-Z]{3}/[A-Z]{3}).*?TIMEFRAME:\s*(\d+)-MIN.*?AI\s*CONFIDENCE:\s*(\d+)%.*?DIRECTION:\s*(BUY|SELL|CALL|PUT)`)
	return &Pattern{
		regex: regex,
		extractor: func(matches []string) (*models.Signal, error) {
			asset := normalizeAsset(matches[1])
			expiry, _ := strconv.Atoi(matches[2])
			confidence, _ := strconv.Atoi(matches[3])
			
			direction := models.DirectionCall
			dirStr := strings.ToUpper(matches[4])
			if dirStr == "SELL" || dirStr == "PUT" {
				direction = models.DirectionPut
			}
			
			return &models.Signal{
				Asset:      asset,
				Direction:  direction,
				Expiry:     expiry,
				Confidence: float64(confidence) / 100.0,
			}, nil
		},
	}
}

func normalizeAsset(asset string) string {
	// Remove OTC suffix
	asset = strings.ReplaceAll(asset, "(OTC)", "")
	asset = strings.ReplaceAll(asset, "OTC", "")
	asset = strings.TrimSpace(asset)
	asset = strings.ReplaceAll(asset, "/", "")
	asset = strings.ToUpper(asset)
	return asset
}
