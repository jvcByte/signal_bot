package analyzer

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"signal-bot/internal/indicators"
	"signal-bot/internal/wstrader"
	"signal-bot/pkg/models"
)

// SignalAnalyzer generates trading signals from multi-factor analysis
type SignalAnalyzer struct {
	trader      *wstrader.Trader
	logger      zerolog.Logger
	config      AnalyzerConfig
	lastSignals map[string]time.Time
	signalsMu   sync.RWMutex
}

// AnalyzerConfig contains strategy parameters
type AnalyzerConfig struct {
	RSIPeriod        int
	RSIOversold      float64
	RSIOverbought    float64
	FastMAPeriod     int
	SlowMAPeriod     int
	MinConfidence    float64
	ExpiryMinutes    int
	EnableMartingale bool
	SignalCooldown   int // minutes
	SignalThreshold  int // min factors agreeing to produce a signal (default: 5)
	// Bollinger Bands
	BBPeriod  int
	BBStdDev  float64
	// Volume
	VolumePeriod int
	VolumeMultiplier float64 // volume must be X times avg to confirm
	// Multi-timeframe
	EnableMTF bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() AnalyzerConfig {
	return AnalyzerConfig{
		RSIPeriod:        14,
		RSIOversold:      30,
		RSIOverbought:    70,
		FastMAPeriod:     10,
		SlowMAPeriod:     20,
		MinConfidence:    0.70,
		ExpiryMinutes:    2,
		EnableMartingale: true,
		SignalCooldown:   7,
		SignalThreshold:  5, // default: require 5/9 factors
		BBPeriod:         20,
		BBStdDev:         2.0,
		VolumePeriod:     14,
		VolumeMultiplier: 1.2,
		EnableMTF:        true,
	}
}

// New creates a new signal analyzer
func New(trader *wstrader.Trader, logger zerolog.Logger, config AnalyzerConfig) *SignalAnalyzer {
	return &SignalAnalyzer{
		trader:      trader,
		logger:      logger,
		config:      config,
		lastSignals: make(map[string]time.Time),
	}
}

// AnalysisResult holds the full breakdown of an analysis
type AnalysisResult struct {
	Asset      string
	Direction  models.Direction
	Confidence float64
	Reasons    []string
	Score      int // votes for direction: positive = bullish, negative = bearish
}

// AnalyzeAsset runs multi-factor analysis on an asset
func (a *SignalAnalyzer) AnalyzeAsset(asset string) (*models.Signal, error) {
	// Cooldown check
	a.signalsMu.RLock()
	lastSignalTime, exists := a.lastSignals[asset]
	a.signalsMu.RUnlock()
	if exists && time.Since(lastSignalTime) < time.Duration(a.config.SignalCooldown)*time.Minute {
		return nil, nil
	}

	// ── 1. Fetch candles: 1m timeframe (100 candles for reliable indicators)
	candles1m, err := a.trader.GetHistoricalCandles(asset, 60, 100)
	if err != nil || len(candles1m) < 30 {
		return nil, fmt.Errorf("insufficient 1m candles for %s: %w", asset, err)
	}

	// ── 2. Fetch 5m and 15m candles for multi-timeframe (MTF) trend
	// These change slowly so we fetch fewer candles
	var candles5m []wstrader.Candle
	var candles15m []wstrader.Candle
	if a.config.EnableMTF {
		candles5m, _ = a.trader.GetHistoricalCandles(asset, 300, 30)
		candles15m, _ = a.trader.GetHistoricalCandles(asset, 900, 20)
	}

	// Extract arrays from 1m data
	closes := extractField(candles1m, func(c wstrader.Candle) float64 { return c.Close })
	opens  := extractField(candles1m, func(c wstrader.Candle) float64 { return c.Open })
	vols   := extractField(candles1m, func(c wstrader.Candle) float64 { return c.Volume })

	result := &AnalysisResult{Asset: asset, Reasons: []string{}}

	// ── FACTOR 1: RSI (1m)
	rsi := indicators.RSI(closes, a.config.RSIPeriod)
	if rsi < a.config.RSIOversold {
		result.Score++
		result.Reasons = append(result.Reasons, fmt.Sprintf("RSI oversold (%.1f)", rsi))
	} else if rsi > a.config.RSIOverbought {
		result.Score--
		result.Reasons = append(result.Reasons, fmt.Sprintf("RSI overbought (%.1f)", rsi))
	}

	// ── FACTOR 2: Moving Average trend (1m)
	fastMA := indicators.EMA(closes, a.config.FastMAPeriod)
	slowMA := indicators.EMA(closes, a.config.SlowMAPeriod)
	fastMAPrev := indicators.EMA(closes[:len(closes)-1], a.config.FastMAPeriod)
	slowMAPrev := indicators.EMA(closes[:len(closes)-1], a.config.SlowMAPeriod)

	if indicators.IsBullishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		result.Score++
		result.Reasons = append(result.Reasons, "EMA bullish crossover (1m)")
	} else if indicators.IsBearishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		result.Score--
		result.Reasons = append(result.Reasons, "EMA bearish crossover (1m)")
	} else if fastMA > slowMA {
		result.Score++
		result.Reasons = append(result.Reasons, "Bullish EMA alignment (1m)")
	} else {
		result.Score--
		result.Reasons = append(result.Reasons, "Bearish EMA alignment (1m)")
	}

	// ── FACTOR 3: Bollinger Bands - price at extreme + squeeze detection
	bbMid, bbUpper, bbLower := indicators.BollingerBands(closes, a.config.BBPeriod, a.config.BBStdDev)
	bbWidth := indicators.BandWidth(bbMid, bbUpper, bbLower)
	currentPrice := closes[len(closes)-1]

	if currentPrice <= bbLower {
		result.Score++
		result.Reasons = append(result.Reasons, fmt.Sprintf("Price at lower BB (width=%.4f%%)", bbWidth))
	} else if currentPrice >= bbUpper {
		result.Score--
		result.Reasons = append(result.Reasons, fmt.Sprintf("Price at upper BB (width=%.4f%%)", bbWidth))
	}

	// Bollinger squeeze: bands very tight = volatility about to expand
	// Use as extra weight when price breaks out after squeeze
	isSqueeze := bbWidth < 0.05 // very tight bands
	if isSqueeze {
		result.Reasons = append(result.Reasons, fmt.Sprintf("BB squeeze detected (width=%.4f%%)", bbWidth))
	}

	// ── FACTOR 4: MACD
	macdLine, macdSignal, macdHist := indicators.MACD(closes, 12, 26, 9)
	if macdHist > 0 && macdLine > macdSignal {
		result.Score++
		result.Reasons = append(result.Reasons, fmt.Sprintf("MACD bullish (hist=%.5f)", macdHist))
	} else if macdHist < 0 && macdLine < macdSignal {
		result.Score--
		result.Reasons = append(result.Reasons, fmt.Sprintf("MACD bearish (hist=%.5f)", macdHist))
	}

	// ── FACTOR 5: Volume confirmation
	avgVol := indicators.AvgVolume(vols, a.config.VolumePeriod)
	lastVol := vols[len(vols)-1]
	volumeConfirmed := avgVol > 0 && lastVol >= avgVol*a.config.VolumeMultiplier
	if volumeConfirmed {
		result.Reasons = append(result.Reasons, fmt.Sprintf("Volume surge (%.1fx avg)", lastVol/avgVol))
	}

	// ── FACTOR 6: Candlestick patterns (1m)
	if indicators.IsBullishEngulfing(opens, closes) {
		result.Score++
		result.Reasons = append(result.Reasons, "Bullish engulfing candle")
	} else if indicators.IsBearishEngulfing(opens, closes) {
		result.Score--
		result.Reasons = append(result.Reasons, "Bearish engulfing candle")
	}

	lastCandle := candles1m[len(candles1m)-1]
	if indicators.IsBullishPinBar(lastCandle.Open, lastCandle.High, lastCandle.Low, lastCandle.Close) {
		result.Score++
		result.Reasons = append(result.Reasons, "Bullish pin bar (hammer)")
	} else if indicators.IsBearishPinBar(lastCandle.Open, lastCandle.High, lastCandle.Low, lastCandle.Close) {
		result.Score--
		result.Reasons = append(result.Reasons, "Bearish pin bar (shooting star)")
	}

	// ── FACTOR 7: Multi-timeframe trend (5m + 15m)
	mtfTrend := 0
	if len(candles5m) >= 20 {
		closes5m := extractField(candles5m, func(c wstrader.Candle) float64 { return c.Close })
		trend5m := indicators.Trend(closes5m, 8, 21)
		mtfTrend += trend5m
		if trend5m > 0 {
			result.Reasons = append(result.Reasons, "5m trend: BULLISH")
		} else if trend5m < 0 {
			result.Reasons = append(result.Reasons, "5m trend: BEARISH")
		}
	}

	if len(candles15m) >= 20 {
		closes15m := extractField(candles15m, func(c wstrader.Candle) float64 { return c.Close })
		trend15m := indicators.Trend(closes15m, 8, 21)
		mtfTrend += trend15m
		if trend15m > 0 {
			result.Reasons = append(result.Reasons, "15m trend: BULLISH")
		} else if trend15m < 0 {
			result.Reasons = append(result.Reasons, "15m trend: BEARISH")
		}
	}

	// If MTF trends conflict (5m bullish but 15m bearish) → no signal
	if len(candles5m) >= 20 && len(candles15m) >= 20 {
		closes5m  := extractField(candles5m, func(c wstrader.Candle) float64 { return c.Close })
		closes15m := extractField(candles15m, func(c wstrader.Candle) float64 { return c.Close })
		t5  := indicators.Trend(closes5m, 8, 21)
		t15 := indicators.Trend(closes15m, 8, 21)
		if t5 != 0 && t15 != 0 && t5 != t15 {
			a.logger.Debug().Str("asset", asset).Msg("MTF conflict - skipping signal")
			return nil, nil
		}
	}

	if mtfTrend > 0 {
		result.Score++
	} else if mtfTrend < 0 {
		result.Score--
	}

	// ── Score to signal conversion
	// Require at least 4 factors agreeing for a signal (was 3)
	absScore := result.Score
	if absScore < 0 {
		absScore = -absScore
	}

	if absScore < a.config.SignalThreshold {
		a.logger.Debug().
			Str("asset", asset).
			Int("score", result.Score).
			Int("abs", absScore).
			Strs("reasons", result.Reasons).
			Msg("No signal (insufficient confirmation)")
		return nil, nil
	}

	// Determine direction
	if result.Score > 0 {
		result.Direction = models.DirectionCall
	} else {
		result.Direction = models.DirectionPut
	}

	// Calculate confidence based on:
	// - Number of factors agreeing (score)
	// - Volume confirmation
	// - Whether squeeze precedes the signal
	// - MTF alignment
	maxScore := 7 // total possible factors
	baseConfidence := 0.65 + (float64(absScore)/float64(maxScore))*0.25 // 65%-90% range

	if volumeConfirmed {
		baseConfidence += 0.03
	}
	if isSqueeze {
		baseConfidence += 0.02
	}
	if mtfTrend > 0 && result.Direction == models.DirectionCall {
		baseConfidence += 0.05
	} else if mtfTrend < 0 && result.Direction == models.DirectionPut {
		baseConfidence += 0.05
	}

	// Cap at 95%
	if baseConfidence > 0.95 {
		baseConfidence = 0.95
	}

	if baseConfidence < a.config.MinConfidence {
		return nil, nil
	}

	// Entry window: 2 minutes from now
	entryWindow := time.Now().Add(2 * time.Minute).Truncate(time.Minute)

	signal := &models.Signal{
		Asset:       asset,
		Direction:   result.Direction,
		Expiry:      a.config.ExpiryMinutes,
		Confidence:  baseConfidence,
		EntryWindow: entryWindow,
		Source:      "jvcbyte-analyzer",
		ReceivedAt:  time.Now(),
	}

	if a.config.EnableMartingale {
		signal.MartingaleLevels = []models.MartingaleTime{
			{Level: 1, Time: entryWindow.Add(2 * time.Minute)},
			{Level: 2, Time: entryWindow.Add(4 * time.Minute)},
			{Level: 3, Time: entryWindow.Add(6 * time.Minute)},
		}
	}

	a.logger.Info().
		Str("asset", asset).
		Str("direction", result.Direction.String()).
		Float64("confidence", baseConfidence).
		Int("score", result.Score).
		Float64("rsi", rsi).
		Float64("bb_width", bbWidth).
		Bool("volume_confirmed", volumeConfirmed).
		Bool("squeeze", isSqueeze).
		Int("mtf_trend", mtfTrend).
		Strs("reasons", result.Reasons).
		Msg("🎯 SIGNAL GENERATED")

	a.signalsMu.Lock()
	a.lastSignals[asset] = time.Now()
	a.signalsMu.Unlock()

	return signal, nil
}

// extractField is a helper to pull a single field from candle slices
func extractField(candles []wstrader.Candle, fn func(wstrader.Candle) float64) []float64 {
	out := make([]float64, len(candles))
	for i, c := range candles {
		out[i] = fn(c)
	}
	return out
}
