# Signal Trading Bot - Architecture Diagram

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          SIGNAL TRADING BOT                                 │
│                    Automated Trading via Telegram Signals                   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## High-Level Architecture

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Telegram   │────▶│   Signal     │────▶│   Risk       │────▶│   Trade      │
│   Channel    │     │   Parser     │     │   Manager    │     │   Executor   │
└──────────────┘     └──────────────┘     └──────────────┘     └──────────────┘
       │                    │                    │                    │
       │                    │                    │                    ▼
       │                    │                    │              ┌──────────────┐
       │                    │                    │              │   IQ Option  │
       │                    │                    │              │   WebSocket  │
       │                    │                    │              │     API      │
       │                    │                    │              └──────────────┘
       │                    │                    │                    │
       │                    │                    ▼                    │
       │                    │              ┌──────────────┐           │
       │                    │              │   Signal     │           │
       │                    │              │   Queue      │           │
       │                    │              └──────────────┘           │
       │                    │                    │                    │
       │                    ▼                    ▼                    ▼
       │              ┌──────────────┐    ┌──────────────┐    ┌──────────────┐
       └─────────────▶│   Database   │    │   Worker     │    │   Result     │
                      │   (SQLite)   │    │   Pool       │    │   Handler    │
                      └──────────────┘    └──────────────┘    └──────────────┘
```

## Component Detail

### 1. Entry Point (`cmd/bot/main.go`)

```
┌─────────────────────────────────────┐
│         main()                      │
│  ┌───────────────────────────────┐  │
│  │ Load config (YAML)            │  │
│  │ Setup logger (zerolog)        │  │
│  │ Create Bot instance           │  │
│  │ Handle shutdown signals       │  │
│  │ Start Bot                     │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

### 2. Bot Orchestrator (`internal/bot/bot.go`)

```
┌─────────────────────────────────────────────────────────────────┐
│                         Bot                                     │
├─────────────────────────────────────────────────────────────────┤
│ Components:                                                     │
│  • Telegram Client (MTProto)                                    │
│  • WebSocket Trader (IQ Option)                                 │
│  • Signal Parser                                                │
│  • Signal Queue (thread-safe)                                   │
│  • Database (SQLite)                                            │
│  • Risk Manager                                                 │
│  • Worker Pool (concurrent trades)                              │
├─────────────────────────────────────────────────────────────────┤
│ State:                                                          │
│  • DailyStats (trades, P&L)                                     │
│  • RecentSignals (deduplication cache)                          │
└─────────────────────────────────────────────────────────────────┘
```

### 3. Signal Flow

```
┌──────────────────────────────────────────────────────────────────────────┐
│                         SIGNAL PROCESSING FLOW                           │
└──────────────────────────────────────────────────────────────────────────┘

Telegram Message
       │
       ▼
┌──────────────────────┐
│  handleMessage()     │
│  • Parse signal      │◀──┐
│  • Deduplicate       │   │
│  • Save to DB        │   │
│  • Risk checks       │   │
│  • Queue signal      │   │
└──────────────────────┘   │
       │                   │
       ▼                   │
┌──────────────────────┐   │
│  shouldTrade()       │   │
│  • Daily limit       │   │
│  • Confidence check  │   │
│  • Balance check     │   │
└──────────────────────┘   │
       │                   │
       ▼                   │
┌──────────────────────┐   │
│  Queue Push          │   │
└──────────────────────┘   │
       │                   │
       ▼                   │
┌──────────────────────┐   │
│  tradeWorker()       │───┘ (N workers)
│  • Wait for entry    │
│  • Check balance     │
│  • Place trade       │
│  • Update stats      │
└──────────────────────┘
       │
       ▼
┌──────────────────────┐
│  handleTradeResult() │
│  • Update DB         │
│  • Martingale logic  │
│  • Update P&L        │
└──────────────────────┘
```

### 4. Telegram Client (`internal/telegram/client.go`)

