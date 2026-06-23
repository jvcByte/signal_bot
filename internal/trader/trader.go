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
	t.logger.Debug().Str("asset", asset).Msg("clicking asset selector button...")
	// Click asset selector
	assetBtn, err := t.page.Timeout(3 * time.Second).Element(`[class*="asset"], [class*="instrument"]`)
	if err != nil {
		return fmt.Errorf("find asset selector: %w", err)
	}
	
	assetBtn.MustClick()
	t.logger.Debug().Msg("asset selector opened")
	time.Sleep(1 * time.Second)
	
	// Search for asset
	t.logger.Debug().Msg("locating search box...")
	searchBox, err := t.page.Timeout(2 * time.Second).Element(`input[placeholder*="Search"], input[type="search"]`)
	if err != nil {
		return fmt.Errorf("find search box: %w", err)
	}
	
	t.logger.Debug().Str("search_term", asset).Msg("typing asset name...")
	searchBox.MustInput(asset)
	time.Sleep(1 * time.Second)
	
	// Select first result
	t.logger.Debug().Msg("selecting asset from results...")
	result, err := t.page.Timeout(2 * time.Second).Element(fmt.Sprintf(`[class*="asset-item"]:contains("%s"), li:contains("%s")`, asset, asset))
	if err != nil {
		return fmt.Errorf("find asset in list: %w", err)
	}
	
	result.MustClick()
	t.logger.Debug().Str("asset", asset).Msg("asset selected")
	time.Sleep(1 * time.Second)
	
	return nil
}

func (t *Trader) setExpiry(minutes int) error {
	t.logger.Debug().Int("minutes", minutes).Msg("clicking expiry selector...")
	expiryBtn, err := t.page.Timeout(3 * time.Second).Element(`[class*="expiry"], [class*="time"]`)
	if err != nil {
		return fmt.Errorf("find expiry selector: %w", err)
	}
	
	expiryBtn.MustClick()
	t.logger.Debug().Msg("expiry dropdown opened")
	time.Sleep(500 * time.Millisecond)
	
	// Look for expiry option matching minutes
	t.logger.Debug().Int("minutes", minutes).Msg("selecting expiry option...")
	expiryOption, err := t.page.Timeout(2 * time.Second).Element(fmt.Sprintf(`[data-value="%d"], li:contains("%d min")`, minutes, minutes))
	if err != nil {
		return fmt.Errorf("find expiry option: %w", err)
	}
	
	expiryOption.MustClick()
	t.logger.Debug().Int("minutes", minutes).Msg("expiry time selected")
	time.Sleep(500 * time.Millisecond)
	
	return nil
}

func (t *Trader) setAmount(amount float64) error {
	t.logger.Debug().Float64("amount", amount).Msg("locating amount input field...")
	amountInput, err := t.page.Timeout(3 * time.Second).Element(`input[class*="amount"], input[type="number"]`)
	if err != nil {
		return fmt.Errorf("find amount input: %w", err)
	}
	
	t.logger.Debug().Msg("clearing existing amount...")
	amountInput.MustSelectAllText()
	
	t.logger.Debug().Float64("amount", amount).Msg("entering new amount...")
	amountInput.MustInput(fmt.Sprintf("%.2f", amount))
	time.Sleep(500 * time.Millisecond)
	
	return nil
}

func (t *Trader) clickDirection(direction models.Direction) error {
	var selector string
	var buttonName string
	if direction == models.DirectionCall {
		selector = `button[class*="call"], button[class*="up"], button:contains("CALL")`
		buttonName = "CALL (UP)"
	} else {
		selector = `button[class*="put"], button[class*="down"], button:contains("PUT")`
		buttonName = "PUT (DOWN)"
	}
	
	t.logger.Debug().Str("button", buttonName).Msg("locating direction button...")
	btn, err := t.page.Timeout(3 * time.Second).Element(selector)
	if err != nil {
		return fmt.Errorf("find direction button: %w", err)
	}
	
	t.logger.Debug().Str("button", buttonName).Msg("clicking direction button...")
	btn.MustClick()
	t.logger.Debug().Msg("waiting for trade confirmation...")
	time.Sleep(2 * time.Second)
	
	return nil
}

func (t *Trader) GetBalance() (float64, error) {
	t.logger.Debug().Msg("locating balance element...")
	balanceElem, err := t.page.Timeout(3 * time.Second).Element(`[class*="balance"]`)
	if err != nil {
		return 0, fmt.Errorf("find balance element: %w", err)
	}
	
	balanceText := balanceElem.MustText()
	t.logger.Debug().Str("raw_text", balanceText).Msg("reading balance text...")
	
	var balance float64
	fmt.Sscanf(balanceText, "$%f", &balance)
	
	t.logger.Debug().Float64("balance", balance).Msg("balance parsed")
	return balance, nil
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
