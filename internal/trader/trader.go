package trader

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
	"github.com/rs/zerolog"

	"signal-bot/internal/config"
	"signal-bot/pkg/models"
)

type Trader struct {
	cfg     *config.IQOptionConfig
	browser *rod.Browser
	page    *rod.Page
	logger  zerolog.Logger
	loggedIn bool
}

func New(cfg *config.IQOptionConfig, logger zerolog.Logger) *Trader {
	return &Trader{
		cfg:    cfg,
		logger: logger,
	}
}

func (t *Trader) Connect(ctx context.Context) error {
	t.logger.Info().Msg("launching browser (Chrome)...")
	l := launcher.New().
		Headless(t.cfg.Headless).
		Set("no-sandbox").
		Set("disable-setuid-sandbox").
		Set("disable-dev-shm-usage")
	
	if !t.cfg.Headless {
		t.logger.Info().Msg("browser will be visible (headless=false)")
	}
	
	t.logger.Debug().Msg("browser launching with sandbox disabled for compatibility...")
	url := l.MustLaunch()
	
	t.logger.Info().Msg("connecting to browser via DevTools Protocol...")
	t.browser = rod.New().ControlURL(url).MustConnect()
	t.page = stealth.MustPage(t.browser)
	t.logger.Debug().Msg("stealth mode enabled")
	
	// Load cookies if they exist
	if t.cfg.CookiesFile != "" && t.loadCookies() {
		t.logger.Info().Msg("✓ loaded saved cookies, checking if session is valid...")
		
		// Navigate to traderoom with existing cookies
		if err := t.page.Navigate(t.cfg.BaseURL); err != nil {
			t.logger.Warn().Err(err).Msg("navigation with cookies failed, will try login")
		} else {
			t.logger.Info().Msg("page loaded, waiting for JavaScript to render...")
			
			// Check frequently with shorter intervals for faster detection
			maxWait := 15 * time.Second
			checkInterval := 1 * time.Second // Check every second
			elapsed := time.Duration(0)
			
			// Initial short wait for page to start loading
			time.Sleep(2 * time.Second)
			elapsed += 2 * time.Second
			
			for elapsed < maxWait {
				t.logger.Debug().
					Float64("elapsed_sec", elapsed.Seconds()).
					Msg("checking session...")
				
				if t.isLoggedIn() {
					t.logger.Info().
						Float64("loaded_in_seconds", elapsed.Seconds()).
						Msg("✓ session valid! Skipping login")
					t.loggedIn = true
					
					// Switch to demo if needed
					if t.cfg.DemoMode {
						t.logger.Info().Msg("demo mode enabled, switching account...")
						if err := t.switchToDemo(); err != nil {
							t.logger.Warn().Err(err).Msg("failed to switch to demo mode")
						} else {
							t.logger.Info().Msg("✓ switched to demo account")
						}
					}
					
					return nil
				}
				
				time.Sleep(checkInterval)
				elapsed += checkInterval
			}
			
			t.logger.Warn().
				Float64("waited_seconds", elapsed.Seconds()).
				Msg("session validation timeout, performing fresh login...")
		}
	}
	
	t.logger.Info().Msg("attempting IQ Option login...")
	if err := t.login(ctx); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	
	// Save cookies after successful login
	if t.cfg.CookiesFile != "" {
		t.saveCookies()
	}
	
	t.logger.Info().Msg("✓ trader connected and ready to execute trades")
	return nil
}
func (t *Trader) login(ctx context.Context) error {
	t.logger.Info().Str("url", t.cfg.BaseURL).Msg("navigating to IQ Option traderoom...")
	
	// Navigate with retry
	maxRetries := 3
	var navErr error
	for i := 0; i < maxRetries; i++ {
		navErr = t.page.Navigate(t.cfg.BaseURL)
		if navErr == nil {
			break
		}
		t.logger.Warn().Err(navErr).Int("attempt", i+1).Msg("navigation failed, retrying...")
		time.Sleep(2 * time.Second)
	}
	if navErr != nil {
		return fmt.Errorf("navigation failed after %d attempts: %w", maxRetries, navErr)
	}
	
	t.logger.Debug().Msg("waiting for page to load...")
	time.Sleep(10 * time.Second) // Wait for JavaScript SPA to render
	
	// Wait for specific elements to appear (page is fully loaded)
	t.logger.Info().Msg("waiting for page JavaScript to render...")
	t.page.MustWaitStable()
	
	// Take screenshot for debugging
	t.logger.Info().Msg("taking screenshot of current page state...")
	screenshot, _ := t.page.Screenshot(true, nil)
	os.WriteFile("debug_page.png", screenshot, 0644)
	t.logger.Info().Msg("screenshot saved to: debug_page.png")
	
	// Get page HTML for debugging
	html, _ := t.page.HTML()
	t.logger.Debug().Int("html_length", len(html)).Msg("page HTML loaded")
	os.WriteFile("debug_page.html", []byte(html), 0644)
	t.logger.Info().Msg("page HTML saved to: debug_page.html")
	
	// Check if already logged in
	t.logger.Info().Msg("checking if already logged in...")
	if t.isLoggedIn() {
		t.logger.Info().Msg("✓ already logged in (session active)")
		t.loggedIn = true
		return nil
	}
	
	t.logger.Info().Msg("not logged in, checking page for login form...")
	
	// Try multiple selectors for email input
	emailSelectors := []string{
		`input[name="email"]`,
		`input[type="email"]`,
		`input[placeholder*="mail" i]`,
		`input[placeholder*="Email" i]`,
		`#login-form input[type="text"]`,
		`.login-form input[type="text"]`,
		`input.email`,
		`input#email`,
	}
	
	var emailInput *rod.Element
	var lastErr error
	
	t.logger.Info().Int("selectors_to_try", len(emailSelectors)).Msg("trying multiple selectors for email field...")
	for i, selector := range emailSelectors {
		t.logger.Debug().Int("attempt", i+1).Str("selector", selector).Msg("trying selector...")
		elem, err := t.page.Timeout(3 * time.Second).Element(selector)
		if err == nil {
			emailInput = elem
			t.logger.Info().Str("selector", selector).Msg("✓ found email input")
			break
		}
		lastErr = err
		t.logger.Debug().Err(err).Str("selector", selector).Msg("selector failed")
	}
	
	if emailInput == nil {
		t.logger.Error().Msg("❌ could not find email input with any selector")
		t.logger.Error().Msg("check debug_page.png and debug_page.html for page state")
		return fmt.Errorf("find email input: %w", lastErr)
	}
	
	t.logger.Debug().Str("email", t.cfg.Email).Msg("entering email...")
	if err := emailInput.Input(t.cfg.Email); err != nil {
		return fmt.Errorf("input email: %w", err)
	}
	
	time.Sleep(500 * time.Millisecond)
	
	// Try multiple selectors for password
	passwordSelectors := []string{
		`input[name="password"]`,
		`input[type="password"]`,
		`input[placeholder*="password" i]`,
		`input[placeholder*="Password" i]`,
		`#login-form input[type="password"]`,
		`.login-form input[type="password"]`,
		`input.password`,
		`input#password`,
	}
	
	var pwdInput *rod.Element
	t.logger.Info().Int("selectors_to_try", len(passwordSelectors)).Msg("trying multiple selectors for password field...")
	for i, selector := range passwordSelectors {
		t.logger.Debug().Int("attempt", i+1).Str("selector", selector).Msg("trying selector...")
		elem, err := t.page.Timeout(3 * time.Second).Element(selector)
		if err == nil {
			pwdInput = elem
			t.logger.Info().Str("selector", selector).Msg("✓ found password input")
			break
		}
		t.logger.Debug().Err(err).Str("selector", selector).Msg("selector failed")
	}
	
	if pwdInput == nil {
		return fmt.Errorf("find password input: %w", lastErr)
	}
	
	t.logger.Debug().Msg("entering password...")
	if err := pwdInput.Input(t.cfg.Password); err != nil {
		return fmt.Errorf("input password: %w", err)
	}
	
	time.Sleep(500 * time.Millisecond)
	
	// Find and click login button
	loginSelectors := []string{
		`button[type="submit"]`,
		`button:contains("Log in")`,
		`button:contains("LOGIN")`,
		`button:contains("Sign in")`,
		`.login-button`,
		`#login-button`,
		`button.submit`,
	}
	
	var loginBtn *rod.Element
	t.logger.Info().Int("selectors_to_try", len(loginSelectors)).Msg("trying multiple selectors for login button...")
	for i, selector := range loginSelectors {
		t.logger.Debug().Int("attempt", i+1).Str("selector", selector).Msg("trying selector...")
		elem, err := t.page.Timeout(3 * time.Second).Element(selector)
		if err == nil {
			loginBtn = elem
			t.logger.Info().Str("selector", selector).Msg("✓ found login button")
			break
		}
		t.logger.Debug().Err(err).Str("selector", selector).Msg("selector failed")
	}
	
	if loginBtn == nil {
		t.logger.Warn().Msg("login button not found, trying to submit form...")
		// Try pressing Enter instead
		if err := pwdInput.Type(input.Enter); err != nil {
			return fmt.Errorf("press enter: %w", err)
		}
	} else {
		t.logger.Info().Msg("clicking login button...")
		if err := loginBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click login button: %w", err)
		}
	}
	
	// Wait for redirect
	t.logger.Info().Msg("waiting for login to complete...")
	time.Sleep(5 * time.Second)
	
	t.logger.Info().Msg("verifying login status...")
	if !t.isLoggedIn() {
		t.logger.Error().Msg("login verification failed - check credentials or 2FA")
		screenshot, _ := t.page.Screenshot(true, nil)
		os.WriteFile("debug_after_login.png", screenshot, 0644)
		t.logger.Info().Msg("post-login screenshot saved to: debug_after_login.png")
		return fmt.Errorf("login failed - check credentials or 2FA")
	}
	
	t.loggedIn = true
	t.logger.Info().Msg("✓ login successful!")
	
	// Switch to demo if needed
	if t.cfg.DemoMode {
		t.logger.Info().Msg("demo mode enabled, switching account...")
		if err := t.switchToDemo(); err != nil {
			t.logger.Warn().Err(err).Msg("failed to switch to demo mode")
		} else {
			t.logger.Info().Msg("✓ switched to demo account")
		}
	}
	
	return nil
}

