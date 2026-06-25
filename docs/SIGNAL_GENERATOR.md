# AI Signal Generator

Autonomous signal generator that analyzes IQ Option market data and posts trading signals to Telegram.

## Overview

The signal generator:
1. Connects to IQ Option WebSocket API for real-time market data
2. Analyzes price movements using technical indicators
3. Generates signals when favorable conditions are detected
4. Posts signals to Telegram channel in Mexy format
5. Your trading bot picks up these signals and executes trades

## Features

### Technical Analysis
- **RSI (Relative Strength Index)**: Detects oversold/overbought conditions
- **Moving Averages**: Identifies trend direction and crossovers
- **Bollinger Bands**: Measures volatility and extremes
- **MACD**: Momentum and trend strength
- **Stochastic Oscillator**: Momentum indicator
- **ATR (Average True Range)**: Volatility measurement

### Signal Generation Strategies

**Strategy 1: RSI Extremes**
- RSI < 30 (oversold) → BUY signal (confidence: 70%+)
- RSI > 70 (overbought) → SELL signal (confidence: 70%+)

**Strategy 2: Moving Average Crossover**
- Fast MA crosses above slow MA → BUY signal (confidence: 75%)
- Fast MA crosses below slow MA → SELL signal (confidence: 75%)

**Strategy 3: Combined Signals** (highest confidence)
- RSI oversold + bullish trend → BUY (confidence: 85%)
- RSI overbought + bearish trend → SELL (confidence: 85%)

### Auto-Martingale
Generated signals include 3 martingale recovery levels:
- Level 1: +2 minutes from entry
- Level 2: +4 minutes from entry  
- Level 3: +6 minutes from entry

## Usage

### Run Signal Generator

```bash
# Basic usage (analyzes EURUSD, GBPUSD, AUDUSD, USDJPY every 60 seconds)
go run cmd/signal-generator/main.go

# Custom interval (analyze every 2 minutes)
go run cmd/signal-generator/main.go -interval 120

# Custom assets
go run cmd/signal-generator/main.go -assets "OPENAI,ANTHROPIC,BITCOIN"

# All options
go run cmd/signal-generator/main.go \
  -config configs/config.yaml \
  -interval 60 \
  -assets "EURUSD,GBPUSD,AUDUSD,USDJPY,OPENAI"
```

### Configuration

The generator uses the same `configs/config.yaml` as the trading bot:

```yaml
telegram:
  api_id: 12345678
  api_hash: "your_hash"
  phone: "+1234567890"
  channel_id: -1003488226342  # Channel where signals will be posted
  session_file: "session/telegram.session"

iqoption:
  email: "your@email.com"
  password: "your_password"
  demo_mode: true  # Generator only reads data, doesn't trade
```

### Full Automation Flow

1. **Start signal generator** (posts signals to Telegram):
```bash
go run cmd/signal-generator/main.go -interval 60
```

2. **Start trading bot** (reads signals from Telegram and trades):
```bash
make run
```

Now you have a fully automated system:
- Generator analyzes → Posts to Telegram → Bot reads → Executes trades

## Example Signal Output

```
MEXY BINARY

🚨 TRADE NOW!!

📊 EURUSD (OTC)
🕒 Timeframe: 2-min expiry
🤖 AI Confidence: 85%
🕰️ Entry Window: 12:30 PM
Direction: 🟢 BUY

📊 Martingale Levels:
• Level 1 → 12:32 PM
• Level 2 → 12:34 PM
• Level 3 → 12:36 PM
```

## Customizing Strategies

Edit `internal/analyzer/analyzer.go` to modify strategy parameters:

```go
// Default configuration
config := analyzer.AnalyzerConfig{
    RSIPeriod:        14,     // RSI calculation period
    RSIOversold:      30,     // Buy threshold
    RSIOverbought:    70,     // Sell threshold
    FastMAPeriod:     10,     // Fast moving average
    SlowMAPeriod:     20,     // Slow moving average
    MinConfidence:    0.65,   // Min confidence to generate signal
    ExpiryMinutes:    2,      // Trade expiry
    EnableMartingale: true,   // Include martingale levels
}
```

## Adding New Indicators

### Example: Add Bollinger Bands Strategy

1. Calculate Bollinger Bands in `AnalyzeAsset()`:
```go
middle, upper, lower := indicators.BollingerBands(closes, 20, 2.0)
```

2. Add strategy logic:
```go
// Price touched lower band = oversold
if currentPrice <= lower {
    direction = models.DirectionCall
    confidence = 0.80
    reason = "Price at lower Bollinger Band"
}

// Price touched upper band = overbought
if currentPrice >= upper {
    direction = models.DirectionPut
    confidence = 0.80
    reason = "Price at upper Bollinger Band"
}
```

## Backtesting

Test strategies on historical data before going live:

```go
// Fetch 500 historical candles
candles, _ := trader.GetHistoricalCandles("EURUSD", 60, 500)

// Simulate signals on historical data
for i := 50; i < len(candles); i++ {
    signal, _ := analyzer.AnalyzeAsset("EURUSD")
    // Track win/loss, calculate metrics
}
```

## Performance Monitoring

Track signal performance metrics:
- Win rate by asset
- Average confidence vs actual win rate
- Best performing strategies
- Optimal timeframes

## Safety Notes

⚠️ **Always test new strategies on demo mode first!**

- Start with low confidence thresholds
- Monitor signal quality for 1-2 days
- Adjust parameters based on results
- Never risk more than you can afford to lose

## Troubleshooting

**No signals generated:**
- Check if assets have recent price data
- Lower `MinConfidence` threshold
- Verify IQ Option connection
- Check indicator periods (need enough candles)

**Too many signals:**
- Increase `MinConfidence` threshold
- Add more restrictive strategy conditions
- Increase analysis interval

**Signals not posting to Telegram:**
- Verify channel_id is correct
- Check bot has write access to channel
- Ensure Telegram session is authenticated

## Next Steps

- Add machine learning models for better predictions
- Implement multi-timeframe analysis
- Add pattern recognition (head & shoulders, double top, etc.)
- Create dashboard for monitoring signal performance
- Add risk management (max signals per day, stop after X losses)
