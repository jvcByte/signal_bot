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
			newPattern1(),
			newPattern2(),
			newPattern3(),
		},
	}
}

func (p *Parser) Parse(text string) (*models.Signal, error) {
	text = strings.TrimSpace(text)
	
	// Try the detailed Mexy parser for both MEXY BINARY and JVCBYTE BLITZ formats
	textUpper := strings.ToUpper(text)
	if strings.Contains(textUpper, "MEXY") || strings.Contains(textUpper, "JVCBYTE") ||
	   (strings.Contains(textUpper, "TIMEFRAME") && strings.Contains(textUpper, "CONFIDENCE")) {
		mexySignal, err := ParseMexyDetailed(text)
		if err == nil {
			mexySignal.Signal.EntryWindow = mexySignal.EntryWindow
			for _, ml := range mexySignal.MartingaleLevels {
				mexySignal.Signal.MartingaleLevels = append(mexySignal.Signal.MartingaleLevels, models.MartingaleTime{
					Level: ml.Level,
					Time:  ml.Time,
				})
			}
			return mexySignal.Signal, nil
		}
	}
	
	// Fall back to simple pattern matching
	for _, pattern := range p.patterns {
		if matches := pattern.regex.FindStringSubmatch(strings.ToUpper(text)); matches != nil {
			signal, err := pattern.extractor(matches)
			if err != nil {
				continue
			}
			signal.Raw = text
			signal.ReceivedAt = time.Now()
			if signal.Confidence == 0 {
				signal.Confidence = 0.8
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

func normalizeAsset(asset string) string {
	// Remove OTC suffix
	asset = strings.ReplaceAll(asset, "(OTC)", "")
	asset = strings.ReplaceAll(asset, "OTC", "")
	asset = strings.TrimSpace(asset)
	asset = strings.ReplaceAll(asset, "/", "")
	asset = strings.ToUpper(asset)
	return asset
}
