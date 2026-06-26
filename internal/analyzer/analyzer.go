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

// SignalAnalyzer generates trading signals from candle data
type SignalAnalyzer struct {
	trader       *wstrader.Trader
	logger       zerolog.Logger
	config       AnalyzerConfig
	lastSignals  map[string]time.Time // Track last signal time per asset
	signalsMu    sync.RWMutex
}

// AnalyzerConfig contains strategy parameters
type AnalyzerConfig struct {
	RSIPeriod        int     // RSI calculation period (default: 14)
	RSIOversold      float64 // RSI oversold threshold (default: 30)
	RSIOverbought    float64 // RSI overbought threshold (default: 70)
	FastMAPeriod     int     // Fast moving average period (default: 10)
	SlowMAPeriod     int     // Slow moving average period (default: 20)
	MinConfidence    float64 // Minimum confidence to generate signal (default: 0.65)
	ExpiryMinutes    int     // Trade expiry in minutes (default: 2)
	EnableMartingale bool    // Enable martingale levels (default: true)
	SignalCooldown   int     // Cooldown between signals for same asset in minutes (default: 7)
}

// DefaultConfig returns default analyzer configuration
func DefaultConfig() AnalyzerConfig {
	return AnalyzerConfig{
		RSIPeriod:        14,
		RSIOversold:      30,
		RSIOverbought:    70,
		FastMAPeriod:     10,
		SlowMAPeriod:     20,
		MinConfidence:    0.65,
		ExpiryMinutes:    2,
		EnableMartingale: true,
		SignalCooldown:   7, // 7 minutes between signals for same asset
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

// AnalyzeAsset analyzes an asset and generates a signal if conditions are met
func (a *SignalAnalyzer) AnalyzeAsset(asset string) (*models.Signal, error) {
	// Check cooldown - don't generate signal if one was sent recently for this asset
	a.signalsMu.RLock()
	lastSignalTime, exists := a.lastSignals[asset]
	a.signalsMu.RUnlock()

	if exists {
		cooldownDuration := time.Duration(a.config.SignalCooldown) * time.Minute
		timeSinceLastSignal := time.Since(lastSignalTime)
		if timeSinceLastSignal < cooldownDuration {
			a.logger.Debug().
				Str("asset", asset).
				Dur("time_since_last", timeSinceLastSignal).
				Dur("cooldown", cooldownDuration).
				Msg("Signal suppressed (cooldown active)")
			return nil, nil // No signal during cooldown
		}
	}

	// Fetch recent candles (60 seconds = 1 minute candles)
	candles, err := a.trader.GetHistoricalCandles(asset, 60, 50)
	if err != nil {
		return nil, fmt.Errorf("failed to get candles: %w", err)
	}

	if len(candles) < a.config.SlowMAPeriod+1 {
		return nil, fmt.Errorf("insufficient candles: need %d, got %d", a.config.SlowMAPeriod+1, len(candles))
	}

	// Extract price arrays
	closes := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))

	for i, c := range candles {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
	}

	// Calculate indicators
	rsi := indicators.RSI(closes, a.config.RSIPeriod)
	fastMA := indicators.SMA(closes, a.config.FastMAPeriod)
	slowMA := indicators.SMA(closes, a.config.SlowMAPeriod)

	// Previous MAs for crossover detection
	closesPrev := closes[:len(closes)-1]
	fastMAPrev := indicators.SMA(closesPrev, a.config.FastMAPeriod)
	slowMAPrev := indicators.SMA(closesPrev, a.config.SlowMAPeriod)

	currentPrice := closes[len(closes)-1]

	a.logger.Debug().
		Str("asset", asset).
		Float64("price", currentPrice).
		Float64("rsi", rsi).
		Float64("fast_ma", fastMA).
		Float64("slow_ma", slowMA).
		Msg("Technical indicators")

	// Determine signal direction and confidence
	var direction models.Direction
	var confidence float64
	var reason string

	// Strategy 1: RSI Oversold/Overbought
	if rsi < a.config.RSIOversold {
		direction = models.DirectionCall
		confidence = 0.7 + (a.config.RSIOversold-rsi)/100.0 // More oversold = higher confidence
		reason = fmt.Sprintf("RSI oversold (%.1f)", rsi)
	} else if rsi > a.config.RSIOverbought {
		direction = models.DirectionPut
		confidence = 0.7 + (rsi-a.config.RSIOverbought)/100.0
		reason = fmt.Sprintf("RSI overbought (%.1f)", rsi)
	}

	// Strategy 2: Moving Average Crossover (higher confidence)
	if indicators.IsBullishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		direction = models.DirectionCall
		confidence = 0.75
		reason = "Bullish MA crossover"
	} else if indicators.IsBearishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		direction = models.DirectionPut
		confidence = 0.75
		reason = "Bearish MA crossover"
	}

	// Strategy 3: Combined signals (highest confidence)
	if rsi < a.config.RSIOversold && fastMA > slowMA {
		direction = models.DirectionCall
		confidence = 0.85
		reason = "RSI oversold + bullish trend"
	} else if rsi > a.config.RSIOverbought && fastMA < slowMA {
		direction = models.DirectionPut
		confidence = 0.85
		reason = "RSI overbought + bearish trend"
	}

	// No signal if confidence too low
	if confidence < a.config.MinConfidence {
		return nil, nil // No error, just no signal
	}

	// Calculate entry window (current time + 1 minute)
	entryWindow := time.Now().Add(1 * time.Minute)
	entryWindow = entryWindow.Truncate(time.Minute) // Round to nearest minute

	// Generate signal
	signal := &models.Signal{
		Asset:      asset,
		Direction:  direction,
		Expiry:     a.config.ExpiryMinutes,
		Confidence: confidence,
		EntryWindow: entryWindow,
		Source:     "ai-analyzer",
		ReceivedAt: time.Now(),
	}

	// Add martingale levels if enabled
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
		Str("reason", reason).
		Time("entry", entryWindow).
		Msg("🎯 SIGNAL GENERATED")

	// Update last signal time for this asset
	a.signalsMu.Lock()
	a.lastSignals[asset] = time.Now()
	a.signalsMu.Unlock()

	return signal, nil
}
