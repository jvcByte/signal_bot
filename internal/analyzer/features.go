package analyzer

import (
	"math"

	"signal-bot/internal/indicators"
	"signal-bot/internal/wstrader"
)

// FeatureVector holds a rich numerical representation of the current market state.
// All values are normalised so they are comparable across instruments.
type FeatureVector struct {
	// ── Momentum
	RSI       float64
	StochK    float64
	StochD    float64
	MACDHist  float64
	MACDAccel float64 // histogram change: current - previous bar

	// ── Trend
	EMADist  float64 // (price - EMA20) / price * 100
	EMASlope float64 // EMA10[now] - EMA10[3 bars ago]
	Trend5m  int     // -1, 0, +1
	Trend15m int

	// ── Volatility
	ATRPct     float64 // ATR / price * 100
	BBWidth    float64 // band width as % of middle band
	BBPosition float64 // (price - lower) / (upper - lower) * 100

	// ── Volume
	VolumeRatio float64 // current vol / avg14

	// ── Candle structure
	BodyPct      float64 // body / total range * 100
	UpperWickPct float64
	LowerWickPct float64
	IsBullish    bool // close > open

	// ── Market structure (ADX family)
	ADX     float64
	DIPlus  float64
	DIMinus float64

	// ── Context
	Regime Regime
}

// ExtractFeatures builds a FeatureVector from raw candle data.
// It is deterministic and allocation-light; safe to call in a hot loop.
func ExtractFeatures(input ScoreInput, cfg AnalyzerConfig) FeatureVector {
	var fv FeatureVector

	closes := input.Closes
	opens  := input.Opens
	highs  := input.Highs
	lows   := input.Lows
	vols   := input.Vols

	if len(closes) < 30 {
		return fv
	}

	price := closes[len(closes)-1]
	if price == 0 {
		return fv
	}

	// ── Momentum
	fv.RSI = indicators.RSI(closes, cfg.RSIPeriod)
	fv.StochK, fv.StochD = indicators.Stochastic(highs, lows, closes, 14, 3)

	_, _, macdHistNow := indicators.MACDSeries(closes, 12, 26, 9)
	fv.MACDHist = macdHistNow

	// MACD acceleration: histogram change vs previous bar
	if len(closes) >= 2 {
		_, _, macdHistPrev := indicators.MACDSeries(closes[:len(closes)-1], 12, 26, 9)
		fv.MACDAccel = macdHistNow - macdHistPrev
	}

	// ── Trend
	ema20 := indicators.EMA(closes, 20)
	fv.EMADist = (price - ema20) / price * 100

	ema10Now := indicators.EMA(closes, 10)
	var ema10Prev float64
	if len(closes) >= 13 {
		ema10Prev = indicators.EMA(closes[:len(closes)-3], 10)
	}
	fv.EMASlope = ema10Now - ema10Prev

	if len(input.Candles5m) >= 20 {
		c5 := extractField(input.Candles5m, func(c wstrader.Candle) float64 { return c.Close })
		fv.Trend5m = indicators.Trend(c5, 8, 21)
	}
	if len(input.Candles15m) >= 20 {
		c15 := extractField(input.Candles15m, func(c wstrader.Candle) float64 { return c.Close })
		fv.Trend15m = indicators.Trend(c15, 8, 21)
	}

	// ── Volatility
	atr := indicators.ATR(highs, lows, closes, 14)
	fv.ATRPct = atr / price * 100

	bbMid, bbUpper, bbLower := indicators.BollingerBands(closes, cfg.BBPeriod, cfg.BBStdDev)
	fv.BBWidth = indicators.BandWidth(bbMid, bbUpper, bbLower)
	if bbUpper != bbLower {
		fv.BBPosition = (price - bbLower) / (bbUpper - bbLower) * 100
	} else {
		fv.BBPosition = 50
	}

	// ── Volume
	avgVol := indicators.AvgVolume(vols, 14)
	if avgVol > 0 {
		fv.VolumeRatio = vols[len(vols)-1] / avgVol
	}

	// ── Candle structure
	n := len(opens)
	if n > 0 {
		o := opens[n-1]
		h := highs[n-1]
		l := lows[n-1]
		c := closes[n-1]
		totalRange := h - l
		if totalRange > 0 {
			body := math.Abs(c - o)
			fv.BodyPct = body / totalRange * 100
			fv.UpperWickPct = (h - math.Max(o, c)) / totalRange * 100
			fv.LowerWickPct = (math.Min(o, c) - l) / totalRange * 100
		}
		fv.IsBullish = c > o
	}

	// ── Market structure
	fv.ADX, fv.DIPlus, fv.DIMinus = indicators.ADX(highs, lows, closes, 14)

	// ── Regime (uses already-computed values)
	fv.Regime = DetectRegime(highs, lows, closes, fv.ADX, fv.ATRPct, fv.BBWidth)

	return fv
}
