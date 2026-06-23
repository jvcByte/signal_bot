package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"signal-bot/internal/config"
)

func main() {
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  IQ OPTION COORDINATE CALIBRATOR")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()
	
	// Load config
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	
	fmt.Println("🚀 Launching browser...")
	
	// Launch browser
	l := launcher.New().
		Headless(false).
		Set("no-sandbox").
		Set("disable-setuid-sandbox")
	
	url := l.MustLaunch()
	browser := rod.New().ControlURL(url).MustConnect()
	defer browser.Close()
	
	page := stealth.MustPage(browser)
	
	// Load cookies if available (skip if not present)
	cookiePath := cfg.IQOption.CookiesFile
	if _, err := os.Stat(cookiePath); err == nil {
		fmt.Println("✓ Loading saved cookies...")
	}
	
	// Navigate to traderoom
	fmt.Println("🌐 Opening IQ Option traderoom...")
	if err := page.Navigate(cfg.IQOption.BaseURL); err != nil {
		log.Fatalf("failed to navigate: %v", err)
	}
	
	time.Sleep(3 * time.Second)
	
	fmt.Println()
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  CALIBRATION INSTRUCTIONS")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()
	fmt.Println("1. Take a screenshot of the IQ Option window")
	fmt.Println("   - Press PrintScreen or use browser DevTools")
	fmt.Println("   - Or run: page.Screenshot(...)")
	fmt.Println()
	fmt.Println("2. Open screenshot in an image editor")
	fmt.Println("   - Windows: Paint, GIMP, Photoshop")
	fmt.Println("   - Mac: Preview, Pixelmator")
	fmt.Println("   - Linux: GIMP, Krita")
	fmt.Println()
	fmt.Println("3. Hover over these elements to get coordinates:")
	fmt.Println()
	fmt.Println("   📍 Asset Selector (top-left)")
	fmt.Println("      - Button showing current pair (e.g., 'EUR/USD')")
	fmt.Println("      - Update: coordinates.asset_x and asset_y")
	fmt.Println()
	fmt.Println("   📍 Expiry Selector (top area)")
	fmt.Println("      - Button showing timeframe (e.g., '2 min', '5 min')")
	fmt.Println("      - Update: coordinates.expiry_x and expiry_y")
	fmt.Println()
	fmt.Println("   📍 Amount Input (center area)")
	fmt.Println("      - Field where you enter trade amount")
	fmt.Println("      - Update: coordinates.amount_x and amount_y")
	fmt.Println()
	fmt.Println("   📍 CALL/UP/BUY Button (bottom, usually GREEN)")
	fmt.Println("      - Left button for upward trades")
	fmt.Println("      - Update: coordinates.call_x and call_y")
	fmt.Println()
	fmt.Println("   📍 PUT/DOWN/SELL Button (bottom, usually RED)")
	fmt.Println("      - Right button for downward trades")
	fmt.Println("      - Update: coordinates.put_x and put_y")
	fmt.Println()
	fmt.Println("4. Update configs/config.yaml with the coordinates")
	fmt.Println()
	fmt.Println("5. Test by running: make run")
	fmt.Println()
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()
	
	// Take a screenshot to help calibration
	screenshotPath := "calibration_screenshot.png"
	data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{})
	if err == nil {
		os.WriteFile(screenshotPath, data, 0644)
		fmt.Printf("✓ Screenshot saved to: %s\n", screenshotPath)
		fmt.Println("  Open this image to find coordinates!")
		fmt.Println()
	}
	
	fmt.Println("Press Ctrl+C when done...")
	
	// Keep browser open
	select {}
}
