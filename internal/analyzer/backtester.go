package analyzer

import (
	"fmt"
	"math"
	"time"

	"github.com/rs/zerolog"
	"signal-bot/internal/wstrader"
	"signal-bot/pkg/models"
)

// RegimeStats holds win/loss counts for a specific market regime
type RegimeStats struct {
	Trades  int
	Wins    int
	WinRate float64
}

// HourStats holds win/loss counts for a specific UTC hour (0-23)
type HourStats struct {
	Trades  int
	Wins    int
	WinRate float64
}

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

	// Extended metrics
	ProfitFactor    float64 // gross profit / gross loss
	Expectancy      float64 // average P&L per trade
	AvgWin          float64
	AvgLoss         float64
	MaxConsecWins   int
	MaxConsecLosses int
	SharpeRatio     float64

	// Breakdown
	ByRegime map[Regime]RegimeStats
	ByHour   map[int]HourStats // UTC hour 0-23

	// Per score tier stats (for confidence calibration)
	ScoreTiers map[int]ScoreTierStats
}

func (r BacktestResult) String() string {
	return fmt.Sprintf(
		"Asset: %s | Trades: %d | Wins: %d | Losses: %d | WinRate: %.1f%% | Profit: $%.2f | MaxDD: $%.2f | PF: %.2f | Expect: $%.3f | Sharpe: %.2f",
		r.Asset, r.TotalTrades, r.Wins, r.Losses, r.WinRate*100,
		r.TotalProfit, r.MaxDrawdown,
		r.ProfitFactor, r.Expectancy, r.SharpeRatio,
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
		ByRegime:   make(map[Regime]RegimeStats),
		ByHour:     make(map[int]HourStats),
	}

	if len(candles) < 2 {
		return result
	}

	result.StartTime = candles[0].Time
	result.EndTime   = candles[len(candles)-1].Time

	const stake = 1.0
	const payout = 0.87

	equity    := 0.0
	peak      := 0.0
	maxDD     := 0.0
	grossWin  := 0.0
	grossLoss := 0.0
	sumWins   := 0.0
	sumLosses := 0.0

	// For Sharpe: track per-trade P&L
	plSeries := make([]float64, 0, 128)

	// Consecutive streak tracking
	streak          := 0
	maxConsecWins   := 0
	maxConsecLosses := 0

	// Per-tier win tracking
	tierWins   := make(map[int]int)
	tierTrades := make(map[int]int)

	// Per-regime win tracking
	regimeWins   := make(map[Regime]int)
	regimeTrades := make(map[Regime]int)

	// Per-hour win tracking
	hourWins   := make(map[int]int)
	hourTrades := make(map[int]int)

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

		// Extract features first (regime detection)
		fv := ExtractFeatures(input, cfg)

		// Skip non-tradeable regimes
		if !fv.Regime.IsTradeable() {
			continue
		}

		out := ComputeWeightedScore(input, cfg, &fv)

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
		entryTime  := candles[i].Time

		var win bool
		if direction == models.DirectionCall {
			win = exitPrice > entryPrice
		} else {
			win = exitPrice < entryPrice
		}

		result.TotalTrades++
		tier := int(absScore)
		tierTrades[tier]++

		regime := fv.Regime
		regimeTrades[regime]++

		hour := entryTime.UTC().Hour()
		hourTrades[hour]++

		var tradePL float64
		if win {
			result.Wins++
			tradePL = stake * payout
			equity += tradePL
			grossWin += tradePL
			sumWins  += tradePL
			tierWins[tier]++
			regimeWins[regime]++
			hourWins[hour]++
			// streak
			if streak >= 0 {
				streak++
			} else {
				streak = 1
			}
			if streak > maxConsecWins {
				maxConsecWins = streak
			}
		} else {
			result.Losses++
			tradePL = -stake
			equity += tradePL
			grossLoss += stake
			sumLosses += stake
			// streak
			if streak <= 0 {
				streak--
			} else {
				streak = -1
			}
			if -streak > maxConsecLosses {
				maxConsecLosses = -streak
			}
		}
		plSeries = append(plSeries, tradePL)

		if equity > peak {
			peak = equity
		}
		if dd := peak - equity; dd > maxDD {
			maxDD = dd
		}
	}

	// ── Aggregate scalar metrics
	if result.TotalTrades > 0 {
		result.WinRate = float64(result.Wins) / float64(result.TotalTrades)
	}
	result.TotalProfit  = equity
	result.MaxDrawdown  = maxDD
	result.MaxConsecWins   = maxConsecWins
	result.MaxConsecLosses = maxConsecLosses

	if result.Wins > 0 {
		result.AvgWin = sumWins / float64(result.Wins)
	}
	if result.Losses > 0 {
		result.AvgLoss = sumLosses / float64(result.Losses)
	}
	if grossLoss > 0 {
		result.ProfitFactor = grossWin / grossLoss
	}
	if result.TotalTrades > 0 {
		result.Expectancy = equity / float64(result.TotalTrades)
	}

	// Sharpe ratio (annualised assuming 1m candles, ~525,600 bars/year)
	result.SharpeRatio = sharpeRatio(plSeries)

	// ── Per-tier stats
	for tier, trades := range tierTrades {
		wins := tierWins[tier]
		wr := 0.0
		if trades > 0 {
			wr = float64(wins) / float64(trades)
		}
		result.ScoreTiers[tier] = ScoreTierStats{WinRate: wr, Trades: trades}
	}

	// ── Per-regime stats
	for regime, trades := range regimeTrades {
		wins := regimeWins[regime]
		wr := 0.0
		if trades > 0 {
			wr = float64(wins) / float64(trades)
		}
		result.ByRegime[regime] = RegimeStats{Trades: trades, Wins: wins, WinRate: wr}
	}

	// ── Per-hour stats
	for hour, trades := range hourTrades {
		wins := hourWins[hour]
		wr := 0.0
		if trades > 0 {
			wr = float64(wins) / float64(trades)
		}
		result.ByHour[hour] = HourStats{Trades: trades, Wins: wins, WinRate: wr}
	}

	return result
}

// sharpeRatio computes annualised Sharpe ratio from a slice of per-trade P&L values.
// Assumes each trade corresponds to roughly one 1-minute bar.
func sharpeRatio(pl []float64) float64 {
	n := len(pl)
	if n < 2 {
		return 0
	}

	mean := 0.0
	for _, v := range pl {
		mean += v
	}
	mean /= float64(n)

	variance := 0.0
	for _, v := range pl {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n - 1)
	sd := math.Sqrt(variance)

	if sd == 0 {
		return 0
	}

	// Annualise: ~525,600 1-minute bars per year
	annualFactor := math.Sqrt(525_600)
	return (mean / sd) * annualFactor
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

		fv := ExtractFeatures(input, cfg)
		if !fv.Regime.IsTradeable() {
			continue
		}

		out := ComputeWeightedScore(input, cfg, &fv)
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
