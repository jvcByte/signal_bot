# Mexy Binary Signal Format

## Example Signal

```
Mexy Binary
TRADE NOW!!

AUD/USD (OTC)
Timeframe: 2-min expiry
AI Confidence: 80%

Entry Window: 11:08 PM
Direction: BUY

Martingale Levels:
• Level 1 → 11:10 PM
• Level 2 → 11:12 PM
• Level 3 → 11:14 PM
```

## Parsed Fields

| Field | Example | Description |
|-------|---------|-------------|
| Asset | `AUD/USD (OTC)` | Currency pair, normalized to `AUDUSD` |
| Timeframe | `2-min expiry` | Trade expiry in minutes |
| AI Confidence | `80%` | Signal confidence (0-100%) |
| Direction | `BUY` or `SELL` | Mapped to CALL/PUT |
| Entry Window | `11:08 PM` | Optimal entry time |
| Martingale Levels | `Level 1 → 11:10 PM` | Follow-up trades if first loses |

## Parser Logic

### Basic Pattern
- Regex: `([A-Z]{3}/[A-Z]{3}).*?TIMEFRAME:\s*(\d+)-MIN.*?AI\s*CONFIDENCE:\s*(\d+)%.*?DIRECTION:\s*(BUY|SELL)`
- Case-insensitive matching
- Handles multi-line format with `(?s)` flag

### Asset Normalization
- Removes `(OTC)` suffix
- Strips `/` separator
- Converts to uppercase: `AUD/USD (OTC)` → `AUDUSD`

### Direction Mapping
- `BUY` → `CALL` (expecting price to go up)
- `SELL` → `PUT` (expecting price to go down)

### Confidence Score
- Extracted as percentage (80% → 0.8)
- Used for risk management filtering
- Signals below threshold (default 0.7) are rejected

## Entry Window Handling

The parser extracts entry window times but execution timing depends on configuration:

### Immediate Execution (Default)
```yaml
trading:
  respect_entry_window: false
```
Trades execute as soon as signal is received.

### Window-Based Execution
```yaml
trading:
  respect_entry_window: true
  entry_window_tolerance: 120  # seconds
```
Trades wait until entry window (±2 minutes).

## Martingale Strategy

Martingale levels are parsed but **NOT automatically executed** by default.

### What is Martingale?
- Double bet after loss to recover losses
- High risk: can drain account quickly
- Level 1: 2x initial amount
- Level 2: 4x initial amount
- Level 3: 8x initial amount

### Example
- Initial trade: $10 (loses)
- Level 1 (11:10 PM): $20 (loses)
- Level 2 (11:12 PM): $40 (loses)
- Level 3 (11:14 PM): $80 (wins)
- Net: -$10 -$20 -$40 +$80 = +$10

### Enabling Martingale (Advanced)
```yaml
trading:
  martingale_enabled: false  # Keep disabled!
  martingale_max_level: 2
  martingale_multiplier: 2.0
```

**WARNING**: Martingale can lead to catastrophic losses. Only enable on demo with strict limits.

## Signal Validation

Before executing, the bot checks:

1. **Confidence threshold**: `signal.Confidence >= config.Risk.MinSignalConfidence`
2. **Balance check**: `balance >= config.Trading.MinBalance`
3. **Daily loss limit**: `dailyLoss < config.Trading.MaxDailyLoss`
4. **Trade rate limit**: `tradesPerHour < config.Risk.MaxTradesPerHour`
5. **Entry window** (if enabled): `now within entry_window ± tolerance`

## Testing

Run parser tests:
```bash
make test-parser
```

Test with live signal:
```bash
echo 'AUD/USD (OTC)
Timeframe: 2-min expiry
AI Confidence: 80%
Direction: BUY' | go run cmd/test-parser/main.go
```

## Common Issues

### Signal Not Parsed
- Check format matches regex exactly
- Verify timeframe has `-min` suffix
- Ensure direction is BUY or SELL (not CALL/PUT)
- Check logs for parser errors

### Wrong Asset Selected
- IQ Options may not have the exact pair
- OTC pairs have different availability hours
- Check asset mapping in `trader.go`

### Confidence Too Low
- Adjust `min_signal_confidence` in config
- Default is 0.7 (70%)
- Mexy signals usually 75-85%

## Advanced: Custom Parser

To handle variations in Mexy format:

```go
// internal/parser/mexy_custom.go
func ParseMexyVariant(text string) (*models.Signal, error) {
    // Custom logic for different format
}
```

Register in parser.go:
```go
patterns: []*Pattern{
    newMexyPattern(),
    newMexyVariantPattern(),
    // ...
}
```
