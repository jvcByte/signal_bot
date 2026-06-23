# Troubleshooting Guide

## Quick Diagnostics

```bash
# Check if bot is running
ps aux | grep signal-bot

# View recent logs
tail -n 50 logs/bot.log

# Check database
sqlite3 data/trades.db "SELECT COUNT(*) FROM signals;"
sqlite3 data/trades.db "SELECT COUNT(*) FROM trades;"

# Test parser
make test-parser
```

## Common Issues

### 1. Bot Won't Start

#### Error: "Failed to load config"
```
ERROR: Failed to load config: open configs/config.yaml: no such file
```

**Cause:** Config file missing

**Solution:**
```bash
cp configs/config.example.yaml configs/config.yaml
nano configs/config.yaml
# Fill in your credentials
```

#### Error: "telegram.api_id is required"
```
ERROR: validate config: telegram.api_id is required
```

**Cause:** Config file incomplete

**Solution:**
1. Go to https://my.telegram.org
2. Get `api_id` and `api_hash`
3. Update config:
```yaml
telegram:
  api_id: 12345678
  api_hash: "abcdef1234567890"
```

---

### 2. Telegram Issues

#### Error: "Failed to connect to Telegram"
```
ERROR: telegram connection failed: dial tcp: lookup api.telegram.org: no such host
```

**Cause:** Network connectivity issue

**Solution:**
```bash
# Test connectivity
ping -c 3 api.telegram.org

# Check DNS
nslookup api.telegram.org

# If blocked, use VPN or proxy
```

#### Error: "Phone number verification failed"
```
ERROR: authenticate: invalid code
```

**Cause:** Wrong verification code

**Solution:**
1. Check Telegram app for 5-digit code
2. Enter exactly as shown (no spaces)
3. Code expires in 5 minutes - be quick

#### Error: "Session file corrupted"
```
ERROR: failed to decode session
```

**Cause:** Corrupted session file

**Solution:**
```bash
rm -rf session/*.session
make run
# Re-authenticate from scratch
```

#### Issue: "No messages received"
```
INFO: listening for signals
... (silence)
```

**Cause:** Wrong channel ID or not in channel

**Solution:**
```bash
# Verify you're in the channel
# Check web.telegram.org/a/#-1003488226342

# Get correct channel ID:
# Method 1: Forward message to @userinfobot
# Method 2: Use web URL (number after #)

# Update config
telegram:
  channel_id: -1003488226342  # Must be negative for supergroups
```

---

### 3. Parsing Issues

#### Issue: "Failed to parse signal"
```
DEBUG: failed to parse signal: no pattern matched
DEBUG: raw message: "🚀 New trade alert..."
```

**Cause:** Signal format not recognized

**Solution:**
```bash
# Copy the raw message
# Test with parser utility
echo 'AUD/USD (OTC)
Timeframe: 2-min expiry
Direction: BUY' | go run cmd/test-parser/main.go

# If it fails, add new pattern in internal/parser/parser.go
```

#### Issue: "Asset name incorrect"
```
INFO: parsed signal asset=AUDUS direction=CALL
```

**Cause:** Asset normalization issue

**Solution:**
Check `normalizeAsset()` function in `parser.go`:
```go
func normalizeAsset(asset string) string {
    asset = strings.ReplaceAll(asset, "(OTC)", "")
    asset = strings.TrimSpace(asset)
    asset = strings.ReplaceAll(asset, "/", "")
    asset = strings.ToUpper(asset)
    return asset
}
```

---

### 4. IQ Options Issues

#### Error: "Login failed"
```
ERROR: trader connect: login failed: check credentials or 2FA
```

**Causes & Solutions:**

**Wrong credentials:**
```bash
# Verify login manually
# Go to https://iqoption.com
# Use exact same email/password
```

**2FA enabled:**
```yaml
# Run with visible browser
iqoption:
  headless: false

# Handle 2FA manually when browser opens
# Consider disabling 2FA for bot account
```

**Captcha:**
```
# IQ Options showing captcha
# Solution: Use stealth mode (already enabled)
# Add delays, use residential proxy
```

#### Error: "Element not found"
```
ERROR: failed to place trade: set amount: find amount input: element not found
```

**Cause:** IQ Option uses **Canvas-based UI** (not HTML elements)

**Solution:**
This is expected! IQ Option's traderoom is rendered on HTML Canvas, not with standard HTML buttons/inputs.

**You must use coordinate-based clicking:**

1. **Run calibration tool:**
   ```bash
   make calibrate
   ```

2. **Screenshot saved** to `calibration_screenshot.png`

3. **Open in image editor** and hover over buttons to find coordinates

4. **Update config.yaml** with actual coordinates:
   ```yaml
   iqoption:
     coordinates:
       asset_x: 150   # Your actual coordinates
       asset_y: 50
       call_x: 500
       call_y: 650
       put_x: 780
       put_y: 650
       # etc...
   ```