```
┌─────────────────────────────────────────────────────────────┐
│                   Telegram Client (MTProto)                 │
├─────────────────────────────────────────────────────────────┤
│  • ConnectAndListen() - Main polling loop                   │
│  • pollMessages() - Poll channel every 2s                   │
│  • authenticate() - Phone + code + 2FA                      │
│  • Session persistence (file-based)                         │
│  • Auto-reconnect with backoff                              │
├─────────────────────────────────────────────────────────────┤
│  Flow:                                                      │
│  1. Resolve channel access hash                             │
│  2. Poll MessagesGetHistory                                 │
│  3. Filter new messages only                                │
│  4. Call handler callback                                   │
└─────────────────────────────────────────────────────────────┘
```

### 5. Signal Parser (`internal/parser/parser.go`)

```
┌─────────────────────────────────────────────────────────────┐
│                      Signal Parser                          │
├─────────────────────────────────────────────────────────────┤
│  Supported Formats:                                         │
│  • Mexy Binary (detailed with confidence/martingale)        │
│  • Pattern 1: "EUR/USD CALL 5MIN"                           │
│  • Pattern 2: "EURUSD - CALL - 5M"                          │
│  • Pattern 3: "BUY EURUSD 5 MINUTES"                        │
├─────────────────────────────────────────────────────────────┤
│  Extracts:                                                  │
│  • Asset (e.g., EURUSD)                                     │
│  • Direction (CALL/PUT)                                     │
│  • Expiry (seconds)                                         │
│  • Confidence (0-1)                                         │
│  • Entry Window (timestamp)                                 │
│  • Martingale Levels (re-entry times)                       │
└─────────────────────────────────────────────────────────────┘
```

### 6. WebSocket Trader (`internal/wstrader/`)

```
┌─────────────────────────────────────────────────────────────┐
│                  IQ Option WebSocket Trader                 │
├─────────────────────────────────────────────────────────────┤
│  Components:                                                │
│  • auth.go - HTTP login, session management                 │
│  • ws.go - WebSocket connection, message handling           │
│  • trade.go - Trade placement, balance management           │
│  • candles.go - Candle data streaming                       │
│  • getactives.go - Available assets resolution              │
├─────────────────────────────────────────────────────────────┤
│  Trade Flow:                                                │
│  1. Authenticate via HTTP API                               │
│  2. Connect WebSocket                                       │
│  3. Subscribe to balance/profile updates                    │
│  4. Resolve asset → active_id                               │
│  5. Place "binary-options.open-option"                      │
│  6. Track option_id → result mapping                        │
│  7. Handle "option-closed" events                           │
└─────────────────────────────────────────────────────────────┘
```

### 7. Database Schema (`internal/database/database.go`)

```
┌─────────────────────────────────────────────────────────────┐
│                    SQLite Database                          │
├─────────────────────────────────────────────────────────────┤
│  signals table:                                             │
│  • id (PK)                                                  │
│  • asset, direction, expiry, amount, confidence             │
│  • source, received_at, processed_at                        │
│  • raw (original message)                                   │
├─────────────────────────────────────────────────────────────┤
│  trades table:                                              │
│  • id (PK)                                                  │
│  • signal_id (FK)                                           │
│  • asset, direction, amount, expiry                         │
│  • status (PENDING/OPEN/CLOSED/FAILED)                      │
│  • result (WIN/LOSE/TIE/NONE)                               │
│  • profit, placed_at, closed_at                             │
├─────────────────────────────────────────────────────────────┤
│  Indexes: received_at, placed_at, status                    │
└─────────────────────────────────────────────────────────────┘
```

### 8. Risk Management

```
┌─────────────────────────────────────────────────────────────┐
│                    Risk Manager                             │
├─────────────────────────────────────────────────────────────┤
│  Checks:                                                    │
│  • Max trades per day                                       │
│  • Min signal confidence                                    │
│  • Minimum balance requirement                              │
│  • Max daily loss limit                                     │
├─────────────────────────────────────────────────────────────┤
│  Martingale (optional):                                     │
│  • Enabled by config                                        │
│  • Queues re-entry trades on loss                           │
│  • Doubles amount each level                                │
│  • Respects martingale_max level                            │
└─────────────────────────────────────────────────────────────┘
```

### 9. Worker Pool

