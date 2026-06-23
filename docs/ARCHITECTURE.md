# Architecture Documentation

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Signal Bot System                        │
└─────────────────────────────────────────────────────────────────┘

External Services          Bot Components              Persistence
─────────────────         ──────────────────          ────────────

┌──────────────┐         ┌──────────────────┐        ┌───────────┐
│  Telegram    │         │   Main Process   │        │  SQLite   │
│  Channel     │────────▶│   cmd/bot        │───────▶│ Database  │
│ (Messages)   │         │                  │        │           │
└──────────────┘         └────────┬─────────┘        └───────────┘
                                  │                         ▲
                                  │                         │
                                  ▼                         │
                         ┌──────────────────┐              │
                         │  Telegram Client │              │
                         │  internal/telegram│             │
                         └────────┬─────────┘              │
                                  │                         │
                                  │ Message                 │
                                  │                         │
                                  ▼                         │
                         ┌──────────────────┐              │
                         │  Signal Parser   │              │
                         │  internal/parser │              │
                         └────────┬─────────┘              │
                                  │                         │
                                  │ Signal                  │
                                  │                         │
                                  ▼                         │
                         ┌──────────────────┐              │
                         │   Risk Manager   │              │
                         │   internal/bot   │              │
                         └────────┬─────────┘              │
                                  │                         │
                                  │ Validated               │
                                  │                         │
                                  ▼                         │
                         ┌──────────────────┐              │
                         │  Signal Queue    │              │
                         │  internal/queue  │              │
                         └────────┬─────────┘              │
                                  │                         │
                           ┌──────┴──────┐                 │
                           │             │                 │
                           ▼             ▼                 │
                    ┌────────────┐ ┌────────────┐         │
                    │  Worker 1  │ │  Worker 2  │         │
                    └──────┬─────┘ └──────┬─────┘         │
                           │              │                │
                           └──────┬───────┘                │
                                  │                         │
                                  ▼                         │
                         ┌──────────────────┐              │
                         │     Trader       │──────────────┘
                         │  internal/trader │
                         └────────┬─────────┘
                                  │
                                  │ Browser Automation
                                  │
┌──────────────┐                 ▼
│  IQ Options  │◀────────┌──────────────────┐
│  Traderoom   │         │   Rod + Chrome   │
│  (Website)   │         │  (go-rod/rod)    │
└──────────────┘         └──────────────────┘
```

## Component Details

### 1. Main Process (`cmd/bot/main.go`)
**Responsibilities:**
- Load configuration
- Setup logging
- Initialize bot instance
- Handle shutdown signals
- Orchestrate lifecycle

**Key Functions:**
```go
func main()
func setupLogger(cfg) Logger
```

### 2. Telegram Client (`internal/telegram/client.go`)
**Responsibilities:**
- Authenticate with Telegram
- Monitor specified channel
- Deliver messages to handler
- Manage session persistence

**Key Functions:**
```go
func (c *Client) Connect(ctx) error
func (c *Client) Listen(ctx, handler) error
func (c *Client) handleUpdate(update, handler) error
```

**External Dependencies:**
- `github.com/gotd/td` - Telegram MTProto client

### 3. Signal Parser (`internal/parser/`)
**Responsibilities:**
- Parse multiple signal formats
- Extract trading parameters
- Validate signal structure
- Normalize asset names

**Key Functions:**
```go
func (p *Parser) Parse(text string) (*Signal, error)
func ParseMexyDetailed(text) (*MexySignal, error)
```

**Supported Patterns:**
1. Mexy Binary (multi-line, with confidence)
2. Simple format: "EUR/USD CALL 5MIN"
3. Dash format: "EURUSD - CALL - 5M"
4. Buy/Sell format: "BUY EUR/USD 5 MINUTES"

### 4. Risk Manager (`internal/bot/bot.go`)
**Responsibilities:**
- Enforce daily loss limits
- Enforce hourly trade limits
- Check signal confidence threshold
- Verify minimum balance
- Track daily statistics

**Key Functions:**
```go
func (b *Bot) shouldTrade(signal) bool
```

**Rules Applied:**
```yaml
- signal.Confidence >= config.Risk.MinSignalConfidence
- balance >= config.Trading.MinBalance
- dailyLoss < config.Trading.MaxDailyLoss
- tradesPerHour < config.Risk.MaxTradesPerHour
```

### 5. Signal Queue (`internal/queue/queue.go`)
**Responsibilities:**
- Buffer signals for processing
- Provide thread-safe access
- Handle capacity limits
- Support graceful shutdown

**Key Functions:**
```go
func (q *Queue) Push(signal) error
func (q *Queue) Pop(ctx) (*Signal, error)
func (q *Queue) Close()
```

**Implementation:**
- Buffered Go channel
- Capacity: 100 signals
- Non-blocking push (returns error if full)
- Blocking pop (waits for signal or context cancel)

### 6. Worker Pool (`internal/bot/bot.go`)
**Responsibilities:**
- Execute trades concurrently
- Add delays between trades
- Handle trade errors
- Update database

**Key Functions:**
```go
func (b *Bot) tradeWorker(ctx, workerID)
```

**Flow:**
```
1. Pop signal from queue
2. Check balance
3. Wait for trade delay
4. Execute trade via Trader
5. Save to database
6. Update statistics
7. Repeat
```

### 7. Trader (`internal/trader/trader.go`)
**Responsibilities:**
- Control Chrome browser via Rod
- Login to IQ Options
- Navigate traderoom UI
- Execute trades
- Query balance

**Key Functions:**
```go
func (t *Trader) Connect(ctx) error
func (t *Trader) PlaceTrade(ctx, signal, amount) (*Trade, error)
func (t *Trader) GetBalance() (float64, error)
func (t *Trader) selectAsset(asset) error
func (t *Trader) setExpiry(minutes) error
func (t *Trader) clickDirection(direction) error
```

**External Dependencies:**
- `github.com/go-rod/rod` - Browser automation
- `github.com/go-rod/stealth` - Anti-detection

### 8. Database (`internal/database/database.go`)
**Responsibilities:**
- Persist signals and trades
- Provide query interface
- Track statistics
- Maintain indexes

**Schema:**
```sql
signals (
  id, asset, direction, expiry, amount,
  confidence, source, received_at, 
  processed_at, raw
)

