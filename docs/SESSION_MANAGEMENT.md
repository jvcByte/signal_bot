# Session Management

## How Telegram Sessions Work

A session file stores your authentication state so you don't need to re-authenticate every time.

## What's In a Session File?

```
session/telegram.session
```

Contains:
- Auth key (encrypted)
- Server salt
- User ID
- DC (Data Center) info
- Last sequence number

**This file is sensitive!** Anyone with this file can access your Telegram account.

## Session Lifecycle

### First Run (No Session)
```bash
make run
→ Enter phone: +1234567890
→ Enter code: 12345
→ Session saved to: session/telegram.session
✓ Authenticated
```

### Subsequent Runs (Session Exists)
```bash
make run
→ Loading session: session/telegram.session
✓ Authenticated (no code needed)
```

### Session Validity
- **Duration**: ~1 year (or until you logout)
- **Invalidated by**:
  - Manual logout in Telegram app
  - Changing password
  - Deleting session file
  - Security breach (Telegram revokes)

## Security Best Practices

### Protect Session Files
```bash
# Set restrictive permissions
chmod 600 session/*.session

# Never commit to git (already in .gitignore)
echo "session/" >> .gitignore

# Encrypt if storing remotely
gpg -c session/telegram.session
```

### Separate Sessions Per Environment
```yaml
# Development
telegram:
  session_file: "session/dev.session"

# Production
telegram:
  session_file: "session/prod.session"
```

### Session Rotation
```bash
# Periodically refresh sessions
rm session/telegram.session
make run  # Re-authenticate
```

## Troubleshooting

### "Session invalid" Error
```
ERROR: session invalid
```

**Cause:** Session expired or revoked

**Solution:**
```bash
rm session/telegram.session
make run  # Re-authenticate with new code
```

### "Session corrupted" Error
```
ERROR: failed to decode session
```

**Cause:** File corrupted or incomplete write

**Solution:**
```bash
# Restore from backup
cp session/telegram.session.backup session/telegram.session

# Or delete and re-auth
rm session/telegram.session
make run
```

### Multiple Bots Fighting
```
ERROR: another client is active
```

**Cause:** Two bot instances using same session

**Solution:**
- Use different session files
- Or stop other instance

### Session Keeps Expiring
```
# Every run asks for code
```

**Check:**
1. Permissions on session directory
   ```bash
   ls -la session/
   # Should be writable
   ```

2. Session file actually saved
   ```bash
   ls -lh session/telegram.session
   # Should have size (not 0 bytes)
   ```

3. Path in config is correct
   ```yaml
   telegram:
     session_file: "session/telegram.session"
   ```

## Backup & Restore

### Manual Backup
```bash
# Backup current session
cp session/telegram.session session/telegram.session.$(date +%Y%m%d)

# List backups
ls -lh session/*.session*
```

### Automated Backup
```bash
# Add to crontab
0 0 * * 0 cp ~/bot/session/telegram.session ~/backups/tg-session-$(date +\%Y\%m\%d)
```

### Restore Session
```bash
# From backup
cp session/telegram.session.20260622 session/telegram.session

# From another server
scp server1:~/bot/session/telegram.session session/

# Then restart bot
make run
```

## Session Transfer

### Moving Bot to New Server

**Option 1: Transfer session file**
```bash
# On old server
tar czf bot-session.tar.gz session/

# Transfer
scp bot-session.tar.gz newserver:~/

# On new server
tar xzf bot-session.tar.gz
make run  # Uses existing session
```

**Option 2: Fresh auth**
```bash
# On new server (no session file)
make run
# Enter code: 12345
# New session created
```

**Note:** Only one client can be active per session. If you transfer session, logout from old server first.

### Running Multiple Bots

**Each bot needs its own session:**

```yaml
# bot1.yaml
telegram:
  phone: "+1234567890"
  session_file: "session/bot1.session"

# bot2.yaml  
telegram:
  phone: "+0987654321"
  session_file: "session/bot2.session"
```

**Or use different accounts:**
- Phone 1 → Bot 1
- Phone 2 → Bot 2

## Session Monitoring

### Check Session Status
```bash
# Last modified (should update occasionally)
stat session/telegram.session

# File size (should be >100 bytes)
du -h session/telegram.session
```

### Log Session Activity
```go
// In telegram/client.go
c.logger.Info().
    Str("session_file", c.cfg.SessionFile).
    Msg("session loaded successfully")
```

### Session Health Check
```bash
#!/bin/bash
# healthcheck.sh

if [ ! -f session/telegram.session ]; then
    echo "ERROR: Session file missing"
    exit 1
fi

if [ ! -s session/telegram.session ]; then
    echo "ERROR: Session file empty"
    exit 1
fi

echo "OK: Session file exists and has content"
```

## Advanced: Session Encryption

### Encrypt Session at Rest
```bash
# Encrypt before backup
gpg --symmetric --cipher-algo AES256 session/telegram.session

# Decrypt when needed
gpg --decrypt session/telegram.session.gpg > session/telegram.session
```

### Use Environment Variables
```bash
# Export session as base64
export TG_SESSION=$(cat session/telegram.session | base64)

# In bot startup
echo $TG_SESSION | base64 -d > session/telegram.session
```

## FAQ

**Q: Can I share session between bots?**
A: No, each bot should have its own session. Sharing causes conflicts.

**Q: How to logout?**
A: Delete session file or use Telegram app → Settings → Devices → Terminate session

**Q: Session file in Docker?**
A: Mount as volume:
```yaml
volumes:
  - ./session:/app/session
```

**Q: Can I generate session programmatically?**
A: No, you must authenticate interactively at least once.

**Q: What if session leaks?**
A: Immediately logout from Telegram app → Settings → Devices → Revoke access

## Best Practices Summary

✅ **DO:**
- Keep session files private (chmod 600)
- Backup session files regularly
- Use separate sessions per environment
- Monitor session validity

❌ **DON'T:**
- Commit session files to git
- Share session files
- Run multiple bots with same session
- Store sessions in public locations