```
┌─────────────────────────────────────────────────────────────┐
│                    Worker Pool                              │
├─────────────────────────────────────────────────────────────┤
│  • N concurrent workers (configurable)                      │
│  • Each worker:                                             │
│    - Pops signal from queue                                 │
│    - Waits for entry window                                 │
│    - Checks balance                                         │
│    - Places trade via WebSocket                             │
│    - Updates daily stats                                    │
│    - Saves trade to DB                                      │
├─────────────────────────────────────────────────────────────┤
│  Concurrency Control:                                       │
│  • Thread-safe signal queue                                 │
│  • Mutex-protected daily stats                              │
│  • Context-based shutdown                                   │
└─────────────────────────────────────────────────────────────┘
```

## Data Models (`pkg/models/`)

```
Signal:
├── ID (UUID)
├── Asset (string)
├── Direction (CALL/PUT)
├── Expiry (seconds)
├── Amount (float64)
├── Confidence (0-1)
├── EntryWindow (time.Time)
├── MartingaleLevels ([]MartingaleTime)
├── ReceivedAt, ProcessedAt
└── Raw (original message)

Trade:
├── ID (UUID)
├── SignalID (FK)
├── Asset, Direction, Amount, Expiry
├── Status (PENDING/OPEN/CLOSED/FAILED)
├── Result (WIN/LOSE/TIE/NONE)
├── Profit (float64)
├── PlacedAt, ClosedAt
└── ErrorMsg
```

## Configuration (`internal/config/config.go`)

```
Config:
├── Telegram (api_id, api_hash, phone, channel_id, session_file)
├── IQOption (email, password, demo_mode)
├── Trading (default_amount, max_concurrent_trades, min_balance, 
│           trade_delay_ms, max_daily_loss)
├── Risk (enabled, max_trades_per_day, min_signal_confidence,
│        martingale, martingale_max)
├── Analyzer (signal_threshold, interval_seconds, assets, etc.)
├── Logging (level, file, console)
└── Database (path)
```

## Command-Line Tools (`cmd/`)

```
bot/              - Main trading bot
backtest/         - Backtesting engine
calibrate/        - Parameter calibration
list-assets/      - List available IQ Option assets
signal-generator/ - Generate test signals
test-parser/      - Test signal parser
test-signal-gen/  - Test signal generator
```

## Key Dependencies

```
github.com/gotd/td          - Telegram MTProto client
github.com/gorilla/websocket - WebSocket client
github.com/mattn/go-sqlite3 - SQLite driver
github.com/rs/zerolog        - Structured logging
github.com/google/uuid       - UUID generation
gopkg.in/yaml.v3            - YAML config parsing
```

## Deployment Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Server Deployment                        │
├─────────────────────────────────────────────────────────────┤
│  • No browser required (headless)                           │
│  • WebSocket API (no Rod automation)                        │
│  • Session persistence (telegram.session)                   │
│  • SQLite database (local file)                             │
│  • Docker support (Dockerfile)                              │
│  • Graceful shutdown (SIGTERM handling)                     │
└─────────────────────────────────────────────────────────────┘
```

## Error Handling & Resilience

```
┌─────────────────────────────────────────────────────────────┐
│                    Resilience Features                      │
├─────────────────────────────────────────────────────────────┤
│  • Telegram auto-reconnect with exponential backoff         │
│  • Trade retry on profit rate change (3 attempts)           │
│  • Asset resolution with caching                            │
│  • Signal deduplication (3-min window)                      │
│  • Context-based graceful shutdown                          │
│  • Database transaction safety                              │
│  • Worker pool isolation (one failure doesn't stop others)  │
└─────────────────────────────────────────────────────────────┘
```

## Signal Lifecycle

```
1. RECEIVE: Telegram message arrives
2. PARSE: Extract signal parameters
3. DEDUPLICATE: Check recent signals cache
4. VALIDATE: Risk management checks
5. QUEUE: Add to signal queue
6. SCHEDULE: Worker picks up signal
7. WAIT: Sleep until entry window
8. EXECUTE: Place trade via WebSocket
9. TRACK: Monitor for result
10. RECORD: Save result to database
11. ANALYZE: Update stats, consider martingale
```

## Technology Stack

```
Language: Go 1.21
Database: SQLite
Protocol: WebSocket (IQ Option), MTProto (Telegram)
Config: YAML
Logging: zerolog (structured)
Deployment: Docker, systemd-ready
```