trades (
  id, signal_id, asset, direction,
  amount, expiry, status, result,
  profit, placed_at, closed_at, error_msg
)
```

**Key Functions:**
```go
func (d *Database) SaveSignal(signal) error
func (d *Database) SaveTrade(trade) error
func (d *Database) GetTradeStats(since) (*TradeStats, error)
```

## Data Flow

### Signal Processing Pipeline

```
1. Message Received
   ├─ Source: Telegram channel
   ├─ Format: Multi-line text
   └─ Example: "AUD/USD (OTC)\nTimeframe: 2-min..."

2. Parse Signal
   ├─ Extract: Asset, Direction, Expiry, Confidence
   ├─ Normalize: "AUD/USD (OTC)" → "AUDUSD"
   └─ Output: Signal{Asset: "AUDUSD", Direction: CALL, ...}

3. Save to Database
   ├─ Table: signals
   └─ Status: received_at = now, processed_at = NULL

4. Risk Validation
   ├─ Check: confidence >= threshold
   ├─ Check: balance >= minimum
   ├─ Check: daily_loss < limit
   └─ Check: trades_per_hour < limit

5. Queue Signal
   ├─ Push to buffered channel
   └─ Return to listening for next signal

6. Worker Processing
   ├─ Pop from queue
   ├─ Wait for trade_delay
   └─ Execute trade

7. Trade Execution
   ├─ Browser: Navigate to IQ Options
   ├─ Browser: Select asset
   ├─ Browser: Set expiry
   ├─ Browser: Set amount
   └─ Browser: Click CALL/PUT

8. Save Trade
   ├─ Table: trades
   ├─ Link: signal_id → signals.id
   └─ Status: OPEN

9. Update Signal
   ├─ Update: processed_at = now
   └─ Complete
```

## Concurrency Model

### Goroutines

```
main()
  ├─ Telegram Connection (blocking)
  └─ Bot.Start()
       ├─ Worker 1 (loop forever)
       ├─ Worker 2 (loop forever)
       ├─ Worker 3 (loop forever)
       └─ Telegram.Listen() (blocking)
            └─ Message Handler (goroutine per message)