5. **See [Canvas UI Guide](CANVAS_UI.md) for detailed instructions**

#### Error: "coordinates not configured"
```
ERROR: CALL button coordinates not configured in config.yaml
```

**Cause:** Coordinates missing or set to 0

**Solution:**
All coordinate values must be set:
- asset_x, asset_y
- expiry_x, expiry_y
- amount_x, amount_y
- call_x, call_y
- put_x, put_y

Run `make calibrate` to find correct values.

#### Issue: "Demo mode not switching"
```
INFO: switching to demo account
WARN: failed to switch to demo mode: element not found
```

**Cause:** Demo switcher selector incorrect

**Solution:**
```go
// In trader.go, update switchToDemo()
// Try different selectors:
switcher := page.Element(`[aria-label="Practice"]`)
// or
switcher := page.Element(`button:contains("Practice")`)
// or
switcher := page.Element(`#account-switcher`)
```

Manual workaround:
```yaml
# Switch to demo manually before starting bot
# Then set:
iqoption:
  skip_demo_switch: true  # (add this config option)
```

#### Error: "Browser crashed"
```
ERROR: rod: browser process exited
```

**Cause:** Chrome crash, memory issue, or system resource exhaustion

**Solution:**
```bash
# Check system resources
free -h
df -h

# Reduce concurrent trades
trading:
  max_concurrent_trades: 1

# Add browser args in trader.go:
l := launcher.New().
    Headless(t.cfg.Headless).
    Set("disable-dev-shm-usage").
    Set("no-sandbox")
```

---

### 5. Trade Execution Issues

#### Issue: "Trades pending forever"
```
sqlite3 data/trades.db "SELECT * FROM trades WHERE status='PENDING';"
# Shows many pending trades
```

**Cause:** Workers stuck or not processing

**Solution:**
```bash
# Check logs for worker activity
grep "trade worker started" logs/bot.log
grep "placing trade" logs/bot.log

# Restart bot
pkill signal-bot
make run
```

#### Issue: "Balance too low"
```
WARN: balance too low balance=0
```

**Cause:** Demo account needs funding

**Solution:**
1. Login to IQ Options manually
2. Go to Practice Account
3. Click "Replenish" or "Add Funds"
4. Demo accounts usually auto-refill

#### Issue: "Trades click wrong location"
```
INFO: clicking direction button
INFO: trade executed!
# But nothing happened in browser, or clicked wrong spot
```

**Cause:** Coordinates not calibrated for your screen resolution

**Solution:**
```bash
# Recalibrate coordinates
make calibrate

# Take new screenshot
# Measure exact pixel positions
# Update config.yaml

# Test with single trade
# Adjust coordinates by 10-20 pixels if needed
```

**Tips:**
- Default coordinates assume 1280x800 resolution
- Different resolution = different coordinates
- Keep browser window at consistent size
- Round to nearest 5-10 pixels for margin

#### Issue: "Asset not found"
```
ERROR: failed to place trade: select asset: find asset in list: element not found
```

**Cause:** Asset not available or wrong name

**Solution:**
```bash
# Check IQ Options for available assets
# Some pairs only available at certain hours
# OTC pairs have different availability

# Map asset names in trader.go:
assetMap := map[string]string{
    "AUDUSD": "AUD/USD (OTC)",
    "EURUSD": "EUR/USD",
}
```

---

### 6. Risk Management Issues

#### Issue: "Signals rejected"
```
WARN: signal rejected by risk management
```

**Cause:** Risk limits triggered

**Check which limit:**
```bash
# Look for specific messages
grep "daily loss limit reached" logs/bot.log
grep "hourly trade limit reached" logs/bot.log
grep "signal confidence too low" logs/bot.log
```

**Solutions:**

Daily loss limit:
```yaml
trading:
  max_daily_loss: 100.0  # Increase if needed
```

Hourly trade limit:
```yaml
risk:
  max_trades_per_hour: 20  # Increase if needed
```

Low confidence:
```yaml
risk:
  min_signal_confidence: 0.6  # Lower threshold (0.7 default)
```

---

### 7. Database Issues

#### Error: "Database locked"
```
ERROR: failed to save trade: database is locked
```

**Cause:** Concurrent access issue

**Solution:**
```bash
# Check for other processes
lsof data/trades.db

# Kill if any
# Then restart bot

# Or enable WAL mode (already should be enabled)
sqlite3 data/trades.db "PRAGMA journal_mode=WAL;"
```

#### Error: "Disk full"
```
ERROR: failed to save signal: no space left on device
```

**Solution:**
```bash
# Check disk space
df -h

