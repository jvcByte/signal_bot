package analyzer

import "signal-bot/internal/indicators"

// Regime represents the detected market regime
type Regime int

const (
	RegimeUnknown  Regime = 0
	RegimeTrending Regime = 1
	RegimeRanging  Regime = 2
	RegimeVolatile Regime = 3
	RegimeBreakout Regime = 4
)

// RegimeDetector is the detector type (stateless, all logic in DetectRegime)
type RegimeDetector struct{}

// DetectRegime classifies the current market regime using ADX, ATR%, and BB width.
//
// Priority order:
//  1. BB squeeze + breakout pressure → Breakout
//  2. ATR% > 0.3% → Volatile
//  3. ADX > 25 + positive EMA slope → Trending
//  4. ADX < 20 + tight BB (bbWidth < 0.5%) → Ranging
//  5. else → Unknown
func DetectRegime(highs, lows, closes []float64, adx, atrPct, bbWidth float64) Regime {
	// Need at least 10 bars to evaluate EMA slope
	if len(closes) < 10 {
		return RegimeUnknown
	}

	// EMA slope: EMA10 now vs 3 bars ago
	ema10Now := indicators.EMA(closes, 10)
	ema10Prev := indicators.EMA(closes[:len(closes)-3], 10)
	emaSlope := ema10Now - ema10Prev

	// 1. Breakout: extreme BB squeeze (< 0.08%)
	if bbWidth < 0.08 && atrPct > 0.05 {
		return RegimeBreakout
	}

	// 2. Volatile: ATR% > 0.3%
	if atrPct > 0.3 {
		return RegimeVolatile
	}

	// 3. Trending: ADX > 25 + EMA moving in a direction
	if adx > 25 && (emaSlope > 0 || emaSlope < 0) {
		return RegimeTrending
	}

	// 4. Ranging: ADX < 20 + tight BB (< 0.5%)
	if adx < 20 && bbWidth < 0.5 {
		return RegimeRanging
	}

	return RegimeUnknown
}

// String returns a human-readable regime name
func (r Regime) String() string {
	switch r {
	case RegimeTrending:
		return "Trending"
	case RegimeRanging:
		return "Ranging"
	case RegimeVolatile:
		return "Volatile"
	case RegimeBreakout:
		return "Breakout"
	default:
		return "Unknown"
	}
}

// IsTradeable returns true when the regime has enough structure to trade
func (r Regime) IsTradeable() bool {
	return r != RegimeUnknown
}
