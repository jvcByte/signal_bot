package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/rs/zerolog"
	"signal-bot/internal/analyzer"
	"signal-bot/internal/config"
	"signal-bot/internal/wstrader"
)

func main() {
	assetsFlag := flag.String("assets", "EURUSD,GBPUSD,AUDUSD,USDJPY,OPENAI", "Comma-separated assets to backtest")
	candles    := flag.Int("candles", 500, "Number of 1m candles to use (max ~500)")
	configPath := flag.String("config", "configs/config.yaml", "Config file path")
	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"}).
		With().Timestamp().Logger().Level(zerolog.InfoLevel)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	trader := wstrader.New(&cfg.IQOption, logger)
	logger.Info().Msg("Connecting to IQ Option...")
	if err := trader.Connect(); err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer trader.Close()

	analyzerCfg := analyzer.DefaultConfig()
	assets := strings.Split(*assetsFlag, ",")

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  BACKTEST RESULTS                         ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Printf("  Period: last %d 1-minute candles per asset\n", *candles)
	fmt.Printf("  Strategy: RSI + EMA + Bollinger + MACD + Volume + Patterns + MTF + Regime\n\n")

	totalTrades := 0
	totalWins   := 0

	for _, asset := range assets {
		asset = strings.TrimSpace(strings.ToUpper(asset))

		// Fetch 1m candles
		c1m, err := trader.GetHistoricalCandles(asset, 60, *candles)
		if err != nil || len(c1m) < 50 {
			logger.Warn().Str("asset", asset).Msg("Could not fetch candles, skipping")
			continue
		}

		// Fetch 5m and 15m candles for MTF
		c5m, _  := trader.GetHistoricalCandles(asset, 300, 100)
		c15m, _ := trader.GetHistoricalCandles(asset, 900, 50)

		result := analyzer.BacktestAsset(asset, c1m, c5m, c15m, analyzerCfg, 2, logger)

		if result.TotalTrades == 0 {
			fmt.Printf("  %-12s  No signals generated\n\n", asset)
			continue
		}

		winEmoji := "✅"
		if result.WinRate < 0.55 {
			winEmoji = "❌"
		} else if result.WinRate < 0.65 {
			winEmoji = "⚠️ "
		}

		// ── Main line
		fmt.Printf("  %s %-12s  Trades: %3d  WinRate: %5.1f%%  P&L: $%+.2f  MaxDD: $%.2f\n",
			winEmoji, asset,
			result.TotalTrades,
			result.WinRate*100,
			result.TotalProfit,
			result.MaxDrawdown,
		)

		// ── Extended metrics line
		fmt.Printf("             ProfitFactor: %.2f  Expectancy: $%+.3f  Sharpe: %.2f  AvgW: $%.3f  AvgL: $%.3f  MaxCW: %d  MaxCL: %d\n",
			result.ProfitFactor,
			result.Expectancy,
			result.SharpeRatio,
			result.AvgWin,
			result.AvgLoss,
			result.MaxConsecWins,
			result.MaxConsecLosses,
		)

		// ── Regime breakdown
		if len(result.ByRegime) > 0 {
			regimeParts := make([]string, 0, len(result.ByRegime))
			// Sort by regime value for stable output
			regimes := make([]int, 0, len(result.ByRegime))
			for r := range result.ByRegime {
				regimes = append(regimes, int(r))
			}
			sort.Ints(regimes)
			for _, ri := range regimes {
				r := analyzer.Regime(ri)
				rs := result.ByRegime[r]
				if rs.Trades > 0 {
					regimeParts = append(regimeParts, fmt.Sprintf("%s:%.0f%%(%d)", r.String(), rs.WinRate*100, rs.Trades))
				}
			}
			fmt.Printf("             By Regime: %s\n", strings.Join(regimeParts, "  "))
		}

		// ── Best hours (top 3 by win rate, min 3 trades)
		type hourEntry struct {
			hour int
			hs   analyzer.HourStats
		}
		var hourList []hourEntry
		for h, hs := range result.ByHour {
			if hs.Trades >= 3 {
				hourList = append(hourList, hourEntry{h, hs})
			}
		}
		sort.Slice(hourList, func(i, j int) bool {
			return hourList[i].hs.WinRate > hourList[j].hs.WinRate
		})
		if len(hourList) > 0 {
			top := hourList
			if len(top) > 3 {
				top = top[:3]
			}
			hourParts := make([]string, 0, len(top))
			for _, he := range top {
				hourParts = append(hourParts, fmt.Sprintf("%02d:00(%.0f%%)", he.hour, he.hs.WinRate*100))
			}
			fmt.Printf("             Best Hours: %s\n", strings.Join(hourParts, "  "))
		}

		fmt.Println()

		totalTrades += result.TotalTrades
		totalWins   += result.Wins
	}

	fmt.Println("──────────────────────────────────────────────────────────────")
	if totalTrades > 0 {
		overallWR := float64(totalWins) / float64(totalTrades) * 100
		fmt.Printf("  Overall:  Trades: %d  WinRate: %.1f%%\n", totalTrades, overallWR)
		fmt.Println()
		if overallWR >= 65 {
			fmt.Println("  ✅ Strategy looks viable (≥65% win rate)")
		} else if overallWR >= 55 {
			fmt.Println("  ⚠️  Strategy is marginal (55-65% win rate) - use with caution")
		} else {
			fmt.Println("  ❌ Strategy underperforms (<55% win rate) - needs tuning")
		}
	}
	fmt.Println()
}