```

### Synchronization

**Signal Queue:**
- Channel-based (thread-safe by design)
- Multiple producers (message handlers)
- Multiple consumers (workers)

**Daily Stats:**
- Mutex-protected struct
- Read lock for checks
- Write lock for updates

**Database:**
- SQLite with WAL mode
- Sequential writes
- No explicit locking (SQLite handles it)

## Configuration Flow

```
config.yaml
    │
    ├─▶ telegram.api_id ────▶ Telegram Client
    ├─▶ telegram.channel_id ─▶ Channel Monitor
    │
    ├─▶ iqoption.email ──────▶ Trader Login
    ├─▶ iqoption.demo_mode ──▶ Account Switch
    │
    ├─▶ trading.default_amount ─▶ Trade Execution
    ├─▶ trading.max_concurrent ─▶ Worker Pool Size
    │
    ├─▶ risk.max_daily_loss ──▶ Risk Manager
    └─▶ risk.min_confidence ──▶ Signal Filter
```

## Error Handling Strategy

### Level 1: Fail Fast (Startup)
- Config load fails → Exit
- Database open fails → Exit
- Telegram auth fails → Exit
- IQ Options login fails → Exit

### Level 2: Log & Continue (Runtime)
- Signal parse fails → Log, skip signal
- Risk check fails → Log, reject signal
- Queue full → Log, drop signal

### Level 3: Retry (Trade Execution)
- Asset selection fails → Log, fail trade
- Balance check fails → Log, retry next iteration
- Browser element not found → Log, fail trade

### Level 4: Graceful Degradation
- Connection lost → Reconnect (TODO)
- Browser crash → Restart (TODO)

## Performance Characteristics

### Throughput
- **Signal ingestion**: Limited by Telegram channel frequency
- **Signal parsing**: ~100,000 signals/sec (CPU bound)
- **Trade execution**: ~3 concurrent trades (browser bound)
- **Database writes**: ~1,000 writes/sec (SSD bound)

### Latency
- **Message → Parse**: <1ms
- **Parse → Queue**: <1ms
- **Queue → Execute**: config.trade_delay_ms (default 2000ms)
- **Execute → Complete**: 2-5 seconds (browser operations)
- **Total (Message → Trade)**: ~4-8 seconds

### Resource Usage
- **Memory**: ~100MB (Go runtime + Chrome)
- **CPU**: ~5% idle, ~30% during trades
- **Disk**: ~1MB/day (database growth)
- **Network**: ~1KB/signal, ~100KB/trade

## Deployment Topology

### Option 1: Single Instance
```
[VPS] ─▶ [Signal Bot] ─▶ [Chrome] ─▶ [IQ Options]
          │
          └─▶ [SQLite]
```
**Pros:** Simple, easy to debug
**Cons:** Single point of failure

### Option 2: Multi-Instance (Future)
```
[Telegram] ─▶ [Message Queue] ─┬─▶ [Bot 1] ─▶ [IQ Account 1]
                               ├─▶ [Bot 2] ─▶ [IQ Account 2]
                               └─▶ [Bot 3] ─▶ [IQ Account 3]
```
**Pros:** Horizontal scaling, redundancy
**Cons:** Complex, requires message broker

## Security Architecture

### Data Protection
```
Secrets (config.yaml)
    ↓
Environment Variables (optional)
    ↓
In-Memory Only (no disk caching)
    ↓
Cleared on Exit
```

### Network Security
- **Telegram**: Encrypted (MTProto)
- **IQ Options**: HTTPS only
- **Database**: Local filesystem (no network exposure)

### Access Control
- Config file: 600 permissions
- Session files: 600 permissions
- Database: 644 permissions
- Logs: 644 permissions

## Monitoring Points

### Health Checks
1. Telegram connection alive
2. Browser process running
3. Worker goroutines active
4. Database responsive
5. Disk space available

### Metrics to Track
- Signals received/hour
- Signals parsed successfully
- Trades executed/hour
- Trades failed/hour
- Average execution latency
- Win rate (if results tracked)
- Daily P&L

### Alerting Thresholds
- No signals for >10 minutes
- Parse failure rate >20%
- Trade failure rate >50%
- Daily loss limit reached
- Balance < minimum threshold

## Future Architecture Improvements

1. **Message Broker**: Replace channel with Redis/NATS
2. **Result Tracking**: Poll IQ Options for closed trades
3. **Reconnection**: Exponential backoff on failures
4. **Web API**: REST endpoints for control/monitoring
5. **Distributed Tracing**: OpenTelemetry integration
6. **Hot Reload**: Watch config file for changes
7. **Multi-Broker**: Abstract trader interface
8. **Backtesting**: Replay historical signals