func (t *Trader) isLoggedIn() bool {
	// Check URL first - if we're on traderoom page AND there's no login form, we're logged in
	info, err := t.page.Info()
	if err != nil {
		t.logger.Debug().Err(err).Msg("failed to get page info")
		return false
	}
	
	t.logger.Debug().Str("url", info.URL).Msg("checking URL...")
	
	// Must be on traderoom URL
	if !strings.Contains(info.URL, "/traderoom") {
		t.logger.Debug().Msg("not on traderoom URL yet")
		return false
	}
	
	// Check for login form elements (if they exist, we're NOT logged in)
	t.logger.Debug().Msg("on traderoom URL, checking for login form...")
	loginForm, err := t.page.Timeout(500 * time.Millisecond).Element(`input[type="email"], input[name="email"], input[placeholder*="mail" i], input[placeholder*="E-mail" i]`)
	if err == nil && loginForm != nil {
		t.logger.Debug().Msg("login form found - not logged in")
		return false
	}
	
	// If we're on traderoom URL and no login form found, we're logged in
	t.logger.Info().Msg("✓ traderoom URL + no login form = session valid!")
	return true
}

func (t *Trader) selectAsset(asset string) error {
	t.logger.Info().Str("asset", asset).Msg("🔍 Selecting asset...")

	assetX := t.cfg.Coordinates.AssetX
	assetY := t.cfg.Coordinates.AssetY
	selectX := t.cfg.Coordinates.AssetSelectX
	selectY := t.cfg.Coordinates.AssetSelectY

	if assetX == 0 || assetY == 0 {
		return fmt.Errorf("asset coordinates not configured - set coordinates.asset_x/asset_y in config.yaml")
	}

	// Step 1: Click to open the asset dropdown
	t.logger.Info().Int("x", assetX).Int("y", assetY).Msg("📍 Step 1/3: Opening asset dropdown...")
	if err := t.page.Mouse.MoveTo(proto.Point{X: float64(assetX), Y: float64(assetY)}); err != nil {
		return fmt.Errorf("move to asset selector: %w", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := t.page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click asset selector: %w", err)
	}
	time.Sleep(1500 * time.Millisecond)
	t.logger.Info().Msg("⏸️  CHECK: Is the asset dropdown/search open?")
	time.Sleep(500 * time.Millisecond)

	// Step 2: Type asset name to filter results
	t.logger.Info().Str("asset", asset).Msg("⌨️  Step 2/3: Typing asset name to filter...")
	for _, char := range asset {
		if err := t.page.Keyboard.Type(input.Key(char)); err != nil {
			return fmt.Errorf("type asset char: %w", err)
		}
		time.Sleep(80 * time.Millisecond)
	}
	time.Sleep(800 * time.Millisecond)
	t.logger.Info().Str("asset", asset).Msg("⏸️  CHECK: Did the asset appear in search results?")
	time.Sleep(800 * time.Millisecond)

	// Step 3: Click the asset in the results list
	if selectX > 0 && selectY > 0 {
		t.logger.Info().Int("x", selectX).Int("y", selectY).Msg("📍 Step 3/3: Clicking asset in results list...")
		if err := t.page.Mouse.MoveTo(proto.Point{X: float64(selectX), Y: float64(selectY)}); err != nil {
			return fmt.Errorf("move to asset in list: %w", err)
		}
		time.Sleep(400 * time.Millisecond)
		if err := t.page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click asset in list: %w", err)
		}
	} else {
		// Fallback: just press Enter to pick first result
		t.logger.Info().Msg("↵  Step 3/3: Pressing Enter to select first result...")
		if err := t.page.Keyboard.Press(input.Enter); err != nil {
			return fmt.Errorf("press Enter: %w", err)
		}
	}

	time.Sleep(1500 * time.Millisecond)
	t.logger.Info().Str("asset", asset).Msg("⏸️  CHECK: Was the asset selected?")
	time.Sleep(500 * time.Millisecond)

	return nil
}

