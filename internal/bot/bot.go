package bot

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"signal-bot/internal/config"
	"signal-bot/internal/database"
	"signal-bot/internal/parser"
	"signal-bot/internal/queue"
	"signal-bot/internal/telegram"
	"signal-bot/internal/trader"
	"signal-bot/pkg/models"
)

type Bot struct {
	cfg      *config.Config
	tg       *telegram.Client
	trader   *trader.Trader
	parser   *parser.Parser
	queue    *queue.Queue
	db       *database.Database
	logger   zerolog.Logger
	
	activeTradesCount int
	mu                sync.RWMutex
	dailyStats        *DailyStats
}

type DailyStats struct {
	TradesCount int
	TotalLoss   float64
	LastReset   time.Time
	mu          sync.RWMutex
}

func New(cfg *config.Config, logger zerolog.Logger) (*Bot, error) {
	db, err := database.New(cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}

	return &Bot{
		cfg:      cfg,
		tg:       telegram.New(&cfg.Telegram, logger),
		trader:   trader.New(&cfg.IQOption, logger),
		parser:   parser.New(),
		queue:    queue.New(100),
		db:       db,
		logger:   logger,
		dailyStats: &DailyStats{
			LastReset: time.Now(),
		},
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info().Msg("═══════════════════════════════════════")
	b.logger.Info().Msg("    SIGNAL BOT STARTING UP")
	b.logger.Info().Msg("═══════════════════════════════════════")

	// Connect to IQ Option first
	b.logger.Info().Msg("→ Step 1/3: Connecting to IQ Option...")
	if err := b.trader.Connect(ctx); err != nil {
		return fmt.Errorf("trader connect: %w", err)
	}

	// Start workers
	b.logger.Info().
		Int("workers", b.cfg.Trading.MaxConcurrentTrades).
		Msg("→ Step 2/3: Starting trade worker pool...")
	for i := 0; i < b.cfg.Trading.MaxConcurrentTrades; i++ {
		go b.tradeWorker(ctx, i)
	}

	b.logger.Info().Msg("═══════════════════════════════════════")
	b.logger.Info().Msg("✓ BOT READY")
	b.logger.Info().Msg("═══════════════════════════════════════")
	
	// Connect to Telegram and start listening (this blocks)
	b.logger.Info().Msg("→ Step 3/3: Connecting to Telegram and listening...")
	if err := b.tg.ConnectAndListen(ctx, b.handleMessage); err != nil {
		return fmt.Errorf("telegram: %w", err)
	}

	return nil
}

func (b *Bot) handleMessage(ctx context.Context, message string) error {
	b.logger.Info().Msg("───────────────────────────────────────")
	b.logger.Info().Str("preview", message[:min(100, len(message))]).Msg("📨 NEW MESSAGE RECEIVED")
	
	// Log the full message for debugging
	b.logger.Debug().Str("full_message", message).Msg("complete message content")

	b.logger.Debug().Msg("attempting to parse signal...")
	signal, err := b.parser.Parse(message)
	if err != nil {
		b.logger.Debug().Err(err).Msg("❌ not a valid signal format (ignoring)")
		return nil
	}

	signal.ID = uuid.New().String()
	signal.Source = "telegram"

	b.logger.Info().Msg("✓ SIGNAL PARSED SUCCESSFULLY")
	b.logger.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	b.logger.Info().
		Str("signal_id", signal.ID[:8]).
		Str("asset", signal.Asset).
		Str("direction", signal.Direction.String()).
		Int("expiry_minutes", signal.Expiry).
		Float64("confidence_pct", signal.Confidence*100).
		Msg("📊 SIGNAL DETAILS")
	b.logger.Info().Str("raw_signal", signal.Raw).Msg("📝 raw signal text")
	b.logger.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	b.logger.Debug().Msg("saving signal to database...")
	if err := b.db.SaveSignal(signal); err != nil {
		b.logger.Error().Err(err).Msg("failed to save signal")
	} else {
		b.logger.Debug().Msg("✓ signal saved to database")
	}

	// Risk checks
	b.logger.Info().Msg("⚖️  Running risk management checks...")
	if !b.shouldTrade(signal) {
		b.logger.Warn().Msg("❌ SIGNAL REJECTED by risk management")
		return nil
	}
	b.logger.Info().Msg("✓ Risk checks passed")

	b.logger.Info().Msg("📤 Queuing signal for execution...")
	if err := b.queue.Push(signal); err != nil {
		b.logger.Error().Err(err).Msg("failed to queue signal")
		return nil
	}

	queueLen := b.queue.Len()
	b.logger.Info().
		Int("queue_length", queueLen).
		Msg("✓ Signal queued successfully")

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (b *Bot) shouldTrade(signal *models.Signal) bool {
	if !b.cfg.Risk.Enabled {
		b.logger.Debug().Msg("  risk management disabled")
		return true
	}

	b.dailyStats.mu.RLock()
	defer b.dailyStats.mu.RUnlock()

	// Reset daily stats if new day
	if time.Since(b.dailyStats.LastReset) > 24*time.Hour {
		b.logger.Info().Msg("  resetting daily statistics (new day)")
		b.dailyStats.TradesCount = 0
		b.dailyStats.TotalLoss = 0
		b.dailyStats.LastReset = time.Now()
	}

	// Check daily loss limit
	b.logger.Debug().
		Float64("current_loss", b.dailyStats.TotalLoss).
		Float64("limit", b.cfg.Trading.MaxDailyLoss).
		Msg("  checking daily loss limit")
	if b.dailyStats.TotalLoss >= b.cfg.Trading.MaxDailyLoss {
		b.logger.Warn().
			Float64("loss", b.dailyStats.TotalLoss).
			Float64("limit", b.cfg.Trading.MaxDailyLoss).
			Msg("  ⛔ daily loss limit reached")
		return false
	}

	// Check hourly trade limit
	b.logger.Debug().
		Int("current_count", b.dailyStats.TradesCount).
		Int("limit", b.cfg.Risk.MaxTradesPerHour).
		Msg("  checking hourly trade limit")
	if b.dailyStats.TradesCount >= b.cfg.Risk.MaxTradesPerHour {
		b.logger.Warn().
			Int("count", b.dailyStats.TradesCount).
			Int("limit", b.cfg.Risk.MaxTradesPerHour).
			Msg("  ⛔ hourly trade limit reached")
		return false
	}

	// Check signal confidence
	b.logger.Debug().
		Float64("signal_confidence", signal.Confidence*100).
		Float64("min_required", b.cfg.Risk.MinSignalConfidence*100).
		Msg("  checking signal confidence")
	if signal.Confidence < b.cfg.Risk.MinSignalConfidence {
		b.logger.Warn().
			Float64("confidence", signal.Confidence*100).
			Float64("required", b.cfg.Risk.MinSignalConfidence*100).
			Msg("  ⛔ signal confidence too low")
		return false
	}

	b.logger.Debug().Msg("  ✓ all risk checks passed")
	return true
}

func (b *Bot) tradeWorker(ctx context.Context, workerID int) {
	b.logger.Info().Int("worker_id", workerID).Msg("✓ trade worker started and ready")

	for {
		b.logger.Debug().Int("worker_id", workerID).Msg("waiting for signal from queue...")
		signal, err := b.queue.Pop(ctx)
		if err != nil {
			if err == context.Canceled {
				b.logger.Info().Int("worker_id", workerID).Msg("worker shutting down")
				return
			}
			b.logger.Error().Int("worker_id", workerID).Err(err).Msg("queue pop error")
			continue
		}

		b.logger.Info().
			Int("worker_id", workerID).
			Str("signal_id", signal.ID[:8]).
			Msg("═══════════════════════════════════════")
		b.logger.Info().
			Int("worker_id", workerID).
			Msg("🔧 WORKER PROCESSING SIGNAL")

		// Check balance before trading (non-fatal if it fails)
		b.logger.Info().Int("worker_id", workerID).Msg("checking account balance...")
		balance, err := b.trader.GetBalance()
		if err != nil {
			b.logger.Warn().Int("worker_id", workerID).Err(err).Msg("⚠️  could not read balance (will proceed anyway)")
			// Continue with trade execution even if balance check fails
		} else {
			b.logger.Info().
				Int("worker_id", workerID).
				Float64("balance", balance).
				Float64("required", b.cfg.Trading.MinBalance).
				Msg("💰 current balance")

			if balance < b.cfg.Trading.MinBalance {
				b.logger.Warn().
					Int("worker_id", workerID).
					Float64("balance", balance).
					Float64("required", b.cfg.Trading.MinBalance).
					Msg("⛔ balance too low, skipping trade")
				continue
			}
		}

		// Add delay between trades
		if b.cfg.Trading.TradeDelayMs > 0 {
			b.logger.Info().
				Int("worker_id", workerID).
				Int("delay_ms", b.cfg.Trading.TradeDelayMs).
				Msg("⏳ waiting before trade execution...")
			time.Sleep(time.Duration(b.cfg.Trading.TradeDelayMs) * time.Millisecond)
		}

		// Place trade
		b.logger.Info().
			Int("worker_id", workerID).
			Str("asset", signal.Asset).
			Str("direction", signal.Direction.String()).
			Float64("amount", b.cfg.Trading.DefaultAmount).
			Msg("🎯 EXECUTING TRADE...")
			
		trade, err := b.trader.PlaceTrade(ctx, signal, b.cfg.Trading.DefaultAmount)
		if err != nil {
			b.logger.Error().
				Int("worker_id", workerID).
				Err(err).
				Msg("❌ TRADE FAILED")
			continue
		}

		trade.ID = uuid.New().String()
		
		b.logger.Info().
			Int("worker_id", workerID).
			Str("trade_id", trade.ID[:8]).
			Str("status", string(trade.Status)).
			Msg("✓ TRADE EXECUTED SUCCESSFULLY")

		b.logger.Debug().Int("worker_id", workerID).Msg("saving trade to database...")
		if err := b.db.SaveTrade(trade); err != nil {
			b.logger.Error().Int("worker_id", workerID).Err(err).Msg("failed to save trade")
		} else {
			b.logger.Debug().Int("worker_id", workerID).Msg("✓ trade saved to database")
		}

		// Update daily stats
		b.dailyStats.mu.Lock()
		b.dailyStats.TradesCount++
		count := b.dailyStats.TradesCount
		b.dailyStats.mu.Unlock()

		b.logger.Info().
			Int("worker_id", workerID).
			Int("trades_today", count).
			Msg("📊 updated daily statistics")

		now := time.Now()
		signal.ProcessedAt = &now
		if err := b.db.SaveSignal(signal); err != nil {
			b.logger.Error().Int("worker_id", workerID).Err(err).Msg("failed to update signal")
		}

		b.logger.Info().Int("worker_id", workerID).Msg("✓ worker cycle complete")
	}
}

func (b *Bot) Stop() error {
	b.logger.Info().Msg("stopping bot")
	
	b.queue.Close()
	
	if err := b.trader.Close(); err != nil {
		b.logger.Error().Err(err).Msg("failed to close trader")
	}
	
	if err := b.db.Close(); err != nil {
		b.logger.Error().Err(err).Msg("failed to close database")
	}
	
	return nil
}
