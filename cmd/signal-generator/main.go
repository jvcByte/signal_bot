package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"signal-bot/internal/analyzer"
	"signal-bot/internal/config"
	"signal-bot/internal/telegram"
	"signal-bot/internal/wstrader"
	"signal-bot/pkg/models"
)

var (
	configPath = flag.String("config", "configs/config.yaml", "Path to config file")
)

func main() {
	flag.Parse()

	// Setup logger
	logger := zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: "15:04:05",
	}).With().Timestamp().Logger().Level(zerolog.InfoLevel)

	logger.Info().Msg("═══════════════════════════════════════")
	logger.Info().Msg("    AI SIGNAL GENERATOR")
	logger.Info().Msg("═══════════════════════════════════════")

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to IQ Option for market data
	trader := wstrader.New(&cfg.IQOption, logger)
	logger.Info().Msg("→ Connecting to IQ Option for market data...")
	if err := trader.Connect(); err != nil {
		log.Fatalf("Failed to connect to IQ Option: %v", err)
	}
	defer trader.Close()

	// Connect to Telegram for posting signals
	tg := telegram.New(&cfg.Telegram, logger)
	logger.Info().Msg("→ Connecting to Telegram...")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run Telegram client in background
	go func() {
		if err := tg.Connect(ctx); err != nil && err != context.Canceled {
			logger.Error().Err(err).Msg("Telegram connection error")
		}
	}()

	// Give Telegram time to connect
	time.Sleep(2 * time.Second)

	// Create analyzer with config-driven settings
	analyzerCfg := analyzer.DefaultConfig()
	if cfg.Analyzer.SignalThreshold > 0 {
		analyzerCfg.SignalThreshold = cfg.Analyzer.SignalThreshold
	}
	if cfg.Analyzer.SignalCooldown > 0 {
		analyzerCfg.SignalCooldown = cfg.Analyzer.SignalCooldown
	}
	an := analyzer.New(trader, logger, analyzerCfg)

	// Asset list from config (fallback to defaults)
	assetList := cfg.Analyzer.Assets
	if len(assetList) == 0 {
		assetList = []string{"EURUSD", "GBPUSD", "AUDUSD", "USDJPY"}
		logger.Warn().Msg("No assets in config, using defaults")
	}

	intervalSec := cfg.Analyzer.IntervalSeconds
	if intervalSec <= 0 {
		intervalSec = 60
	}

	logger.Info().
		Strs("assets", assetList).
		Int("interval_sec", intervalSec).
		Int("signal_threshold", analyzerCfg.SignalThreshold).
		Int("signal_cooldown_min", analyzerCfg.SignalCooldown).
		Msg("✓ Signal generator ready")

	logger.Info().Msg("═══════════════════════════════════════")
	logger.Info().Msg("✓ GENERATOR ACTIVE")
	logger.Info().Msg("═══════════════════════════════════════")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	// Run analysis loop
	for {
		select {
		case <-ticker.C:
			analyzeAndSendSignals(ctx, an, tg, assetList, logger, cfg.Telegram.ChannelID)

		case <-sigChan:
			logger.Info().Msg("Shutting down signal generator...")
			return
		}
	}
}

func analyzeAndSendSignals(ctx context.Context, an *analyzer.SignalAnalyzer, tg *telegram.Client, assets []string, logger zerolog.Logger, channelID int64) {
	logger.Info().Msg("─────────────────────────────────────")
	logger.Info().Msg("🔍 Analyzing market conditions...")

	signalsFound := 0
	for _, asset := range assets {
		signal, err := an.AnalyzeAsset(asset)
		if err != nil {
			logger.Warn().Err(err).Str("asset", asset).Msg("Analysis failed")
			continue
		}

		if signal == nil {
			logger.Info().Str("asset", asset).Msg("  ↳ No signal")
			continue
		}

		signalsFound++

		// Format signal as Mexy-style message
		message := formatSignalMessage(signal)

		// Post to Telegram
		if err := tg.SendMessage(ctx, channelID, message); err != nil {
			logger.Error().Err(err).Msg("Failed to send signal to Telegram")
			continue
		}

		logger.Info().
			Str("asset", asset).
			Str("direction", signal.Direction.String()).
			Float64("confidence", signal.Confidence).
			Msg("✅ Signal posted to Telegram")
	}

	if signalsFound == 0 {
		logger.Info().Msg("  ↳ No signals this cycle (market conditions not met)")
	}
}

func formatSignalMessage(signal *models.Signal) string {
	direction := "BUY"
	directionEmoji := "🟢"
	assetArrow := "📈"
	if signal.Direction == models.DirectionPut {
		direction = "SELL"
		directionEmoji = "🔴"
		assetArrow = "📉"
	}

	// Format asset as XXX/YYY
	asset := signal.Asset
	if len(asset) == 6 {
		asset = asset[:3] + "/" + asset[3:]
	}

	msg := fmt.Sprintf(`JVCBYTE BLITZ

🚨 TRADE NOW!!

%s  %s (OTC)
🕒  Timeframe: %d-min expiry
🤖  AI Confidence: %.0f%%
🕰️  Entry Window: %s
Direction: %s %s

📊  Martingale Levels:`,
		assetArrow,
		asset,
		signal.Expiry,
		signal.Confidence*100,
		signal.EntryWindow.Format("3:04 PM"),
		directionEmoji,
		direction,
	)

	for _, ml := range signal.MartingaleLevels {
		msg += fmt.Sprintf("\n• Level %d  →  %s", ml.Level, ml.Time.Format("3:04 PM"))
	}

	return msg
}
