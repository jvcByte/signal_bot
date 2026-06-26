package analyzer

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"signal-bot/internal/indicators"
	"signal-bot/internal/wstrader"
	"signal-bot/pkg/models"
)

// BacktestResult holds the performance metrics of a backtest
type BacktestResult struct {
	Asset       string
	TotalTrades int
	Wins        int
	Losses      int
	WinRate     float64
	TotalProfit float64 // assuming 87% payout, $1 stake
	MaxDrawdown float64
	StartTime   time.Time
	EndTime     time.Time
}

func (r BacktestResult) String() string {
	return fmt.Sprintf(
		"Asset: %s | Trades: %d | Wins: %d | Losses: %d | WinRate: %.1f%% | Profit: $%.2f | MaxDrawdown: $%.2f",
		r.Asset, r.TotalTrades, r.Wins, r.Losses, r.WinRate*100, r.TotalProfit, r.MaxDrawdown,
	)
}

// BacktestAsset runs the analysis strategy on historical data and returns performance
func BacktestAsset(
	asset string,
	candles []wstrader.Candle,
	candles5m []wstrader.Candle,
	candles15m []wstrader.Candle,
	cfg AnalyzerConfig,
	expiryMinutes int,
	logger zerolog.Logger,
) BacktestResult {
	result := BacktestResult{
		Asset:     asset,
		StartTime: candles[0].Time,
		EndTime:   candles[len(candles)-1].Time,
	}

	const stake = 1.0
	const payout = 0.87

	equity := 0.0
	peak := 0.0
	maxDD := 0.0

	minCandles := cfg.SlowMAPeriod + cfg.BBPeriod + 5
	if len(candles) < minCandles+expiryMinutes {
		logger.Warn().Str("asset", asset).Int("have", len(candles)).Int("need", minCandles).Msg("Not enough candles for backtest")
		return result
	}

	for i := minCandles; i < len(candles)-expiryMinutes; i++ {
		window := candles[:i]

		closes := extractField(window, func(c wstrader.Candle) float64 { return c.Close })
		opens  := extractField(window, func(c wstrader.Candle) float64 { return c.Open })
		highs  := extractField(window, func(c wstrader.Candle) float64 { return c.High })
		lows   := extractField(window, func(c wstrader.Candle) float64 { return c.Low })
		vols   := extractField(window, func(c wstrader.Candle) float64 { return c.Volume })

		score, _ := computeScore(closes, opens, highs, lows, vols, candles5m, candles15m, cfg)

		absScore := score
		if absScore < 0 {
			absScore = -absScore
		}
		if absScore < 5 {
			continue
		}

		var direction models.Direction
		if score > 0 {
			direction = models.DirectionCall
		} else {
			direction = models.DirectionPut
		}

		entryPrice := closes[len(closes)-1]
		exitPrice := candles[i+expiryMinutes].Close

		var win bool
		if direction == models.DirectionCall {
			win = exitPrice > entryPrice
		} else {
			win = exitPrice < entryPrice
		}

		result.TotalTrades++
		if win {
			result.Wins++
			equity += stake * payout
		} else {
			result.Losses++
			equity -= stake
		}

		if equity > peak {
			peak = equity
		}
		if dd := peak - equity; dd > maxDD {
			maxDD = dd
		}
	}

	if result.TotalTrades > 0 {
		result.WinRate = float64(result.Wins) / float64(result.TotalTrades)
	}
	result.TotalProfit = equity
	result.MaxDrawdown = maxDD

	return result
}

