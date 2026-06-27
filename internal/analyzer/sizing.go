package analyzer

import "math"

// SizingMethod controls how position size is calculated
type SizingMethod int

const (
	SizingFlat        SizingMethod = 0 // fixed amount
	SizingKelly       SizingMethod = 1 // Kelly criterion
	SizingFixedFrac   SizingMethod = 2 // fixed fraction of balance
	SizingVolAdjusted SizingMethod = 3 // volatility-adjusted fixed fraction
)

// SizingConfig holds position sizing parameters
type SizingConfig struct {
	Method        SizingMethod
	BaseAmount    float64 // base trade amount or fraction
	MaxAmount     float64 // hard cap per trade
	MinAmount     float64 // minimum trade amount
	KellyFraction float64 // fraction of full Kelly (0.25 = quarter Kelly for safety)
	MaxPctBalance float64 // max % of balance per trade (e.g., 0.05 = 5%)
}

// DefaultSizingConfig returns safe conservative defaults
func DefaultSizingConfig() SizingConfig {
	return SizingConfig{
		Method:        SizingVolAdjusted,
		BaseAmount:    1.0,
		MaxAmount:     50.0,
		MinAmount:     1.0,
		KellyFraction: 0.25, // quarter Kelly
		MaxPctBalance: 0.03, // max 3% of balance per trade
	}
}

// CalculateSize returns the appropriate trade size given current conditions.
//
// winRate: estimated win probability (from confidence model)
// payout:  broker payout ratio (e.g., 0.87)
// balance: current account balance
// atrPct:  current ATR as % of price (volatility measure)
func CalculateSize(cfg SizingConfig, winRate, payout, balance, atrPct float64) float64 {
	if balance <= 0 {
		return cfg.MinAmount
	}

	var size float64

	switch cfg.Method {
	case SizingFlat:
		size = cfg.BaseAmount

	case SizingKelly:
		// Kelly = (bp - q) / b  where b=payout, p=winRate, q=1-winRate
		q := 1.0 - winRate
		kelly := (payout*winRate - q) / payout
		if kelly <= 0 {
			return cfg.MinAmount // negative Kelly = don't trade
		}
		size = balance * kelly * cfg.KellyFraction

	case SizingFixedFrac:
		size = balance * cfg.BaseAmount // BaseAmount = fraction e.g. 0.02

	case SizingVolAdjusted:
		// Base: fixed fraction of balance
		baseFrac := cfg.BaseAmount / balance
		if baseFrac <= 0 || baseFrac > cfg.MaxPctBalance {
			baseFrac = cfg.MaxPctBalance
		}
		// Volatility adjustment: reduce size in high volatility, increase in moderate
		volMult := 1.0
		switch {
		case atrPct < 0.05:
			volMult = 0.5 // very flat, binary options unlikely to move enough
		case atrPct < 0.15:
			volMult = 1.0 // normal conditions
		case atrPct < 0.3:
			volMult = 1.2 // good volatility for binary options
		default:
			volMult = 0.7 // news spike, reduce exposure
		}
		size = balance * baseFrac * volMult
	}

	// Confidence scaling: higher confidence = slightly larger size (capped)
	confMult := 0.8 + winRate*0.4 // range: 0.8-1.2
	size *= confMult

	// Apply caps
	maxByBalance := balance * cfg.MaxPctBalance
	if size > maxByBalance {
		size = maxByBalance
	}
	if size > cfg.MaxAmount {
		size = cfg.MaxAmount
	}
	if size < cfg.MinAmount {
		size = cfg.MinAmount
	}

	// Round to 2 decimal places
	return math.Round(size*100) / 100
}
