package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"signal-bot/internal/bot"
	"signal-bot/internal/config"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	logger := setupLogger(cfg)

	logger.Info().Msg("starting signal bot")
	logger.Info().
		Bool("demo_mode", cfg.IQOption.DemoMode).
		Int("max_concurrent_trades", cfg.Trading.MaxConcurrentTrades).
		Float64("default_amount", cfg.Trading.DefaultAmount).
		Msg("configuration loaded")

	// Create bot
	b, err := bot.New(cfg, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to create bot")
	}

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info().Msg("received shutdown signal")
		cancel()
	}()

	// Start bot
	if err := b.Start(ctx); err != nil {
		logger.Error().Err(err).Msg("bot stopped with error")
		b.Stop()
		os.Exit(1)
	}

	b.Stop()
	logger.Info().Msg("bot stopped gracefully")
}

func setupLogger(cfg *config.Config) zerolog.Logger {
	// Parse log level
	level, err := zerolog.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(level)

	// Setup file logging
	if cfg.Logging.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Logging.File), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log directory: %v\n", err)
			os.Exit(1)
		}

		logFile, err := os.OpenFile(cfg.Logging.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
			os.Exit(1)
		}

		if cfg.Logging.Console {
			multi := zerolog.MultiLevelWriter(
				zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"},
				logFile,
			)
			return zerolog.New(multi).With().Timestamp().Logger()
		}

		return zerolog.New(logFile).With().Timestamp().Logger()
	}

	// Console only
	return log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"})
}
