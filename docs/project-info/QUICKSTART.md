# Quick Start Guide

## 1. Get Telegram API Credentials

1. Go to https://my.telegram.org
2. Login with your phone number
3. Click "API development tools"
4. Create an app to get:
   - `api_id` (number)
   - `api_hash` (string)

## 2. Get Channel ID

### Method 1: Using Web Telegram
1. Open https://web.telegram.org/a/#-1003488226342
2. The number after `#` is the channel ID: `-1003488226342`

### Method 2: Using Bot
1. Forward a message from the channel to [@userinfobot](https://t.me/userinfobot)
2. Bot will show the channel ID

## 3. Setup Configuration

```bash
# Copy example config
cp configs/config.example.yaml configs/config.yaml

# Edit with your credentials
nano configs/config.yaml
```

**Critical settings:**
```yaml
telegram:
  api_id: YOUR_API_ID
  api_hash: "YOUR_API_HASH"
  phone: "+1234567890"
  channel_id: -1003488226342

iqoption:
  email: "your@email.com"
  password: "your_password"
  demo_mode: true        # Keep true!
  headless: false        # Set false for first run

trading:
  default_amount: 10.0   # Start small
  max_daily_loss: 50.0   # Set strict limit

risk:
  enabled: true
  min_signal_confidence: 0.7  # Mexy signals are usually 75-85%
```

## 4. Install & Run

```bash
# Install dependencies
make install

# Test parser first
make test-parser

# Run bot
make run
```

### First Run Checklist

1. **Telegram Auth**
   - Bot will prompt for verification code
   - Check your Telegram app for code
   - Enter code in terminal
   - Session is saved for future runs

2. **IQ Options Login**
   - Browser window opens (if `headless: false`)
   - Bot enters email/password
   - **If 2FA enabled**: Handle manually in browser
   - Bot switches to demo account
   - Browser stays open for trading

3. **Signal Reception**
   - Bot listens to the Telegram channel
   - When signal arrives, logs appear:
     ```
     INFO received message
     INFO parsed signal asset=AUDUSD direction=CALL expiry=2
     INFO placing trade
     INFO trade placed successfully
     ```

4. **Verify in IQ Options**
   - Check traderoom for active trades
   - Verify demo balance is being used
   - Watch first few trades manually

## 5. Monitoring

### Live Logs
```bash
# Watch logs in real-time
tail -f logs/bot.log

# Pretty print JSON logs
tail -f logs/bot.log | jq .
```

### Check Database
```bash
# Recent signals
sqlite3 data/trades.db "SELECT * FROM signals ORDER BY received_at DESC LIMIT 5;"

# Recent trades
sqlite3 data/trades.db "SELECT asset, direction, amount, status FROM trades ORDER BY placed_at DESC LIMIT 10;"

# Win/loss stats
sqlite3 data/trades.db "
SELECT 
  COUNT(*) as total,
  SUM(CASE WHEN result = 'WIN' THEN 1 ELSE 0 END) as wins,
  ROUND(100.0 * SUM(CASE WHEN result = 'WIN' THEN 1 ELSE 0 END) / COUNT(*), 2) as win_rate
FROM trades WHERE status = 'CLOSED';
"
```

## 6. Troubleshooting

### Telegram Not Connecting
```bash
# Delete session and re-auth
rm -rf session/*.session
make run
```

### IQ Options Login Fails
- Disable 2FA temporarily in IQ Options settings
- Or: Handle 2FA manually when browser opens
- Check credentials in config
- Run with `headless: false` to see what's happening

### Signals Not Parsing
```bash
# Test parser with real signal
go run cmd/test-parser/main.go

# Check logs for parse errors
grep "failed to parse" logs/bot.log
```

### Trades Not Executing
- Check `demo_mode: true` is set
- Verify balance > `min_balance` (default 100)
- Check `daily_loss < max_daily_loss`
- Look for error logs in trade execution

### Browser Crashes
```bash
# Reduce concurrent trades
trading:
  max_concurrent_trades: 1

# Add more delay between trades
trading:
  trade_delay_ms: 5000
```

## 7. Production Checklist

**DO NOT run on real account until:**

- [ ] 100+ trades on demo account
- [ ] Win rate > 55%
- [ ] No crashes or errors for 7+ days
- [ ] Strict risk limits tested
- [ ] Daily loss limit tested (trigger it intentionally)
- [ ] Network disconnection handling verified
- [ ] Manual monitoring in place

**When ready for real account:**

1. Start with minimum amounts
2. Set conservative limits:
   ```yaml
   trading:
     default_amount: 5.0
     max_daily_loss: 25.0
   risk:
     max_trades_per_hour: 5
   ```
3. Monitor 24/7 for first week
4. Never leave unattended
5. Have kill switch ready

## 8. Common Commands

```bash
# Start bot
make run

# Run in background (Linux)
nohup make run > output.log 2>&1 &

# Stop bot (Ctrl+C)
# Or kill process
pkill signal-bot

# View stats
sqlite3 data/trades.db < queries/stats.sql

# Backup database
cp data/trades.db data/trades.backup.db

# Test parser
make test-mexy

# Run all tests
make test

# Clean everything
make clean
```

## 9. Next Steps

- Read [docs/MEXY_SIGNALS.md](docs/MEXY_SIGNALS.md) for signal format details
- Check [TASKS.md](TASKS.md) for development roadmap
- Review [README.md](README.md) for full documentation
- Refine IQ Options selectors (see `internal/trader/trader.go`)
- Add monitoring/alerting
- Implement result tracking

## 10. Getting Help

**Before asking:**
1. Check logs: `tail -f logs/bot.log`
2. Run tests: `make test`
3. Verify config: `cat configs/config.yaml`
4. Check database: `sqlite3 data/trades.db .tables`

**Common Issues:**
- **"Failed to parse signal"**: Signal format changed, update parser
- **"Login failed"**: Wrong credentials or 2FA enabled
- **"Balance too low"**: Increase demo balance in IQ Options
- **"Element not found"**: IQ Options UI changed, update selectors

**Debug Mode:**
```yaml
logging:
  level: "debug"  # More verbose logs
  console: true

iqoption:
  headless: false  # See browser actions
```

## Safety Reminder

- Demo account only until proven profitable
- IQ Options prohibits bots (risk of ban)
- Telegram signals are often unreliable
- Never risk more than you can afford to lose
- This is educational software, not financial advice
