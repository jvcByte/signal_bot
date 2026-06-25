# Signal Trading Bot

Automated trading bot that monitors Telegram channels for trading signals and executes trades on IQ Option via **WebSocket API** — no browser required, runs fully headless on any server.

## How It Works

```
Telegram Channel → Signal Parser → Risk Check → Entry Window Wait → IQ Option WebSocket → Trade
```

1. Monitors a Telegram channel for Mexy Binary signals
2. Parses the signal (asset, direction, expiry, confidence, entry window)
3. Runs risk management checks
4. Waits until the exact entry window time
5. Places a Blitz option trade directly via IQ Option's WebSocket API

## Quick Start

```bash
# 1. Clone and install
git clone https://github.com/jvcByte/signal_bot
cd signal_bot
make install

# 2. Configure
cp configs/config.example.yaml configs/config.yaml
# Edit configs/config.yaml with your credentials

# 3. Run
make run
```

**First run:** You'll be prompted to enter your Telegram verification code. Session is saved for future runs.

## Configuration

```yaml
telegram:
  api_id: 12345678          # From https://my.telegram.org
  api_hash: "your_hash"
  phone: "+1234567890"
  channel_id: -1003488226342

iqoption:
  email: "your@email.com"
  password: "your_password"
  demo_mode: true           # Always test on demo first!

trading:
  default_amount: 2.0       # Amount per trade
  max_concurrent_trades: 3

risk:
  enabled: true
  max_trades_per_hour: 30
  min_signal_confidence: 0.7
```

## Signal Format (Mexy Binary)

```
MEXY BINARY

🚨 TRADE NOW!!

📉  🇪🇺 EUR/USD 🇺🇸 (OTC)
🕒  Timeframe: 2-min expiry
🤖  AI Confidence: 90%
🕰️  Entry Window: 11:15 AM
Direction: 🔴 SELL

📊  Martingale Levels:
• Level 1  →  11:17 AM
• Level 2  →  11:19 AM
```

The bot waits until the entry window time before placing the trade.

## Branches

| Branch | Approach | Use Case |
|--------|----------|----------|
| `main` | Browser automation (Rod) | Local machine with display |
| `websocket-api` | WebSocket API | Server/headless deployment |

## Server Deployment

Since this branch uses no browser, it runs anywhere:

```bash
# On any Linux server
git clone -b websocket-api https://github.com/jvcByte/signal_bot
cd signal_bot
make install

# Copy your session files (to avoid re-authentication)
scp session/telegram.session user@server:~/signal_bot/session/

# Configure and run
cp configs/config.example.yaml configs/config.yaml
nano configs/config.yaml
make run
```

Or with Docker:
```bash
docker-compose up -d
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Signal Formats](docs/MEXY_SIGNALS.md)
- [Session Management](docs/SESSION_MANAGEMENT.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)

## Project Structure

```
signal-bot/
├── cmd/
│   ├── bot/              # Main entry point
│   └── test-parser/      # Signal parser testing
├── internal/
│   ├── bot/              # Orchestration, risk management, worker pool
│   ├── config/           # YAML config loading
│   ├── database/         # SQLite trade/signal storage
│   ├── parser/           # Mexy signal parser
│   ├── queue/            # Thread-safe signal queue
│   ├── telegram/         # Telegram MTProto client
│   └── wstrader/         # IQ Option WebSocket trader
├── pkg/
│   └── models/           # Signal, Trade data models
└── configs/
    └── config.example.yaml
```
