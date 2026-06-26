package analyzer

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
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
	SignalCooldown   int     // minutes
	SignalThreshold  float64 // min weighted score to generate signal
	// Bollinger Bands
	BBPeriod  int
	BBStdDev  float64
	// Volume
	VolumePeriod     int
	VolumeMultiplier float64
	// Multi-timeframe
	EnableMTF bool
	// Score calibration from backtest (optional)
	ScoreTierMap map[int]ScoreTierStats
}

// ScoreTierStats holds historical performance for a given score tier
type ScoreTierStats struct {
	WinRate float64
	Trades  int
}

// DefaultConfig returns sensible defaults
func DefaultConfig() AnalyzerConfig {
	return AnalyzerConfig{
		RSIPeriod:        14,
		RSIOversold:      30,
		RSIOverbought:    70,
		FastMAPeriod:     10,
		SlowMAPeriod:     20,
		MinConfidence:    0.60,
		ExpiryMinutes:    2,
		EnableMartingale: true,
		SignalCooldown:   7,
		SignalThreshold:  3.5, // weighted score threshold
		BBPeriod:         20,
		BBStdDev:         2.0,
		VolumePeriod:     14,
		VolumeMultiplier: 1.2,
		EnableMTF:        true,
		ScoreTierMap:     make(map[int]ScoreTierStats),
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

// AnalyzeAsset runs multi-factor weighted analysis on an asset
func (a *SignalAnalyzer) AnalyzeAsset(asset string) (*models.Signal, error) {
	// Cooldown check
	a.signalsMu.RLock()
	lastSignalTime, exists := a.lastSignals[asset]
	a.signalsMu.RUnlock()
	if exists && time.Since(lastSignalTime) < time.Duration(a.config.SignalCooldown)*time.Minute {
		return nil, nil
	}

	// Fetch 1m candles
	candles1m, err := a.trader.GetHistoricalCandles(asset, 60, 100)
	if err != nil || len(candles1m) < 30 {
		return nil, fmt.Errorf("insufficient 1m candles for %s: %w", asset, err)
	}

	// Fetch 5m and 15m for MTF
	var candles5m, candles15m []wstrader.Candle
	if a.config.EnableMTF {
		candles5m, _  = a.trader.GetHistoricalCandles(asset, 300, 30)
		candles15m, _ = a.trader.GetHistoricalCandles(asset, 900, 20)
	}

	input := ScoreInput{
		Closes:     extractField(candles1m, func(c wstrader.Candle) float64 { return c.Close }),
		Opens:      extractField(candles1m, func(c wstrader.Candle) float64 { return c.Open }),
		Highs:      extractField(candles1m, func(c wstrader.Candle) float64 { return c.High }),
		Lows:       extractField(candles1m, func(c wstrader.Candle) float64 { return c.Low }),
		Vols:       extractField(candles1m, func(c wstrader.Candle) float64 { return c.Volume }),
		Candles5m:  candles5m,
		Candles15m: candles15m,
	}

	result := ComputeWeightedScore(input, a.config)

	absScore := result.Score
	if absScore < 0 {
		absScore = -absScore
	}

	a.logger.Info().
		Str("asset", asset).
		Float64("score", result.Score).
		Float64("abs_score", absScore).
		Float64("threshold", a.config.SignalThreshold).
		Float64("rsi", result.Meta.RSI).
		Float64("adx", result.Meta.ADX).
		Float64("stoch_k", result.Meta.StochK).
		Strs("factors", result.Reasons).
		Msg("  ↳ Analysis")

	if absScore < a.config.SignalThreshold {
		return nil, nil
	}

	// Direction
	var direction models.Direction
	if result.Score > 0 {
		direction = models.DirectionCall
	} else {
		direction = models.DirectionPut
	}

	// Confidence: use calibrated win rate if available, else formula
	scoreTier := int(absScore)
	confidence := 0.0
	if stats, ok := a.config.ScoreTierMap[scoreTier]; ok && stats.Trades >= 30 {
		confidence = stats.WinRate
		a.logger.Debug().
			Int("tier", scoreTier).
			Float64("calibrated_wr", stats.WinRate).
			Int("sample_size", stats.Trades).
			Msg("Using calibrated confidence")
	} else {
		// Formula fallback: 60%-90% range based on score
		maxScore := weightRSIExtreme + weightStoch + weightEMACross + weightADX + weightBB + weightMACD + weightEngulfing + weightPinBar + weightMTF5m + weightMTF15m
		confidence = 0.60 + (absScore/maxScore)*0.30
		if confidence > 0.95 {
			confidence = 0.95
		}
	}

	if confidence < a.config.MinConfidence {
		return nil, nil
	}

	entryWindow := time.Now().Add(2 * time.Minute).Truncate(time.Minute)

	signal := &models.Signal{
		Asset:       asset,
		Direction:   direction,
		Expiry:      a.config.ExpiryMinutes,
		Confidence:  confidence,
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
		Str("direction", direction.String()).
		Float64("confidence", confidence).
		Float64("score", result.Score).
		Float64("adx", result.Meta.ADX).
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