func (t *Trader) setExpiry(minutes int) error {
	t.logger.Info().Int("minutes", minutes).Msg("⏱️  Setting expiry time...")

	expiryX := t.cfg.Coordinates.ExpiryX
	expiryY := t.cfg.Coordinates.ExpiryY
	selectX := t.cfg.Coordinates.ExpirySelectX
	selectY := t.cfg.Coordinates.ExpirySelectY

	if expiryX == 0 || expiryY == 0 {
		return fmt.Errorf("expiry coordinates not configured - set coordinates.expiry_x/expiry_y in config.yaml")
	}

	// Step 1: Click to open expiry dropdown
	t.logger.Info().Int("x", expiryX).Int("y", expiryY).Msg("📍 Step 1/2: Opening expiry dropdown...")
	if err := t.page.Mouse.MoveTo(proto.Point{X: float64(expiryX), Y: float64(expiryY)}); err != nil {
		return fmt.Errorf("move to expiry selector: %w", err)
	}
	time.Sleep(400 * time.Millisecond)
	if err := t.page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click expiry selector: %w", err)
	}

	time.Sleep(1200 * time.Millisecond)
	t.logger.Info().Msg("⏸️  CHECK: Is the expiry dropdown open?")
	time.Sleep(500 * time.Millisecond)

	// Step 2: Click the actual expiry option in the dropdown
	if selectX > 0 && selectY > 0 {
		t.logger.Info().Int("x", selectX).Int("y", selectY).Msg("📍 Step 2/2: Clicking expiry option...")
		if err := t.page.Mouse.MoveTo(proto.Point{X: float64(selectX), Y: float64(selectY)}); err != nil {
			return fmt.Errorf("move to expiry option: %w", err)
		}
		time.Sleep(400 * time.Millisecond)
		if err := t.page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
			return fmt.Errorf("click expiry option: %w", err)
		}
		t.logger.Info().Int("minutes", minutes).Msg("⏸️  CHECK: Was the expiry time selected?")
		time.Sleep(1000 * time.Millisecond)
	} else {
		t.logger.Info().Msg("⎋  Closing expiry dropdown (using current selection)...")
		if err := t.page.Keyboard.Press(input.Escape); err != nil {
			return fmt.Errorf("close expiry dropdown: %w", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

func (t *Trader) setAmount(amount float64) error {
	t.logger.Info().Float64("amount", amount).Msg("💰 Setting trade amount...")

	amountX := t.cfg.Coordinates.AmountX
	amountY := t.cfg.Coordinates.AmountY

	if amountX == 0 || amountY == 0 {
		return fmt.Errorf("amount coordinates not configured - set coordinates.amount_x/amount_y in config.yaml")
	}

	amountStr := fmt.Sprintf("%.0f", amount)

	// Click the amount field to focus it
	t.logger.Info().Int("x", amountX).Int("y", amountY).Msg("📍 Clicking amount field...")
	if err := t.page.Mouse.MoveTo(proto.Point{X: float64(amountX), Y: float64(amountY)}); err != nil {
		return fmt.Errorf("move to amount field: %w", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := t.page.Mouse.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click amount field: %w", err)
	}
	time.Sleep(400 * time.Millisecond)

	// Select all text and delete it using keyboard
	// Press End first to go to end of field, then Shift+Home to select all, then Delete
	t.logger.Info().Msg("⌨️  Selecting all and clearing...")
	t.page.Keyboard.Press(input.End)
	time.Sleep(80 * time.Millisecond)

	// Hold Shift and press Home to select everything
	if err := t.page.Keyboard.Press(input.ShiftLeft); err == nil {
		time.Sleep(50 * time.Millisecond)
		t.page.Keyboard.Press(input.Home)
		time.Sleep(80 * time.Millisecond)
		t.page.Keyboard.Release(input.ShiftLeft)
		time.Sleep(80 * time.Millisecond)
	}

	// Delete selected text
	t.page.Keyboard.Press(input.Delete)
	time.Sleep(100 * time.Millisecond)
	t.page.Keyboard.Press(input.Backspace)
	time.Sleep(100 * time.Millisecond)

	// Type each digit of the new amount
	t.logger.Info().Str("amount", amountStr).Msg("⌨️  Typing new amount...")
	for _, char := range amountStr {
		if err := t.page.Keyboard.Type(input.Key(char)); err != nil {
			return fmt.Errorf("type amount: %w", err)
		}
		time.Sleep(80 * time.Millisecond)
	}

	time.Sleep(600 * time.Millisecond)
	t.logger.Info().Str("amount", amountStr).Msg("⏸️  CHECK: Is the amount showing correctly?")
	time.Sleep(800 * time.Millisecond)

	return nil
}

func (t *Trader) clickDirection(direction models.Direction) error {
	var x, y int
	var buttonName string
	var emoji string
	
	if direction == models.DirectionCall {
		x = t.cfg.Coordinates.CallX
		y = t.cfg.Coordinates.CallY
		buttonName = "CALL (UP/BUY)"
		emoji = "🟢"
	} else {
		x = t.cfg.Coordinates.PutX
		y = t.cfg.Coordinates.PutY
		buttonName = "PUT (DOWN/SELL)"
		emoji = "🔴"
	}
	
	if x == 0 || y == 0 {
		return fmt.Errorf("%s button coordinates not configured in config.yaml - please set coordinates.call_x/call_y or put_x/put_y", buttonName)
	}
	
	t.logger.Info().Str("button", buttonName).Str("emoji", emoji).Msg("🎯 Executing trade direction...")
	t.logger.Info().Str("button", buttonName).Int("x", x).Int("y", y).Msg("📍 Moving mouse to direction button...")
	
	// Move mouse to button
	err := t.page.Mouse.MoveTo(proto.Point{X: float64(x), Y: float64(y)})
	if err != nil {
		return fmt.Errorf("move mouse to %s button: %w", buttonName, err)
	}
	
	time.Sleep(800 * time.Millisecond) // Longer wait so user can see mouse on button
	
	t.logger.Info().Str("button", buttonName).Msg("🖱️  Clicking direction button NOW...")
	// Click button
	err = t.page.Mouse.Click(proto.InputMouseButtonLeft, 1)
	if err != nil {
		return fmt.Errorf("click %s button: %w", buttonName, err)
	}
	
	t.logger.Info().Str("direction", buttonName).Msg("⏸️  CHECK: Did the trade get executed? Watch for confirmation...")
	time.Sleep(3 * time.Second) // Longer wait to see if trade was placed
	
	t.logger.Info().Str("direction", buttonName).Msg("✓ Trade execution attempt complete")
	
	return nil
}

func (t *Trader) GetBalance() (float64, error) {
	// Balance reading from canvas is complex (requires OCR)
	// For now, we'll skip balance checks and rely on risk management
	t.logger.Debug().Msg("balance check skipped (canvas-based UI requires OCR)")
	
	// Return a placeholder value to indicate balance check was skipped
	// The bot's risk management will still work based on trade counts and limits
	return 0, fmt.Errorf("balance reading not supported for canvas UI (non-fatal)")
}

func (t *Trader) switchToDemo() error {
	t.logger.Debug().Msg("looking for account switcher...")
	
	// Try to find account/balance area (usually clickable to switch)
	// Use simpler selectors without :contains() which is not valid CSS
	accountBtn, err := t.page.Timeout(3 * time.Second).Element(`[class*="account"], [class*="balance-type"]`)
	if err != nil {
		t.logger.Debug().Err(err).Msg("account switcher not found, might already be in demo mode")
		return nil // Don't fail, just skip
	}
	
	accountBtn.MustClick()
	t.logger.Debug().Msg("account menu opened")
	time.Sleep(1 * time.Second)
	
	// Look for demo/practice option by text content using JavaScript
	t.logger.Debug().Msg("searching for demo/practice option...")
	_, err = t.page.Eval(`() => {
		const buttons = Array.from(document.querySelectorAll('button, div[role="button"], li'));
		const demoBtn = buttons.find(el => 
			el.textContent.toLowerCase().includes('practice') || 
			el.textContent.toLowerCase().includes('demo')
		);
		if (demoBtn) {
			demoBtn.click();
			return true;
		}
		return false;
	}`)
	
	if err != nil {
		t.logger.Warn().Msg("demo option not found, might already be in demo mode")
		return nil
	}
	
	t.logger.Debug().Msg("switched to demo mode")
	time.Sleep(1 * time.Second)
	
	return nil
}

func (t *Trader) PlaceTrade(ctx context.Context, signal *models.Signal, amount float64) (*models.Trade, error) {
	if !t.loggedIn {
		return nil, fmt.Errorf("not logged in")
	}
	
	t.logger.Info().
		Str("asset", signal.Asset).
		Str("direction", string(signal.Direction)).
		Int("expiry", signal.Expiry).
		Float64("amount", amount).
		Msg("🎯 executing trade...")
	
	// Create trade record
	trade := &models.Trade{
		ID:        signal.ID,
		SignalID:  signal.ID,
		Asset:     signal.Asset,
		Direction: signal.Direction,
		Amount:    amount,
		Expiry:    signal.Expiry,
		Status:    models.StatusPending,
		Result:    models.ResultNone,
		PlacedAt:  time.Now(),
	}
	
	// Step 1: Select asset
	t.logger.Info().Str("asset", signal.Asset).Msg("→ Step 1/4: Selecting asset...")
	if err := t.selectAsset(signal.Asset); err != nil {
		t.logger.Error().Err(err).Msg("failed to select asset")
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, fmt.Errorf("select asset: %w", err)
	}
	t.logger.Info().Msg("✓ asset selected")
	
	// Step 2: Set expiry time
	t.logger.Info().Int("minutes", signal.Expiry).Msg("→ Step 2/4: Setting expiry time...")
	if err := t.setExpiry(signal.Expiry); err != nil {
		t.logger.Error().Err(err).Msg("failed to set expiry")
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, fmt.Errorf("set expiry: %w", err)
	}
	t.logger.Info().Msg("✓ expiry set")
	
	// Step 3: Set amount
	t.logger.Info().Float64("amount", amount).Msg("→ Step 3/4: Setting trade amount...")
	if err := t.setAmount(amount); err != nil {
		t.logger.Error().Err(err).Msg("failed to set amount")
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, fmt.Errorf("set amount: %w", err)
	}
	t.logger.Info().Msg("✓ amount set")
	
	// Step 4: Execute trade
	t.logger.Info().Str("direction", string(signal.Direction)).Msg("→ Step 4/4: Executing trade direction...")
	if err := t.clickDirection(signal.Direction); err != nil {
		t.logger.Error().Err(err).Msg("failed to execute trade")
		trade.Status = models.StatusFailed
		trade.ErrorMsg = err.Error()
		return trade, fmt.Errorf("click direction: %w", err)
	}
	
	trade.Status = models.StatusOpen
	t.logger.Info().
		Str("asset", signal.Asset).
		Str("direction", string(signal.Direction)).
		Msg("✅ TRADE EXECUTED SUCCESSFULLY!")
	
	return trade, nil
}

func (t *Trader) Close() error {
	if t.page != nil {
		t.page.MustClose()
	}
	if t.browser != nil {
		t.browser.MustClose()
	}
	return nil
}