# Clean old logs
rm logs/*.log.old

# Vacuum database
sqlite3 data/trades.db "VACUUM;"
```

---

### 8. Performance Issues

#### Issue: "High memory usage"
```bash
top -p $(pgrep signal-bot)
# Shows >500MB memory
```

**Cause:** Memory leak or browser accumulation

**Solution:**
```bash
# Restart bot periodically (cron job)
# Or investigate with pprof:
import _ "net/http/pprof"
go tool pprof http://localhost:6060/debug/pprof/heap
```

#### Issue: "Slow trade execution"
```
INFO: placing trade
... (10 seconds later)
INFO: trade placed successfully
```

**Cause:** Browser operations slow, network latency

**Solution:**
```yaml
# Reduce delays
trading:
  trade_delay_ms: 1000  # From 2000

# Or check network
ping iqoption.com
```

---

### 9. Logging Issues

#### Issue: "No logs appearing"
```bash
tail -f logs/bot.log
# Empty or no file
```

**Solution:**
```bash
# Check config
logging:
  level: "info"  # Not "silent"
  file: "logs/bot.log"
  console: true

# Create log directory
mkdir -p logs

# Check permissions
ls -la logs/
```

#### Issue: "Logs too verbose"
```
DEBUG: ... (thousands of lines)
```

**Solution:**
```yaml
logging:
  level: "info"  # Change from "debug"
```

---

### 10. Docker Issues

#### Error: "Chrome not found in container"
```
ERROR: failed to launch browser: exec: "chromium": executable file not found
```

**Solution:**
```dockerfile
# In Dockerfile, ensure chromium installed
RUN apk add --no-cache chromium

# Set path
ENV CHROME_BIN=/usr/bin/chromium-browser
```

#### Issue: "Can't access config file"
```
ERROR: open /app/configs/config.yaml: no such file
```

**Solution:**
```bash
# Mount config directory
docker run -v $(PWD)/configs:/app/configs signal-bot

# Or copy into image during build
COPY configs/config.yaml /app/configs/
```

---

## Debug Mode

### Enable Verbose Logging

```yaml
logging:
  level: "debug"
  console: true
```

### Watch Browser Actions

```yaml
iqoption:
  headless: false
```

### Slow Down Execution

```yaml
trading:
  trade_delay_ms: 10000  # 10 seconds between trades
```

### Test Single Trade

```bash
# Reduce workers to 1
trading:
  max_concurrent_trades: 1

# Set low limits
risk:
  max_trades_per_hour: 5
```

---

## Getting Stack Traces

### Enable Panic Recovery

```go
// In main.go
defer func() {
    if r := recover(); r != nil {
        log.Error().Interface("panic", r).Msg("recovered from panic")
        debug.PrintStack()
    }
}()
```

### Send SIGQUIT for Stack Dump

```bash
kill -QUIT $(pgrep signal-bot)
# Check logs for goroutine dump
```

---

## Health Check Script

```bash
#!/bin/bash
# healthcheck.sh

# Check process
if ! pgrep signal-bot > /dev/null; then
    echo "ERROR: Bot not running"
    exit 1
fi

# Check recent activity (signals in last 10 min)
recent=$(sqlite3 data/trades.db "SELECT COUNT(*) FROM signals WHERE received_at > datetime('now', '-10 minutes');")
if [ "$recent" -eq 0 ]; then
    echo "WARN: No signals in last 10 minutes"
fi

# Check trade failures
failures=$(sqlite3 data/trades.db "SELECT COUNT(*) FROM trades WHERE status='FAILED' AND placed_at > datetime('now', '-1 hour');")
if [ "$failures" -gt 5 ]; then
    echo "WARN: High failure rate: $failures in last hour"
fi

# Check daily loss
loss=$(sqlite3 data/trades.db "SELECT SUM(profit) FROM trades WHERE DATE(placed_at) = DATE('now') AND profit < 0;")
if (( $(echo "$loss < -50" | bc -l) )); then
    echo "ALERT: Daily loss limit approaching: $loss"
fi

echo "OK: Bot healthy"
```

---

## When All Else Fails

### Nuclear Option: Clean Restart

```bash
# Stop bot
pkill signal-bot

# Backup data
cp -r data data.backup
cp -r logs logs.backup

# Clean everything
make clean

# Fresh install
make install

# Rebuild
make build

# Fresh config
cp configs/config.example.yaml configs/config.yaml
# Edit config...

# Start with debug
make run
```

### Ask for Help

Before reporting issues, collect:
```bash
# System info
uname -a
go version

# Recent logs
tail -n 100 logs/bot.log > debug.log

# Config (remove secrets!)
cat configs/config.yaml | grep -v "api_hash\|password" > config.safe.yaml

# Database stats
sqlite3 data/trades.db ".schema" > schema.txt
sqlite3 data/trades.db "SELECT status, COUNT(*) FROM trades GROUP BY status;" > stats.txt
```

Include in issue:
- What you were trying to do
- What happened (error message)
- What you expected
- Steps to reproduce
- Relevant logs
- System info