// computeScore runs all indicators and returns a directional vote score.
// Positive = bullish, negative = bearish. Magnitude = conviction.
// Used by both live analyzer and backtester.
func computeScore(
	closes, opens, highs, lows, vols []float64,
	candles5m []wstrader.Candle,
	candles15m []wstrader.Candle,
	cfg AnalyzerConfig,
) (score int, reasons []string) {

	// ── RSI
	rsi := indicators.RSI(closes, cfg.RSIPeriod)
	if rsi < cfg.RSIOversold {
		score++
		reasons = append(reasons, fmt.Sprintf("RSI oversold (%.1f)", rsi))
	} else if rsi > cfg.RSIOverbought {
		score--
		reasons = append(reasons, fmt.Sprintf("RSI overbought (%.1f)", rsi))
	}

	// ── EMA trend + crossover
	fastMA     := indicators.EMA(closes, cfg.FastMAPeriod)
	slowMA     := indicators.EMA(closes, cfg.SlowMAPeriod)
	fastMAPrev := indicators.EMA(closes[:len(closes)-1], cfg.FastMAPeriod)
	slowMAPrev := indicators.EMA(closes[:len(closes)-1], cfg.SlowMAPeriod)

	// ── EMA crossover only (not plain alignment - too noisy in ranging markets)
	if indicators.IsBullishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		score++
		reasons = append(reasons, "EMA bullish crossover")
	} else if indicators.IsBearishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		score--
		reasons = append(reasons, "EMA bearish crossover")
	}
	// EMA alignment only when RSI agrees (avoid false signals in ranging markets)
	if fastMA > slowMA && rsi < 50 {
		score++
	} else if fastMA < slowMA && rsi > 50 {
		score--
	}

	// ── Bollinger Bands
	bbMid, bbUpper, bbLower := indicators.BollingerBands(closes, cfg.BBPeriod, cfg.BBStdDev)
	_ = bbMid
	currentPrice := closes[len(closes)-1]
	if currentPrice <= bbLower {
		score++
		reasons = append(reasons, "Price at lower BB")
	} else if currentPrice >= bbUpper {
		score--
		reasons = append(reasons, "Price at upper BB")
	}

	// ── MACD
	_, macdSignal, macdHist := indicators.MACD(closes, 12, 26, 9)
	if macdHist > 0 && macdSignal >= 0 {
		score++
	} else if macdHist < 0 && macdSignal <= 0 {
		score--
	}

	// ── Volume
	avgVol  := indicators.AvgVolume(vols, cfg.VolumePeriod)
	lastVol := vols[len(vols)-1]
	if avgVol > 0 && lastVol >= avgVol*cfg.VolumeMultiplier {
		reasons = append(reasons, "Volume surge")
	}

	// ── Candlestick patterns
	if indicators.IsBullishEngulfing(opens, closes) {
		score++
		reasons = append(reasons, "Bullish engulfing")
	} else if indicators.IsBearishEngulfing(opens, closes) {
		score--
		reasons = append(reasons, "Bearish engulfing")
	}
	n := len(opens)
	if n > 0 {
		if indicators.IsBullishPinBar(opens[n-1], highs[n-1], lows[n-1], closes[n-1]) {
			score++
			reasons = append(reasons, "Bullish pin bar")
		} else if indicators.IsBearishPinBar(opens[n-1], highs[n-1], lows[n-1], closes[n-1]) {
			score--
			reasons = append(reasons, "Bearish pin bar")
		}
	}

	// ── MTF 5m
	t5 := 0
	if len(candles5m) >= 20 {
		c5 := extractField(candles5m, func(c wstrader.Candle) float64 { return c.Close })
		t5 = indicators.Trend(c5, 8, 21)
		if t5 > 0 {
			score++
			reasons = append(reasons, "5m trend: BULLISH")
		} else if t5 < 0 {
			score--
			reasons = append(reasons, "5m trend: BEARISH")
		}
	}

	// ── MTF 15m
	t15 := 0
	if len(candles15m) >= 20 {
		c15 := extractField(candles15m, func(c wstrader.Candle) float64 { return c.Close })
		t15 = indicators.Trend(c15, 8, 21)
		if t15 > 0 {
			score++
			reasons = append(reasons, "15m trend: BULLISH")
		} else if t15 < 0 {
			score--
			reasons = append(reasons, "15m trend: BEARISH")
		}
	}

	// ── MTF conflict: 5m and 15m disagree → no trade
	if t5 != 0 && t15 != 0 && t5 != t15 {
		score = 0
	}

	return score, reasons
}
