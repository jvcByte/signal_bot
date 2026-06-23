# Quick Coordinate Calibration Guide

## Why Calibration is Needed

IQ Option uses a **Canvas-based UI** - the entire trading interface is drawn on an HTML5 `<canvas>` element using WebGL. There are no clickable HTML buttons or input fields. Instead, everything is rendered programmatically.

This means traditional web automation (clicking by CSS selector) won't work. We must use **coordinate-based clicking** - clicking at specific pixel positions on the screen.

## Quick Steps

### 1. Run Calibration Tool
```bash
make calibrate
```

This will:
- Open Chrome with IQ Option traderoom
- Take a screenshot: `calibration_screenshot.png`
- Keep browser open for manual verification

### 2. Open Screenshot
Open `calibration_screenshot.png` in any image editor that shows pixel coordinates:
- **Windows:** Paint, GIMP, Photoshop
- **Mac:** Preview (Tools → Show Inspector), Pixelmator
- **Linux:** GIMP, Krita

### 3. Find Button Coordinates

Hover your mouse over these elements and note the X,Y coordinates (usually shown in bottom-left):

| Element | What to Look For | Config Key |
|---------|------------------|------------|
| **Asset Selector** | Top-left button showing current pair (e.g., "EUR/USD") | `asset_x`, `asset_y` |
| **Expiry Selector** | Top area button showing timeframe (e.g., "2 min") | `expiry_x`, `expiry_y` |
| **Amount Input** | Center field where you type trade amount | `amount_x`, `amount_y` |
| **CALL Button** | Bottom button, usually GREEN, for upward trades | `call_x`, `call_y` |
| **PUT Button** | Bottom button, usually RED, for downward trades | `put_x`, `put_y` |

### 4. Update Config

Edit `configs/config.yaml`:

```yaml
iqoption:
  coordinates:
    asset_x: 150    # Replace with your values
    asset_y: 50
    expiry_x: 300
    expiry_y: 100
    amount_x: 640
    amount_y: 400
    call_x: 500     # GREEN button
    call_y: 650
    put_x: 780      # RED button
    put_y: 650
```

### 5. Test

```bash
make run
```

Send a test signal to your Telegram channel and watch the bot click the buttons.

## Tips

- **Click center of buttons** - don't click edges, aim for the middle
- **Round to nearest 5-10 pixels** - gives margin for error
- **Keep browser size consistent** - coordinates change if you resize window
- **Default resolution: 1280x800** - different resolution = recalibrate

## Typical Coordinates (1280x800)

These are **approximate** - you must calibrate for your specific setup:

```yaml
coordinates:
  asset_x: 150      # Top-left area
  asset_y: 50
  expiry_x: 300     # Top middle area
  expiry_y: 100
  amount_x: 640     # Center of screen
  amount_y: 400
  call_x: 500       # Bottom left (green button)
  call_y: 650
  put_x: 780        # Bottom right (red button)
  put_y: 650
```

## Troubleshooting

### Browser window size different
- Take new screenshot
- Recalibrate all coordinates
- Keep `headless: false` to see actual window size

### Clicks missing buttons by a few pixels
- Adjust coordinates by ±10-20 pixels
- Test again
- Fine-tune until accurate

### IQ Option UI updated/changed
- Take fresh screenshot
- Button positions may have moved
- Recalibrate all coordinates

## Alternative: Visual Testing

Instead of screenshot, you can test clicks live:

1. Run: `make run`
2. Browser opens (headless: false)
3. Manually trigger a test trade
4. Watch where bot clicks
5. If wrong position, adjust config
6. Restart and test again

## Understanding the Canvas

When you "Right-click → Inspect" on IQ Option, you'll only see:

```html
<canvas id="glcanvas" width="1280" height="800"></canvas>
```

This canvas contains the entire UI rendered via WebGL. There are no child elements to inspect, which is why coordinate-based clicking is necessary.

## See Full Documentation

For detailed information, see [docs/CANVAS_UI.md](docs/CANVAS_UI.md)
