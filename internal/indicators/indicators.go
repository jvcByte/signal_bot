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
		// Use SMA for initial value
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
		return 50.0 // neutral
	}

	gains := 0.0
	losses := 0.0

	// Calculate initial average gain/loss
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
	rsi := 100.0 - (100.0 / (1.0 + rs))

	return rsi
}

// BollingerBands calculates Bollinger Bands (middle, upper, lower)
func BollingerBands(values []float64, period int, stdDev float64) (float64, float64, float64) {
	if len(values) < period {
		return 0, 0, 0
	}

	middle := SMA(values, period)

	// Calculate standard deviation
	sum := 0.0
	for i := len(values) - period; i < len(values); i++ {
		diff := values[i] - middle
		sum += diff * diff
	}
	sd := math.Sqrt(sum / float64(period))

	upper := middle + (stdDev * sd)
	lower := middle - (stdDev * sd)

	return middle, upper, lower
}

// MACD calculates Moving Average Convergence Divergence
// Returns: (macd, signal, histogram)
func MACD(values []float64, fast, slow, signal int) (float64, float64, float64) {
	if len(values) < slow {
		return 0, 0, 0
	}

	fastEMA := EMA(values, fast)
	slowEMA := EMA(values, slow)
	macdLine := fastEMA - slowEMA

	// For signal line, we need historical MACD values
	// Simplified: using current MACD as signal for now
	// TODO: Track historical MACD values for proper signal line
	signalLine := macdLine
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

// StochasticOscillator calculates %K and %D
func StochasticOscillator(highs, lows, closes []float64, period int) (float64, float64) {
	if len(closes) < period {
		return 50, 50
	}

	// Find highest high and lowest low in period
	highestHigh := highs[len(highs)-period]
	lowestLow := lows[len(lows)-period]

	for i := len(highs) - period + 1; i < len(highs); i++ {
		if highs[i] > highestHigh {
			highestHigh = highs[i]
		}
		if lows[i] < lowestLow {
			lowestLow = lows[i]
		}
	}

	currentClose := closes[len(closes)-1]
	percentK := 100.0 * (currentClose - lowestLow) / (highestHigh - lowestLow)

	// %D is 3-period SMA of %K (simplified: using %K for now)
	percentD := percentK

	return percentK, percentD
}

// ATR calculates Average True Range (volatility indicator)
func ATR(highs, lows, closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0)

	for i := len(closes) - period; i < len(closes); i++ {
		high := highs[i]
		low := lows[i]
		prevClose := closes[i-1]

		tr := math.Max(high-low, math.Max(math.Abs(high-prevClose), math.Abs(low-prevClose)))
		trueRanges = append(trueRanges, tr)
	}

	return SMA(trueRanges, len(trueRanges))
}
