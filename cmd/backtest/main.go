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
	trade_assets := "EURUSD,GBPUSD,AUDUSD,USDJPY,USDCAD,USDCHF,EURJPY,GBPJPY,AUDJPY,AUDCAD,EURGBP,EURAUD,EURCAD,GBPAUD,GBPCAD,CHFJPY,CADCHF,AUDCHF,AUDNZD,NZDCAD,NZDJPY,GBPCHF,GBPNZD,ETHUSD,XRPUSD,SOLUSD,DOGECOIN,CARDANO,LTCUSD,TONUSD,BCHUSD,OPENAI,ANTHROPIC,TESLA,APPLE,AMAZON,GOOGLE,MSFT,NVDA,FB,SNAP,SPACEX,PLTR,SP500,US30,USNDAQ100,GER30,UK100,EU50,JP225,AUS200,HK33,XAUUSD,XAGUSD,USOUSD,UKOUSD"
	assetsFlag := flag.String("assets", trade_assets, "Comma-separated assets to backtest")
	candles    := flag.Int("candles", 500, "Number of 1m candles to use (max ~500)")
	configPath := flag.String("config", "configs/config.yaml", "Config file path")
	threshold  := flag.Float64("threshold", 0, "Override signal threshold (0 = use config)")
	exportHours := flag.String("export-hours", "", "Write recommended UTC hours to JSON file")
	minHourWinRate := flag.Float64("min-hour-win-rate", 0.70, "Min win rate for hour export")
	minHourTrades  := flag.Int("min-hour-trades", 3, "Min trades per hour for hour export")
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

	analyzerCfg := analyzer.ApplyYAMLConfig(analyzer.DefaultConfig(), cfg.Analyzer)
	if *threshold > 0 {
		analyzerCfg.SignalThreshold = *threshold
	}
	assets := strings.Split(*assetsFlag, ",")

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                  BACKTEST RESULTS                          ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Printf("  Period: last %d 1-minute candles per asset\n", *candles)
	fmt.Printf("  Strategy: RSI + EMA + Bollinger + MACD + Volume + Patterns + MTF + Regime\n\n")

	totalTrades := 0
	totalWins   := 0
	aggregateHours := make(map[int]analyzer.HourStats)

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
		for hour, hs := range result.ByHour {
			agg := aggregateHours[hour]
			agg.Trades += hs.Trades
			agg.Wins += hs.Wins
			if agg.Trades > 0 {
				agg.WinRate = float64(agg.Wins) / float64(agg.Trades)
			}
			aggregateHours[hour] = agg
		}
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

	if *exportHours != "" {
		hours := analyzer.RecommendHours(aggregateHours, *minHourWinRate, *minHourTrades)
		if err := analyzer.SaveAllowedHoursFile(*exportHours, hours); err != nil {
			log.Fatalf("export hours: %v", err)
		}
		fmt.Printf("  ✓ Exported %d UTC hours (≥%.0f%% WR, ≥%d trades) to %s\n",
			len(hours), *minHourWinRate*100, *minHourTrades, *exportHours)
		if len(hours) > 0 {
			fmt.Printf("    Hours: %v\n", hours)
			fmt.Println("    Set analyzer.allowed_hours_file in config.yaml to use these hours.")
		} else {
			fmt.Println("    No hours met the criteria — try lowering -min-hour-win-rate or -min-hour-trades.")
		}
	}
	fmt.Println()
}
