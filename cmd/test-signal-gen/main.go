package main

import (
	"fmt"
	"log"
	"os"

	"github.com/rs/zerolog"
	"signal-bot/internal/analyzer"
	"signal-bot/internal/config"
	"signal-bot/internal/wstrader"
)

func main() {
	logger := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "15:04:05",
	}).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	logger.Info().Msg("═══════════════════════════════════════")
	logger.Info().Msg("    TESTING SIGNAL GENERATION")
	logger.Info().Msg("═══════════════════════════════════════")

	// Load config
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to IQ Option
	trader := wstrader.New(&cfg.IQOption, logger)
	logger.Info().Msg("→ Connecting to IQ Option...")
	if err := trader.Connect(); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer trader.Close()

	// Create analyzer
	analyzerCfg := analyzer.DefaultConfig()
	analyzerCfg.MinConfidence = 0.60 // Lower threshold for testing
	an := analyzer.New(trader, logger, analyzerCfg)

	// Test assets
	assets := []string{"EURUSD", "GBPUSD", "AUDUSD", "USDJPY", "OPENAI"}

	logger.Info().Msg("═══════════════════════════════════════")
	logger.Info().Msg("🔍 Analyzing assets...")
	logger.Info().Msg("═══════════════════════════════════════")

	signalsGenerated := 0

	for _, asset := range assets {
		logger.Info().Str("asset", asset).Msg("→ Analyzing...")

		signal, err := an.AnalyzeAsset(asset)
		if err != nil {
			logger.Warn().Err(err).Str("asset", asset).Msg("✗ Analysis failed")
			continue
		}

		if signal == nil {
			logger.Info().Str("asset", asset).Msg("  No signal (conditions not met)")
			continue
		}

		// Signal generated!
		signalsGenerated++

		direction := "BUY"
		directionEmoji := "🟢"
		if signal.Direction == "PUT" {
			direction = "SELL"
			directionEmoji = "🔴"
		}

		fmt.Println()
		fmt.Println("╔════════════════════════════════════════════════════╗")
		fmt.Println("║           ✅ SIGNAL GENERATED                      ║")
		fmt.Println("╚════════════════════════════════════════════════════╝")
		fmt.Printf("\n📊 Asset: %s\n", signal.Asset)
		fmt.Printf("📈 Direction: %s %s\n", directionEmoji, direction)
		fmt.Printf("🎯 Confidence: %.0f%%\n", signal.Confidence*100)
		fmt.Printf("⏱️  Expiry: %d minutes\n", signal.Expiry)
		fmt.Printf("🕐 Entry: %s\n", signal.EntryWindow.Format("15:04:05"))

		if len(signal.MartingaleLevels) > 0 {
			fmt.Println("\n📊 Martingale Levels:")
			for _, ml := range signal.MartingaleLevels {
				fmt.Printf("   • Level %d → %s\n", ml.Level, ml.Time.Format("15:04:05"))
			}
		}

		fmt.Println("\n─────────────────────────────────────────────────────")
		fmt.Println("Telegram Message Format:")
		fmt.Println("─────────────────────────────────────────────────────")
		fmt.Printf(`MEXY BINARY

🚨 TRADE NOW!!

📊 %s (OTC)
🕒 Timeframe: %d-min expiry
🤖 AI Confidence: %.0f%%
🕰️ Entry Window: %s
Direction: %s %s
`,
			signal.Asset,
			signal.Expiry,
			signal.Confidence*100,
			signal.EntryWindow.Format("3:04 PM"),
			directionEmoji,
			direction,
		)

		if len(signal.MartingaleLevels) > 0 {
			fmt.Println("\n📊 Martingale Levels:")
			for _, ml := range signal.MartingaleLevels {
				fmt.Printf("• Level %d → %s\n", ml.Level, ml.Time.Format("3:04 PM"))
			}
		}
		fmt.Println("─────────────────────────────────────────────────────")
		fmt.Println()
	}

	logger.Info().Msg("═══════════════════════════════════════")
	logger.Info().
		Int("total_analyzed", len(assets)).
		Int("signals_generated", signalsGenerated).
		Msg("✓ Analysis complete")
	logger.Info().Msg("═══════════════════════════════════════")

	if signalsGenerated == 0 {
		fmt.Println("\n💡 No signals generated - market conditions don't meet criteria")
		fmt.Println("   Try again in a few minutes, or lower MinConfidence in analyzer config")
	}
}
