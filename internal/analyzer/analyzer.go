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
	confidence  *ConfidenceModel
}

// AnalyzerConfig contains strategy parameters
type AnalyzerConfig struct {
	RSIPeriod        int
	RSIOversold      float64
	RSIOverbought    float64
	FastMAPeriod     int
	SlowMAPeriod     int
	MinConfidence    float64
	ExpiryMinutes    int  // expiry in seconds (renamed but keeping field name for compat)
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
	// Position sizing
	Sizing SizingConfig
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
		ExpiryMinutes:    30,  // 30 seconds default
		EnableMartingale: true,
		SignalCooldown:   7,
		SignalThreshold:  3.5,
		BBPeriod:         20,
		BBStdDev:         2.0,
		VolumePeriod:     14,
		VolumeMultiplier: 1.2,
		EnableMTF:        true,
		ScoreTierMap:     make(map[int]ScoreTierStats),
		Sizing:           DefaultSizingConfig(),
	}
}

// New creates a new signal analyzer
func New(trader *wstrader.Trader, logger zerolog.Logger, config AnalyzerConfig) *SignalAnalyzer {
	return &SignalAnalyzer{
		trader:      trader,
		logger:      logger,
		config:      config,
		lastSignals: make(map[string]time.Time),
		confidence:  NewConfidenceModel(),
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

	// ── Step 1: Extract feature vector (includes regime detection)
	fv := ExtractFeatures(input, a.config)

	// ── Step 2: Run filter chain (replaces manual regime check)
	chain := NewFilterChain(
		&RegimeFilter{},
		&LowVolatilityFilter{MinATRPct: 0.03},
		&HighVolatilityFilter{MaxATRPct: 0.8},
		&ADXFilter{MinADX: 15},
	)
	if pass, reason := chain.Apply(fv, a.config); !pass {
		a.logger.Debug().
			Str("asset", asset).
			Str("reason", reason).
			Msg("⏭  Signal filtered")
		return nil, nil
	}

	// ── Step 3: Compute score with regime-aware weights
	result := ComputeWeightedScore(input, a.config, &fv)

	absScore := result.Score
	if absScore < 0 {
		absScore = -absScore
	}

	// ── Step 4: Dynamic threshold based on volatility
	threshold := DynamicThreshold(a.config.SignalThreshold, fv.ATRPct)

	a.logger.Info().
		Str("asset", asset).
		Str("regime", fv.Regime.String()).
		Float64("score", result.Score).
		Float64("abs_score", absScore).
		Float64("threshold", threshold).
		Float64("rsi", result.Meta.RSI).
		Float64("adx", result.Meta.ADX).
		Float64("stoch_k", result.Meta.StochK).
		Float64("ema_dist", fv.EMADist).
		Float64("atr_pct", fv.ATRPct).
		Float64("bb_width", fv.BBWidth).
		Strs("factors", result.Reasons).
		Msg("  ↳ Analysis")

	if absScore < threshold {
		return nil, nil
	}

	// Direction
	var direction models.Direction
	if result.Score > 0 {
		direction = models.DirectionCall
	} else {
		direction = models.DirectionPut
	}

	// ── Step 5: Estimate confidence
	confidence := a.confidence.Estimate(result.Score, fv.Regime, fv)
	calibrated := a.confidence.IsCalibrated(result.Score, fv.Regime)

	// If not calibrated, fall back to backtest ScoreTierMap
	if !calibrated {
		scoreTier := int(absScore)
		if stats, ok := a.config.ScoreTierMap[scoreTier]; ok && stats.Trades >= 20 {
			confidence = stats.WinRate
			calibrated = true
			a.logger.Debug().
				Int("tier", scoreTier).
				Float64("backtest_wr", stats.WinRate).
				Msg("Using backtest-calibrated confidence")
		}
	}

	// Still no data - use a conservative estimate and flag it
	if !calibrated {
		// Conservative formula: score / theoretical max, floored at 60%
		maxScore := 15.5 // approximate max weighted score
		confidence = 0.60 + (absScore/maxScore)*0.10 // 60-70% range only
		a.logger.Debug().
			Str("asset", asset).
			Float64("confidence", confidence).
			Msg("⚠️  Confidence is UNVALIDATED (run backtest to calibrate)")
	}

	// Enforce minimum
	if confidence < a.config.MinConfidence {
		return nil, nil
	}

	entryWindow := time.Now().Add(2 * time.Minute).Truncate(time.Minute)

	// ── Step 6: Volatility-adjusted position sizing
	tradeAmount := CalculateSize(a.config.Sizing, confidence, 0.87, 100.0, fv.ATRPct)

	signal := &models.Signal{
		Asset:       asset,
		Direction:   direction,
		Expiry:      a.config.ExpiryMinutes,
		Confidence:  confidence,
		Amount:      tradeAmount,
		EntryWindow: entryWindow,
		Source:      "jvcbyte-analyzer",
		ReceivedAt:  time.Now(),
	}

	if a.config.EnableMartingale {
		// Space martingale levels by one expiry duration each
		expDur := time.Duration(a.config.ExpiryMinutes) * time.Second
		signal.MartingaleLevels = []models.MartingaleTime{
			{Level: 1, Time: entryWindow.Add(expDur)},
			{Level: 2, Time: entryWindow.Add(expDur * 2)},
			{Level: 3, Time: entryWindow.Add(expDur * 3)},
			{Level: 4, Time: entryWindow.Add(expDur * 4)},
			{Level: 5, Time: entryWindow.Add(expDur * 5)},
			{Level: 6, Time: entryWindow.Add(expDur * 6)},
			{Level: 7, Time: entryWindow.Add(expDur * 7)},
			{Level: 8, Time: entryWindow.Add(expDur * 8)},
			{Level: 9, Time: entryWindow.Add(expDur * 9)},
			{Level: 10, Time: entryWindow.Add(expDur * 10)},
		}
	}

	a.logger.Info().
		Str("asset", asset).
		Str("regime", fv.Regime.String()).
		Str("direction", direction.String()).
		Float64("confidence", confidence).
		Bool("calibrated", calibrated).
		Float64("score", result.Score).
		Float64("adx", result.Meta.ADX).
		Float64("ema_dist", fv.EMADist).
		Float64("amount", tradeAmount).
		Strs("reasons", result.Reasons).
		Msg("🎯 SIGNAL GENERATED")

	a.signalsMu.Lock()
	a.lastSignals[asset] = time.Now()
	a.signalsMu.Unlock()

	return signal, nil
}

// RecordOutcome updates the online confidence model with the result of a trade.
// Call this after each trade settles.
func (a *SignalAnalyzer) RecordOutcome(score float64, regime Regime, won bool) {
	a.confidence.Update(score, regime, won)
}

// extractField is a helper to pull a single field from candle slices
func extractField(candles []wstrader.Candle, fn func(wstrader.Candle) float64) []float64 {
	out := make([]float64, len(candles))
	for i, c := range candles {
		out[i] = fn(c)
	}
	return out
}

// LoadConfidenceModel loads a previously calibrated confidence model from disk.
func (a *SignalAnalyzer) LoadConfidenceModel(path string) error {
	return a.confidence.LoadFromFile(path)
}

// SaveConfidenceModel persists the current confidence model to disk.
func (a *SignalAnalyzer) SaveConfidenceModel(path string) error {
	return a.confidence.SaveToFile(path)
}
