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
	"signal-bot/internal/wstrader"
	"signal-bot/pkg/models"
)

type Bot struct {
	cfg      *config.Config
	tg       *telegram.Client
	trader   *wstrader.Trader
	parser   *parser.Parser
	queue    *queue.Queue
	db       *database.Database
	logger   zerolog.Logger

	mu         sync.RWMutex
	dailyStats *DailyStats

	// dedup: track recently seen signal hashes to avoid trading duplicates
	recentSignals   map[string]time.Time
	recentSignalsMu sync.Mutex
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

	// Load today's stats from DB so risk limits account for existing trades
	stats := &DailyStats{LastReset: time.Now()}
	if todayStats, err := db.GetTodayStats(); err == nil {
		stats.TradesCount = todayStats.Total
		stats.TotalLoss = todayStats.TotalProfit // already summed as loss amount
		logger.Info().
			Int("trades_today", stats.TradesCount).
			Float64("loss_today", stats.TotalLoss).
			Msg("📊 Loaded today's existing trade stats from database")
	}

	return &Bot{
		cfg:           cfg,
		tg:            telegram.New(&cfg.Telegram, logger),
		trader:        wstrader.New(&cfg.IQOption, logger),
		parser:        parser.New(),
		queue:         queue.New(100),
		db:            db,
		logger:        logger,
		dailyStats:    stats,
		recentSignals: make(map[string]time.Time),
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info().Msg("═══════════════════════════════════════")
	b.logger.Info().Msg("    SIGNAL BOT STARTING UP")
	b.logger.Info().Msg("═══════════════════════════════════════")

	// Connect to IQ Option via WebSocket API
	b.logger.Info().Msg("→ Step 1/3: Connecting to IQ Option via WebSocket API...")
	if err := b.trader.Connect(); err != nil {
		return fmt.Errorf("trader connect: %w", err)
	}

	// Register trade result handler
	b.trader.SetResultHandler(b.handleTradeResult)

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
	if !signal.EntryWindow.IsZero() {
		waitDuration := time.Until(signal.EntryWindow).Round(time.Second)
		b.logger.Info().
			Str("entry_window", signal.EntryWindow.Format("15:04:05")).
			Dur("wait_for", waitDuration).
			Msg("🕐 Trade will execute at entry window")
	}
	b.logger.Info().Str("raw_signal", signal.Raw).Msg("📝 raw signal text")
	b.logger.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Deduplicate: same asset+entryWindow within 3 minutes = duplicate signal
	// Ignore direction - channel sends same signal multiple times with different directions
	dedupKey := fmt.Sprintf("%s_%s", signal.Asset, signal.EntryWindow.Format("15:04"))
	b.recentSignalsMu.Lock()
	if lastSeen, exists := b.recentSignals[dedupKey]; exists && time.Since(lastSeen) < 3*time.Minute {
		b.recentSignalsMu.Unlock()
		b.logger.Debug().Str("key", dedupKey).Msg("⏭️  Duplicate signal ignored")
		return nil
	}
	b.recentSignals[dedupKey] = time.Now()
	// Clean up old entries
	for k, t := range b.recentSignals {
		if time.Since(t) > 5*time.Minute {
			delete(b.recentSignals, k)
		}
	}
	b.recentSignalsMu.Unlock()

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

	// Reset daily stats if it's a new calendar day
	now := time.Now()
	if b.dailyStats.LastReset.Day() != now.Day() || b.dailyStats.LastReset.Month() != now.Month() {
		b.logger.Info().Msg("  resetting daily statistics (new day)")
		b.dailyStats.TradesCount = 0
		b.dailyStats.TotalLoss = 0
		b.dailyStats.LastReset = now
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

		// Wait for entry window if specified
		if !signal.EntryWindow.IsZero() {
			now := time.Now()
			waitDuration := time.Until(signal.EntryWindow)

			if waitDuration > 5*time.Minute {
				// Entry window too far in the future
				b.logger.Warn().
					Int("worker_id", workerID).
					Str("entry_window", signal.EntryWindow.Format("15:04:05")).
					Dur("wait", waitDuration).
					Msg("⛔ Entry window too far away, skipping signal")
				continue
			}

			if waitDuration < -2*time.Minute {
				// Entry window already passed by more than 2 minutes
				b.logger.Warn().
					Int("worker_id", workerID).
					Str("entry_window", signal.EntryWindow.Format("15:04:05")).
					Msg("⛔ Entry window already expired, skipping signal")
				continue
			}

			if waitDuration > 0 {
				b.logger.Info().
					Int("worker_id", workerID).
					Str("entry_window", signal.EntryWindow.Format("15:04:05")).
					Str("current_time", now.Format("15:04:05")).
					Dur("waiting", waitDuration.Round(time.Second)).
					Msg("⏰ Waiting for entry window...")

				select {
				case <-ctx.Done():
					return
				case <-time.After(waitDuration):
				}
			}

			b.logger.Info().
				Int("worker_id", workerID).
				Str("entry_window", signal.EntryWindow.Format("15:04:05")).
				Msg("✅ Entry window reached - executing trade NOW")
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
		// Use signal.Amount if set (martingale override), else use config default
		tradeAmount := b.cfg.Trading.DefaultAmount
		if signal.Amount > 0 {
			tradeAmount = signal.Amount
		}

		b.logger.Info().
			Int("worker_id", workerID).
			Str("asset", signal.Asset).
			Str("direction", signal.Direction.String()).
			Float64("amount", tradeAmount).
			Msg("🎯 EXECUTING TRADE...")

		trade, err := b.trader.PlaceTrade(signal, tradeAmount)
		if err != nil {
			b.logger.Error().
				Int("worker_id", workerID).
				Err(err).
				Msg("❌ TRADE FAILED")
			continue
		}

		// ID was already set inside PlaceTrade to match what's in openTrades
		// Do NOT override it with a new UUID here
		
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

func (b *Bot) handleTradeResult(result wstrader.TradeResult) {
	now := result.ClosedAt

	status := models.StatusClosed
	tradeResult := models.ResultLose
	if result.Win {
		tradeResult = models.ResultWin
	}

	resultStr := "LOSS ❌"
	if result.Win {
		resultStr = "WIN  ✅"
	}

	b.logger.Info().
		Str("trade_id", result.TradeID).
		Str("result", resultStr).
		Float64("profit", result.Profit).
		Msg("🏁 Trade closed")

	// Update trade in database
	trade := &models.Trade{
		ID:       result.TradeID,
		Status:   status,
		Result:   tradeResult,
		Profit:   result.Profit,
		ClosedAt: &now,
	}
	if err := b.db.UpdateTrade(trade); err != nil {
		b.logger.Error().Err(err).Str("trade_id", result.TradeID).Msg("failed to update trade result")
	}

	if !result.Win {
		// Update daily P&L
		b.dailyStats.mu.Lock()
		b.dailyStats.TotalLoss += -result.Profit
		loss := b.dailyStats.TotalLoss
		b.dailyStats.mu.Unlock()

		b.logger.Warn().
			Float64("daily_loss", loss).
			Float64("limit", b.cfg.Trading.MaxDailyLoss).
			Msg("📉 Loss recorded")

		// Martingale: queue next level if enabled and levels remain
		if b.cfg.Risk.Martingale && result.Signal != nil {
			b.tryMartingale(result)
		}
	}
}

func (b *Bot) tryMartingale(result wstrader.TradeResult) {
	// Recover from any panic to prevent bot crash
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error().Interface("panic", r).Msg("panic in tryMartingale - recovered")
		}
	}()

	signal := result.Signal
	if signal == nil || len(signal.MartingaleLevels) == 0 {
		return
	}

	now := time.Now()

	for i, ml := range signal.MartingaleLevels {
		// Skip levels beyond configured max
		if b.cfg.Risk.MartingaleMax > 0 && ml.Level > b.cfg.Risk.MartingaleMax {
			break
		}

		// Find the next level that hasn't passed yet
		windowEnd := ml.Time.Add(2 * time.Minute)
		if now.After(windowEnd) {
			continue // this level already passed
		}

		// Double the amount for each martingale level
		newAmount := result.Amount * float64(int(1)<<i) * 2
		waitFor := time.Until(ml.Time)
		if waitFor < 0 {
			waitFor = 0
		}

		b.logger.Info().
			Int("level", ml.Level).
			Str("entry_time", ml.Time.Format("15:04:05")).
			Float64("amount", newAmount).
			Int64("wait_sec", int64(waitFor.Seconds())).
			Msg("Martingale: scheduling re-entry on loss")

		// Create a new signal clone with updated amount and entry window
		martingaleSignal := *signal // copy
		martingaleSignal.ID = uuid.New().String()
		martingaleSignal.EntryWindow = ml.Time
		martingaleSignal.Amount = newAmount
		// Pass remaining levels (safe slice)
		if i+1 < len(signal.MartingaleLevels) {
			martingaleSignal.MartingaleLevels = signal.MartingaleLevels[i+1:]
		} else {
			martingaleSignal.MartingaleLevels = nil
		}

		if err := b.queue.Push(&martingaleSignal); err != nil {
			b.logger.Error().Err(err).Int("level", ml.Level).Msg("failed to queue martingale signal")
		} else {
			b.logger.Info().Int("level", ml.Level).Float64("amount", newAmount).Msg("✅ Martingale signal queued")
		}
		return // only queue the next level, not all of them
	}

	b.logger.Debug().Msg("no valid martingale levels remaining")
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
