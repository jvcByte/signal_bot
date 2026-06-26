package analyzer

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"signal-bot/internal/wstrader"
	"signal-bot/pkg/models"
)

// BacktestResult holds performance metrics
type BacktestResult struct {
	Asset       string
	TotalTrades int
	Wins        int
	Losses      int
	WinRate     float64
	TotalProfit float64
	MaxDrawdown float64
	StartTime   time.Time
	EndTime     time.Time
	// Per score tier stats (for confidence calibration)
	ScoreTiers  map[int]ScoreTierStats
}

func (r BacktestResult) String() string {
	return fmt.Sprintf(
		"Asset: %s | Trades: %d | Wins: %d | Losses: %d | WinRate: %.1f%% | Profit: $%.2f | MaxDD: $%.2f",
		r.Asset, r.TotalTrades, r.Wins, r.Losses, r.WinRate*100, r.TotalProfit, r.MaxDrawdown,
	)
}

// BacktestAsset runs the strategy on historical data using ComputeWeightedScore.
// This shares the exact same scoring logic as live trading.
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
		Asset:      asset,
		ScoreTiers: make(map[int]ScoreTierStats),
	}

	if len(candles) < 2 {
		return result
	}

	result.StartTime = candles[0].Time
	result.EndTime   = candles[len(candles)-1].Time

	const stake = 1.0
	const payout = 0.87

	equity := 0.0
	peak   := 0.0
	maxDD  := 0.0

	minCandles := cfg.SlowMAPeriod + cfg.BBPeriod + 30
	if len(candles) < minCandles+expiryMinutes {
		logger.Warn().Str("asset", asset).
			Int("have", len(candles)).Int("need", minCandles).
			Msg("Not enough candles for backtest")
		return result
	}

	for i := minCandles; i < len(candles)-expiryMinutes; i++ {
		window := candles[:i]

		input := ScoreInput{
			Closes:     extractField(window, func(c wstrader.Candle) float64 { return c.Close }),
			Opens:      extractField(window, func(c wstrader.Candle) float64 { return c.Open }),
			Highs:      extractField(window, func(c wstrader.Candle) float64 { return c.High }),
			Lows:       extractField(window, func(c wstrader.Candle) float64 { return c.Low }),
			Vols:       extractField(window, func(c wstrader.Candle) float64 { return c.Volume }),
			Candles5m:  candles5m,
			Candles15m: candles15m,
		}

		out := ComputeWeightedScore(input, cfg)

		absScore := out.Score
		if absScore < 0 {
			absScore = -absScore
		}
		if absScore < cfg.SignalThreshold {
			continue
		}

		var direction models.Direction
		if out.Score > 0 {
			direction = models.DirectionCall
		} else {
			direction = models.DirectionPut
		}

		entryPrice := input.Closes[len(input.Closes)-1]
		exitPrice  := candles[i+expiryMinutes].Close

		var win bool
		if direction == models.DirectionCall {
			win = exitPrice > entryPrice
		} else {
			win = exitPrice < entryPrice
		}

		result.TotalTrades++
		tier := int(absScore)
		ts := result.ScoreTiers[tier]
		ts.Trades++
		if win {
			result.Wins++
			equity += stake * payout
			ts.WinRate = float64(ts.Trades-1)/float64(ts.Trades)*ts.WinRate + payout/float64(ts.Trades)
		} else {
			result.Losses++
			equity -= stake
		}
		result.ScoreTiers[tier] = ts

		if equity > peak {
			peak = equity
		}
		if dd := peak - equity; dd > maxDD {
			maxDD = dd
		}
	}

	// Compute final win rates per tier
	for tier, ts := range result.ScoreTiers {
		wins := 0
		// Recount (simplified - approximation since we didn't store per-tier wins separately)
		// For exact per-tier win rates, the caller should use CalibrateFromBacktest
		_ = wins
		_ = tier
		_ = ts
	}

	if result.TotalTrades > 0 {
		result.WinRate = float64(result.Wins) / float64(result.TotalTrades)
	}
	result.TotalProfit = equity
	result.MaxDrawdown = maxDD

	return result
}

// CalibrateFromBacktest runs a detailed backtest and returns ScoreTierStats per score tier.
// Use this to populate AnalyzerConfig.ScoreTierMap for calibrated confidence.
func CalibrateFromBacktest(
	candles []wstrader.Candle,
	candles5m []wstrader.Candle,
	candles15m []wstrader.Candle,
	cfg AnalyzerConfig,
	expiryMinutes int,
) map[int]ScoreTierStats {
	tiers := make(map[int]struct {
		wins   int
		losses int
	})

	minCandles := cfg.SlowMAPeriod + cfg.BBPeriod + 30
	if len(candles) < minCandles+expiryMinutes {
		return nil
	}

	for i := minCandles; i < len(candles)-expiryMinutes; i++ {
		window := candles[:i]
		input := ScoreInput{
			Closes:     extractField(window, func(c wstrader.Candle) float64 { return c.Close }),
			Opens:      extractField(window, func(c wstrader.Candle) float64 { return c.Open }),
			Highs:      extractField(window, func(c wstrader.Candle) float64 { return c.High }),
			Lows:       extractField(window, func(c wstrader.Candle) float64 { return c.Low }),
			Vols:       extractField(window, func(c wstrader.Candle) float64 { return c.Volume }),
			Candles5m:  candles5m,
			Candles15m: candles15m,
		}

		out := ComputeWeightedScore(input, cfg)
		absScore := out.Score
		if absScore < 0 {
			absScore = -absScore
		}
		if absScore < cfg.SignalThreshold {
			continue
		}

		var direction models.Direction
		if out.Score > 0 {
			direction = models.DirectionCall
		} else {
			direction = models.DirectionPut
		}

		entryPrice := input.Closes[len(input.Closes)-1]
		exitPrice  := candles[i+expiryMinutes].Close

		var win bool
		if direction == models.DirectionCall {
			win = exitPrice > entryPrice
		} else {
			win = exitPrice < entryPrice
		}

		tier := int(absScore)
		t := tiers[tier]
		if win {
			t.wins++
		} else {
			t.losses++
		}
		tiers[tier] = t
	}

	result := make(map[int]ScoreTierStats)
	for tier, t := range tiers {
		total := t.wins + t.losses
		if total > 0 {
			result[tier] = ScoreTierStats{
				WinRate: float64(t.wins) / float64(total),
				Trades:  total,
			}
		}
	}
	return result
}
