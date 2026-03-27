package browser

import (
	"fmt"
	"log"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// stealthJS is injected into every new Page to hide headless/webdriver fingerprints.
// This prevents sites (e.g. Bilibili) from detecting automation and blocking requests.
const stealthJS = `() => {
	// Hide navigator.webdriver flag.
	Object.defineProperty(navigator, 'webdriver', { get: () => undefined });

	// Fake plugins array (headless has 0 plugins).
	Object.defineProperty(navigator, 'plugins', {
		get: () => [1, 2, 3, 4, 5],
	});

	// Fake languages.
	Object.defineProperty(navigator, 'languages', {
		get: () => ['zh-CN', 'zh', 'en'],
	});

	// Remove Chrome DevTools protocol detection.
	window.chrome = { runtime: {} };

	// Override permissions query to hide "denied" notification state.
	const originalQuery = window.navigator.permissions.query;
	window.navigator.permissions.query = (parameters) =>
		parameters.name === 'notifications'
			? Promise.resolve({ state: Notification.permission })
			: originalQuery(parameters);
}`

// Config holds browser-related configuration (from config.yaml browser block).
type Config struct {
	Headless    bool   // headless mode (default true)
	UserDataDir string // user data directory for login persistence
	Concurrency int    // number of Pages to create (= pool size)
	BrowserBin  string // custom browser binary path (skip auto-download if set)
}

// Manager manages a single Browser process and a pool of reusable Pages.
// It is the central entry point for all browser operations.
// Safe for concurrent use (pagePool is a buffered channel).
type Manager struct {
	browser  *rod.Browser
	pagePool chan *rod.Page // buffered channel as Page pool
	cfg      Config
}

// New creates a Manager, launches a Chromium process, and pre-creates Pages.
// The Browser uses cfg.UserDataDir for login state persistence.
func New(cfg Config) (*Manager, error) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}

	l := buildLauncher(cfg)
	wsURL, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(wsURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect browser: %w", err)
	}

	log.Printf("INFO: [browser] Browser launched, headless=%v, user_data_dir=%s", cfg.Headless, cfg.UserDataDir)

	// Create Page pool.
	pagePool := make(chan *rod.Page, cfg.Concurrency)
	for i := 0; i < cfg.Concurrency; i++ {
		page, err := browser.Page(proto.TargetCreateTarget{URL: ""})
		if err != nil {
			// Clean up already created pages and browser on failure.
			close(pagePool)
			for p := range pagePool {
				p.Close()
			}
			browser.Close()
			return nil, fmt.Errorf("create page %d: %w", i, err)
		}
		// Inject stealth JS to hide headless fingerprints before any navigation.
		if _, err := page.EvalOnNewDocument(stealthJS); err != nil {
			log.Printf("WARN: [browser] failed to inject stealth JS on page %d: %v", i, err)
		}
		pagePool <- page
	}

	log.Printf("INFO: [browser] Page pool created, size=%d", cfg.Concurrency)

	return &Manager{
		browser:  browser,
		pagePool: pagePool,
		cfg:      cfg,
	}, nil
}

// GetPage borrows a Page from the pool (blocks if all Pages are in use).
// The caller MUST call PutPage() when done.
func (m *Manager) GetPage() *rod.Page {
	return <-m.pagePool
}

// PutPage returns a Page to the pool after use.
// Navigates the Page to about:blank to reset state before returning.
func (m *Manager) PutPage(page *rod.Page) {
	// Reset page state to avoid residual interceptors and JS context.
	// We only navigate to about:blank — no need to wait for stability
	// since about:blank is a trivial page that loads instantly.
	if err := page.Navigate("about:blank"); err != nil {
		log.Printf("WARN: [browser] failed to navigate page to about:blank: %v", err)
	}
	// Brief sleep to allow the navigation to take effect and clear event listeners.
	time.Sleep(200 * time.Millisecond)
	m.pagePool <- page
}

// Close gracefully shuts down all Pages and the Browser process.
// Must be called on program exit (typically via defer).
func (m *Manager) Close() error {
	// Drain and close all pages from the pool.
	close(m.pagePool)
	for page := range m.pagePool {
		if err := page.Close(); err != nil {
			log.Printf("WARN: [browser] failed to close page: %v", err)
		}
	}

	// Close the browser process.
	if err := m.browser.Close(); err != nil {
		return fmt.Errorf("close browser: %w", err)
	}

	log.Printf("INFO: [browser] Browser closed")
	return nil
}

// Browser returns the underlying rod.Browser for advanced operations.
// Used by auth module for login flow.
func (m *Manager) Browser() *rod.Browser {
	return m.browser
}

// buildLauncher constructs a rod Launcher with appropriate flags.
func buildLauncher(cfg Config) *launcher.Launcher {
	l := launcher.New().
		Headless(cfg.Headless).
		UserDataDir(cfg.UserDataDir).
		Set("disable-gpu").
		Set("disable-dev-shm-usage"). // Docker/low-memory environments
		Set("no-sandbox").            // Linux server environments
		Set("disable-background-networking").
		Set("disable-extensions").
		Set("disable-blink-features", "AutomationControlled"). // Hide webdriver flag from Blink.
		Set("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// Use custom browser binary if specified, skipping auto-download and validation.
	if cfg.BrowserBin != "" {
		l = l.Bin(cfg.BrowserBin)
		log.Printf("INFO: [browser] Using custom browser binary: %s", cfg.BrowserBin)
	}

	return l
}
