package indicators

import (
	"math"
)

// SMA calculates Simple Moving Average
func SMA(values []float64, period int) float64 {
	if len(values) < period {
		return 0
	}

	sum := 0.0
	for i := len(values) - period; i < len(values); i++ {
		sum += values[i]
	}
	return sum / float64(period)
}

// EMA calculates Exponential Moving Average
func EMA(values []float64, period int) float64 {
	if len(values) == 0 {
		return 0
	}

	if len(values) < period {
		return SMA(values, len(values))
	}

	multiplier := 2.0 / float64(period+1)
	ema := SMA(values[:period], period)

	for i := period; i < len(values); i++ {
		ema = (values[i] * multiplier) + (ema * (1 - multiplier))
	}

	return ema
}

// RSI calculates Relative Strength Index
func RSI(values []float64, period int) float64 {
	if len(values) < period+1 {
		return 50.0
	}

	gains := 0.0
	losses := 0.0

	for i := len(values) - period; i < len(values); i++ {
		change := values[i] - values[i-1]
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100.0
	}

	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

// BollingerBands calculates Bollinger Bands (middle, upper, lower)
func BollingerBands(values []float64, period int, stdDev float64) (middle, upper, lower float64) {
	if len(values) < period {
		return 0, 0, 0
	}

	middle = SMA(values, period)

	sum := 0.0
	for i := len(values) - period; i < len(values); i++ {
		diff := values[i] - middle
		sum += diff * diff
	}
	sd := math.Sqrt(sum / float64(period))

	upper = middle + (stdDev * sd)
	lower = middle - (stdDev * sd)
	return
}

// BandWidth returns Bollinger Band width as % of middle band (squeeze detection)
func BandWidth(middle, upper, lower float64) float64 {
	if middle == 0 {
		return 0
	}
	return (upper - lower) / middle * 100
}

// MACD calculates Moving Average Convergence Divergence
// Returns: (macd line, signal line, histogram)
func MACD(values []float64, fast, slow, signalPeriod int) (float64, float64, float64) {
	if len(values) < slow+signalPeriod {
		return 0, 0, 0
	}

	// Calculate MACD line history for signal line EMA
	macdHistory := make([]float64, 0)
	for i := slow; i <= len(values); i++ {
		f := EMA(values[:i], fast)
		s := EMA(values[:i], slow)
		macdHistory = append(macdHistory, f-s)
	}

	macdLine := macdHistory[len(macdHistory)-1]
	signalLine := EMA(macdHistory, signalPeriod)
	histogram := macdLine - signalLine

	return macdLine, signalLine, histogram
}

// IsBullishCrossover checks if fast MA crossed above slow MA
func IsBullishCrossover(fastPrev, slowPrev, fastCurr, slowCurr float64) bool {
	return fastPrev <= slowPrev && fastCurr > slowCurr
}

// IsBearishCrossover checks if fast MA crossed below slow MA
func IsBearishCrossover(fastPrev, slowPrev, fastCurr, slowCurr float64) bool {
	return fastPrev >= slowPrev && fastCurr < slowCurr
}

// ATR calculates Average True Range
func ATR(highs, lows, closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0, period)
	for i := len(closes) - period; i < len(closes); i++ {
		high := highs[i]
		low := lows[i]
		prevClose := closes[i-1]
		tr := math.Max(high-low, math.Max(math.Abs(high-prevClose), math.Abs(low-prevClose)))
		trueRanges = append(trueRanges, tr)
	}

	return SMA(trueRanges, len(trueRanges))
}

// AvgVolume returns the average volume over the last N candles
func AvgVolume(volumes []float64, period int) float64 {
	return SMA(volumes, period)
}

// IsBullishEngulfing checks if the last two candles form a bullish engulfing pattern
func IsBullishEngulfing(opens, closes []float64) bool {
	n := len(opens)
	if n < 2 {
		return false
	}
	prev := n - 2
	curr := n - 1
	// Previous candle is bearish, current is bullish and engulfs previous
	prevBearish := closes[prev] < opens[prev]
	currBullish := closes[curr] > opens[curr]
	engulfs := opens[curr] <= closes[prev] && closes[curr] >= opens[prev]
	return prevBearish && currBullish && engulfs
}

// IsBearishEngulfing checks if the last two candles form a bearish engulfing pattern
func IsBearishEngulfing(opens, closes []float64) bool {
	n := len(opens)
	if n < 2 {
		return false
	}
	prev := n - 2
	curr := n - 1
	prevBullish := closes[prev] > opens[prev]
	currBearish := closes[curr] < opens[curr]
	engulfs := opens[curr] >= closes[prev] && closes[curr] <= opens[prev]
	return prevBullish && currBearish && engulfs
}

// IsBullishPinBar checks for a bullish pin bar (hammer) - long lower wick, small body at top
func IsBullishPinBar(open, high, low, close float64) bool {
	body := math.Abs(close - open)
	lowerWick := math.Min(open, close) - low
	upperWick := high - math.Max(open, close)
	totalRange := high - low
	if totalRange == 0 {
		return false
	}
	// Lower wick at least 2x body, body in upper 1/3, small upper wick
	return lowerWick >= 2*body && body/totalRange < 0.35 && upperWick < body
}

// IsBearishPinBar checks for a bearish pin bar (shooting star) - long upper wick, small body at bottom
func IsBearishPinBar(open, high, low, close float64) bool {
	body := math.Abs(close - open)
	upperWick := high - math.Max(open, close)
	lowerWick := math.Min(open, close) - low
	totalRange := high - low
	if totalRange == 0 {
		return false
	}
	return upperWick >= 2*body && body/totalRange < 0.35 && lowerWick < body
}

// Trend returns 1 (bullish), -1 (bearish), 0 (neutral) based on EMA alignment
func Trend(closes []float64, fastPeriod, slowPeriod int) int {
	if len(closes) < slowPeriod {
		return 0
	}
	fast := EMA(closes, fastPeriod)
	slow := EMA(closes, slowPeriod)
	if fast > slow*1.0001 {
		return 1
	}
	if fast < slow*0.9999 {
		return -1
	}
	return 0
}
