# How to Run the Bot

## First Time Setup

1. **Get Telegram API credentials** from https://my.telegram.org
2. **Fill in config file** with your credentials
3. **Run the bot**

## Running the Bot

```bash
make run
```

### What Happens

**Step 1: Telegram Authentication**
```
Enter verification code:
```
- Check your Telegram app for a 5-digit code
- Enter the code in the terminal
- Press Enter
- The session is saved for future runs

**Step 2: Browser Launch**
- Chrome downloads automatically (first time only)
- Browser opens (if `headless: false`)
- Navigates to IQ Options

**Step 3: IQ Options Login**
- Bot enters your email and password
- If 2FA enabled: handle manually in the browser
- Bot switches to demo account
- Ready to trade!

**Step 4: Listening**
```
✓ BOT READY - Waiting for signals...
```
Bot is now monitoring your Telegram channel.

## What You'll See

### When Signal Arrives
```
───────────────────────────────────────
📨 NEW MESSAGE RECEIVED
✓ SIGNAL PARSED SUCCESSFULLY
  📊 Signal Details
    signal_id: abc12345
    asset: AUDUSD
    direction: CALL
    expiry: 2
    confidence: 80
⚖️  Running risk management checks...
✓ Risk checks passed
📤 Queuing signal for execution...
✓ Signal queued successfully queue_length=1
```

### When Trade Executes
```
═══════════════════════════════════════
🔧 WORKER PROCESSING SIGNAL
checking account balance...
💰 current balance balance=10000 required=100
⏳ waiting before trade execution... delay_ms=2000
🎯 EXECUTING TRADE...
─────────────────────────────────────
🎯 PLACING TRADE
→ Step 1/4: Selecting asset... asset=AUDUSD
  ✓ asset selected
→ Step 2/4: Setting expiry time... minutes=2
  ✓ expiry time set
→ Step 3/4: Setting trade amount... amount=10
  ✓ amount set
→ Step 4/4: Clicking direction button... direction=CALL
  ✓ direction clicked
✓ TRADE PLACED SUCCESSFULLY
─────────────────────────────────────
✓ TRADE EXECUTED SUCCESSFULLY
✓ worker cycle complete
```

## Troubleshooting

### "Enter verification code:" but no code received
- Check your Telegram app (mobile/desktop)
- Look for message from Telegram
- Code expires in 5 minutes

### Browser fails to launch
Already fixed with `--no-sandbox` flag. Should work now.

### "login failed - check credentials"
- Verify email/password in `configs/config.yaml`
- Disable 2FA in IQ Options temporarily
- Or handle 2FA manually when browser opens

### No messages received
- Verify channel ID is correct: `-1003488226342`
- Make sure you're a member of the channel
- Check logs for "message from different channel"

### Trade execution fails
- IQ Options selectors need updating
- Run with `headless: false` to see browser
- Inspect elements and update selectors in `internal/trader/trader.go`

## Stopping the Bot

Press `Ctrl+C` in the terminal:
```
^C
stopping bot
bot stopped gracefully
```

## Monitoring

### View Logs
```bash
tail -f logs/bot.log
```

### Check Database
```bash
# Recent signals
sqlite3 data/trades.db "SELECT * FROM signals ORDER BY received_at DESC LIMIT 5;"

# Recent trades  
sqlite3 data/trades.db "SELECT * FROM trades ORDER BY placed_at DESC LIMIT 5;"
```

## Production Tips

1. **Always use demo mode first**
   ```yaml
   iqoption:
     demo_mode: true  # Keep this!
   ```

2. **Set conservative limits**
   ```yaml
   trading:
     default_amount: 5.0
     max_daily_loss: 25.0
   ```

3. **Monitor continuously**
   ```bash
   tail -f logs/bot.log | grep -E "TRADE|SIGNAL|ERROR"
   ```

4. **Keep session alive**
   - Don't delete `session/` folder
   - Telegram session lasts ~1 year
   - No need to re-auth every time
