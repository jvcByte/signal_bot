package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"signal-bot/internal/analyzer"
	"signal-bot/internal/config"
	"signal-bot/internal/wstrader"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "Config file path")
	candles    := flag.Int("candles", 500, "Number of 1m candles per asset")
	outputPath := flag.String("output", "data/confidence.json", "Output path for confidence model")
	minTrades  := flag.Int("min-trades", 10, "Min trades per tier to include in output")
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
	if cfg.Analyzer.SignalThreshold > 0 {
		analyzerCfg.SignalThreshold = float64(cfg.Analyzer.SignalThreshold)
	}

	assets := cfg.Analyzer.Assets
	if len(assets) == 0 {
		assets = []string{"EURUSD", "GBPUSD", "AUDUSD", "USDJPY"}
	}

	model := analyzer.NewConfidenceModel()

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           CONFIDENCE MODEL CALIBRATION                      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Printf("  Assets: %d  Candles: %d  Min-trades: %d\n\n", len(assets), *candles, *minTrades)

	totalCalibrated := 0

	for _, asset := range assets {
		asset = strings.TrimSpace(strings.ToUpper(asset))

		c1m, err := trader.GetHistoricalCandles(asset, 60, *candles)
		if err != nil || len(c1m) < 50 {
			logger.Warn().Str("asset", asset).Msg("Could not fetch candles, skipping")
			continue
		}

		c5m, _  := trader.GetHistoricalCandles(asset, 300, 100)
		c15m, _ := trader.GetHistoricalCandles(asset, 900, 50)

		// Run detailed backtest to get per-regime, per-tier win rates
		result := analyzer.BacktestAsset(asset, c1m, c5m, c15m, analyzerCfg, 2, logger)

		if result.TotalTrades == 0 {
			fmt.Printf("  %-12s  No trades generated\n", asset)
			continue
		}

		// Feed outcomes into the confidence model per regime
		// We don't have per-trade regime data from BacktestAsset directly,
		// so we use the ByRegime stats to seed the model
		for regime, rs := range result.ByRegime {
			if rs.Trades < *minTrades {
				continue
			}
			// Estimate average score tier from ScoreTiers
			for tier, ts := range result.ScoreTiers {
				if ts.Trades < *minTrades {
					continue
				}
				// Seed model with backtest win rate for this regime+tier
				wins := int(float64(ts.Trades) * ts.WinRate)
				for i := 0; i < ts.Trades; i++ {
					won := i < wins
					model.Update(float64(tier), regime, won)
				}
				totalCalibrated++
				_ = rs
			}
		}

		winEmoji := "✅"
		if result.WinRate < 0.55 {
			winEmoji = "❌"
		} else if result.WinRate < 0.65 {
			winEmoji = "⚠️ "
		}

		fmt.Printf("  %s %-12s  Trades: %3d  WinRate: %.1f%%  PF: %.2f\n",
			winEmoji, asset, result.TotalTrades, result.WinRate*100, result.ProfitFactor)

		// Print regime breakdown
		for regime, rs := range result.ByRegime {
			if rs.Trades > 0 {
				fmt.Printf("       %s: %d trades, %.1f%% WR\n", regime.String(), rs.Trades, rs.WinRate*100)
			}
		}
		fmt.Println()
	}

	// Save model to disk
	if err := model.SaveToFile(*outputPath); err != nil {
		log.Fatalf("failed to save confidence model: %v", err)
	}

	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf("  ✓ Confidence model saved to %s\n", *outputPath)
	fmt.Printf("  ✓ %d regime+tier combinations calibrated\n", totalCalibrated)
	fmt.Println()
	fmt.Println("  Run the signal generator - it will load this file automatically.")
	fmt.Println("  Re-run calibration periodically as market conditions change.")
	fmt.Println()
}
