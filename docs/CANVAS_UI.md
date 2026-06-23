# Canvas UI Interaction Guide

## Problem: IQ Option Uses Canvas Rendering

IQ Option's traderoom is built using **Canvas (WebGL)**, not standard HTML elements. This means:
- ❌ No buttons, inputs, or divs to select with CSS
- ❌ Standard browser automation (click by selector) won't work
- ✅ We must use **coordinate-based clicking** instead

## How Coordinate-Based Trading Works

Instead of finding elements by class/id, we click at specific pixel positions:

```go
// Click CALL button at position (500, 650)
page.Mouse.Click(proto.InputMouseButtonLeft, 500.0, 650.0, 1)
```

## Calibration Required

You **must calibrate coordinates** for your screen resolution. Default values in config are placeholders.

### Quick Start

1. **Run calibration tool:**
   ```bash
   make calibrate
   ```

2. **Browser opens** showing IQ Option traderoom

3. **Screenshot is auto-saved** to `calibration_screenshot.png`

4. **Open screenshot** in image editor (Paint, GIMP, Preview, etc.)

5. **Hover over UI elements** to find pixel coordinates:
   - Asset selector (top-left, shows "EUR/USD")
   - Expiry selector (top area, shows "2 min")
   - Amount input (center, where you type trade size)
   - CALL button (bottom, green/up)
   - PUT button (bottom, red/down)

6. **Update `configs/config.yaml`** with actual coordinates:
   ```yaml
   iqoption:
     coordinates:
       asset_x: 150   # Your calibrated values
       asset_y: 50
       expiry_x: 300
       expiry_y: 100
       amount_x: 640
       amount_y: 400
       call_x: 500
       call_y: 650
       put_x: 780
       put_y: 650
   ```

7. **Test** by running bot and sending a signal

## Manual Calibration (Alternative)

If calibrate tool doesn't work:

1. Run bot: `make run`
2. Browser opens with IQ Option
3. Press `PrintScreen` or use browser DevTools screenshot
4. Open in image editor
5. Hover over buttons, note coordinates in bottom-left corner
6. Update config.yaml

## Resolution Matters

Coordinates are resolution-dependent:
- Default values assume **1280x800** window size
- Different resolution = different coordinates
- You must recalibrate if you change window size

## Interaction Flow

### 1. Select Asset
- **Click** asset selector at `(asset_x, asset_y)`
- **Type** asset name (e.g., "EURUSD")
- **Press** Enter to select first result

### 2. Set Expiry (Optional)
- **Click** expiry selector at `(expiry_x, expiry_y)`
- Currently uses default/current selection
- Enhancement: Could use arrow keys to select specific time

### 3. Set Amount
- **Click** amount field at `(amount_x, amount_y)`
- **Select all** text (Ctrl+A)
- **Type** new amount value

### 4. Execute Trade
- **Click** CALL button at `(call_x, call_y)` for BUY
- **Or click** PUT button at `(put_x, put_y)` for SELL

## Limitations

### ❌ Balance Reading
Reading balance from canvas requires OCR (Optical Character Recognition). Currently skipped as non-fatal.

**Workaround:** Risk management still works based on:
- Trade count limits
- Daily loss limits (tracked in database)
- Hourly trade limits

### ❌ Resolution Changes
If browser window size changes, coordinates become invalid.

**Workaround:** Keep browser window at consistent size, or recalibrate.

### ❌ UI Updates
If IQ Option redesigns their interface, coordinates may shift.

**Workaround:** Recalibrate after platform updates.

## Future: WebSocket API

A more robust solution would be IQ Option's **WebSocket API**:

**Pros:**
- No UI interaction needed
- Resolution-independent
- Faster execution
- More reliable

**Cons:**
- Requires reverse-engineering unofficial API
- Authentication may differ from web login
- No official documentation

**Reference implementations:**
- Python: [ejtraderIQ](https://github.com/ejtraderLabs/ejtraderIQ)
- JavaScript: [LuKks/iqoption](https://github.com/LuKks/iqoption)

## Troubleshooting

### "coordinates not configured" Error
- You must set all coordinate values in config.yaml
- Default placeholders are not calibrated for your screen

### Clicks Missing Buttons
- Coordinates are off by a few pixels
- Recalibrate using screenshot method
- Check browser window size matches calibration

### Keyboard Input Not Working
- Canvas UI still accepts keyboard events
- Typing works for asset search and amount input
- Ensure field is focused first (click coordinates)

### Trade Executes Wrong Direction
- CALL vs PUT coordinates swapped
- Check which button is green (CALL) vs red (PUT)
- Update config accordingly

## Tips

- **Keep headless: false** during calibration to see what's happening
- **Take multiple screenshots** at different stages (asset selector open, closed, etc.)
- **Use high contrast** in image editor to see UI elements clearly
- **Round coordinates** to nearest 5-10 pixels for margin of error
- **Test with manual clicks first** in calibrate tool before running bot
