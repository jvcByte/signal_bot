# Signal Trading Bot

Automated trading bot that monitors Telegram channels for trading signals and executes trades on IQ Options platform.

## Features

- 📨 **Telegram Integration**: Monitors channels for trading signals with session persistence
- 🤖 **Signal Parsing**: Supports multiple signal formats including Mexy Binary with emoji handling
- 💹 **IQ Options Automation**: Browser automation with cookie-based session persistence
- ⚖️ **Risk Management**: Daily loss limits, hourly trade limits, confidence thresholds
- 🔄 **Concurrent Execution**: Worker pool for parallel trade processing
- 💾 **SQLite Database**: Persistent storage for signals and trades
- 📊 **Comprehensive Logging**: Detailed logs with emojis for better readability

## Quick Start

### 1. Install Dependencies
```bash
make install
```

### 2. Configure Your Credentials
```bash
cp configs/config.example.yaml configs/config.yaml
# Edit configs/config.yaml with your Telegram API credentials and IQ Options login
```

### 3. **IMPORTANT: Calibrate Coordinates**

IQ Option uses a **Canvas-based UI** (not HTML elements), so you must calibrate click coordinates:

```bash
make calibrate
```

This will:
- Open IQ Option in browser
- Take a screenshot (`calibration_screenshot.png`)
- Show instructions for finding coordinates

Open the screenshot in an image editor and hover over:
- Asset selector button (top-left)
- Expiry selector button (top area)
- Amount input field (center)
- CALL button (bottom, green)
- PUT button (bottom, red)

Update `configs/config.yaml` with the pixel coordinates you find.

**See [Canvas UI Guide](docs/CANVAS_UI.md) for detailed instructions.**

### 4. Run the Bot
```bash
make run
```

**First run**: Enter your Telegram verification code when prompted. Session is saved for future runs.

## Configuration

Edit `configs/config.yaml`:

```yaml
telegram:
  api_id: YOUR_API_ID          # Get from https://my.telegram.org
  api_hash: "YOUR_API_HASH"
  phone: "+1234567890"
  channel_id: -1001234567890   # Your signal channel ID

iqoption:
  email: "your@email.com"
  password: "your_password"
  demo_mode: true              # Start with demo mode!
  headless: false              # Keep false to see browser during calibration
  
  # Canvas UI Coordinates - MUST CALIBRATE! Run: make calibrate
  coordinates:
    asset_x: 150    # Asset selector button
    asset_y: 50
    expiry_x: 300   # Expiry selector button
    expiry_y: 100
    amount_x: 640   # Amount input field
    amount_y: 400
    call_x: 500     # CALL/BUY button (green)
    call_y: 650
    put_x: 780      # PUT/SELL button (red)
    put_y: 650

trading:
  default_amount: 10.0
  max_concurrent_trades: 3
  min_balance: 100.0

risk:
  enabled: true
  max_trades_per_hour: 10
  min_signal_confidence: 0.7
```

**Important:** Default coordinates are placeholders. You must calibrate them for your screen resolution.

## Documentation

### Core Documentation
- [Canvas UI Guide](docs/CANVAS_UI.md) - **START HERE** - Coordinate calibration
- [Architecture](docs/ARCHITECTURE.md) - System design and components
- [Troubleshooting](docs/TROUBLESHOOTING.md) - Common issues and solutions
- [Session Management](docs/SESSION_MANAGEMENT.md) - How sessions work
- [Signal Formats](docs/MEXY_SIGNALS.md) - Supported signal formats

### Project Information
- [Accomplishments](docs/project-info/ACCOMPLISHMENTS.md) - What's been built
- [Quick Start Guide](docs/project-info/QUICKSTART.md) - Detailed setup guide
- [Run Instructions](docs/project-info/RUN.md) - How to run the bot
- [Tasks](docs/project-info/TASKS.md) - Development tasks and roadmap

## Project Structure

```
signal-bot/
├── cmd/
│   ├── bot/           # Main bot executable
│   ├── calibrate/     # Coordinate calibration tool
│   └── test-parser/   # Signal parser testing tool
├── internal/
│   ├── bot/           # Bot orchestration and worker pool
│   ├── config/        # Configuration management
│   ├── database/      # SQLite data layer
│   ├── parser/        # Signal parsers (Mexy + legacy formats)
│   ├── queue/         # Thread-safe signal queue
│   ├── telegram/      # Telegram client with polling
│   └── trader/        # IQ Options browser automation
├── pkg/
│   └── models/        # Data models (Signal, Trade)
├── configs/
│   └── config.example.yaml
├── docs/              # Documentation
├── session/           # Session files (gitignored)
├── data/              # SQLite database (gitignored)
└── logs/              # Log files (gitignored)
```

## Building

```bash
# Build binary
make build

# Run tests
make test

# Clean build artifacts
make clean
```

## Docker

```bash
# Build image
docker build -t signal-bot .

# Run container
docker run -v $(pwd)/configs:/app/configs \
           -v $(pwd)/session:/app/session \
           -v $(pwd)/data:/app/data \
           signal-bot
```

## Status

✅ **Working**:
- Telegram connection and message polling
- Signal parsing (Mexy Binary format with emojis)
- IQ Options session management (~2s login)
- Risk management and validation
- Database storage
- Worker pool and queue system

⚠️ **In Progress**:
- IQ Options UI selectors (trade execution interface)
- Balance reading optimization

## Requirements

- Go 1.20+
- Chrome/Chromium browser (for IQ Options automation)
- Telegram account with API credentials
- IQ Options account

## License

MIT License - See LICENSE file for details

## Security Notes

- Never commit `configs/config.yaml` (contains credentials)
- Session files are gitignored for security
- Use demo mode first before real money trading
- Review all trades in logs before enabling real mode

## Support

For issues and questions, see [Troubleshooting](docs/TROUBLESHOOTING.md) or open an issue on GitHub.
