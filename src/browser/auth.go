package browser

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// LoginChecker is a platform-specific function that checks if the browser is logged in.
// Returns true if logged in, false otherwise.
// Implementations check platform-specific cookies or page elements.
type LoginChecker func(ctx context.Context, page *rod.Page) (bool, error)

// EnsureLogin ensures the browser has a valid login state.
// Strategy (in priority order):
//  1. Check user_data_dir for existing login state (via checker)
//  2. If headless=false and not logged in: open login page, wait for manual login
//  3. If headless=true and not logged in: inject cookie from config
//  4. If no cookie configured: proceed anonymously (log warning)
func EnsureLogin(ctx context.Context, manager *Manager, loginURL string, cookie string, checker LoginChecker) error {
	page := manager.GetPage()
	defer manager.PutPage(page)

	// Step 1: Navigate to the platform and check existing login state.
	if err := page.Navigate(loginURL); err != nil {
		return fmt.Errorf("navigate to login URL %s: %w", loginURL, err)
	}
	page.MustWaitStable()

	loggedIn, err := checker(ctx, page)
	if err != nil {
		return fmt.Errorf("check login state: %w", err)
	}

	if loggedIn {
		log.Printf("INFO: [browser] Already logged in (from user_data_dir)")
		return nil
	}

	// Step 2: Not logged in — try strategies based on headless mode.
	if !manager.cfg.Headless {
		// GUI mode: wait for manual login.
		log.Printf("INFO: [browser] Not logged in, waiting for manual login...")
		if err := WaitForManualLogin(ctx, page, loginURL, checker); err != nil {
			return fmt.Errorf("manual login: %w", err)
		}
		log.Printf("INFO: [browser] Manual login successful, login state saved to user_data_dir")
		return nil
	}

	// Headless mode: try cookie injection.
	if cookie != "" {
		log.Printf("INFO: [browser] Not logged in, injecting cookie...")
		// Extract domain from loginURL.
		domain := extractDomain(loginURL)
		if err := InjectCookie(page, domain, cookie); err != nil {
			return fmt.Errorf("inject cookie: %w", err)
		}

		// Reload page and verify login.
		if err := page.Navigate(loginURL); err != nil {
			return fmt.Errorf("reload after cookie injection: %w", err)
		}
		page.MustWaitStable()

		loggedIn, err = checker(ctx, page)
		if err != nil {
			return fmt.Errorf("verify login after cookie injection: %w", err)
		}
		if loggedIn {
			log.Printf("INFO: [browser] Cookie injection successful, logged in")
			return nil
		}
		log.Printf("WARN: [browser] Cookie injection failed, cookie may be expired")
	}

	// Step 3: No valid login — proceed anonymously.
	log.Printf("WARN: [browser] Running in anonymous mode (no valid login state)")
	return nil
}

// InjectCookie parses a cookie string and injects it into the browser via CDP.
// Cookie format: "key1=value1; key2=value2; ..."
func InjectCookie(page *rod.Page, domain string, cookieStr string) error {
	pairs := strings.Split(cookieStr, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			log.Printf("WARN: [browser] skipping malformed cookie pair: %q", pair)
			continue
		}

		name := strings.TrimSpace(pair[:eqIdx])
		value := strings.TrimSpace(pair[eqIdx+1:])

		_, err := proto.NetworkSetCookie{
			Name:   name,
			Value:  value,
			Domain: domain,
			Path:   "/",
		}.Call(page)
		if err != nil {
			return fmt.Errorf("set cookie %s: %w", name, err)
		}
	}

	log.Printf("INFO: [browser] Injected %d cookies for domain %s", len(pairs), domain)
	return nil
}

// WaitForManualLogin opens the login page in a visible browser and waits
// for the user to complete login manually.
// Polls the checker function every 3 seconds until it returns true or ctx expires.
func WaitForManualLogin(ctx context.Context, page *rod.Page, loginURL string, checker LoginChecker) error {
	log.Printf("INFO: [browser] Please log in manually in the browser window...")
	log.Printf("INFO: [browser] Login URL: %s", loginURL)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("manual login timeout: %w", ctx.Err())
		case <-ticker.C:
			loggedIn, err := checker(ctx, page)
			if err != nil {
				log.Printf("WARN: [browser] login check error: %v", err)
				continue
			}
			if loggedIn {
				return nil
			}
			log.Printf("INFO: [browser] Waiting for login...")
		}
	}
}

// extractDomain extracts the domain from a URL for cookie injection.
// Example: "https://www.bilibili.com" → ".bilibili.com"
func extractDomain(rawURL string) string {
	// Remove protocol.
	domain := rawURL
	if idx := strings.Index(domain, "://"); idx >= 0 {
		domain = domain[idx+3:]
	}
	// Remove path.
	if idx := strings.Index(domain, "/"); idx >= 0 {
		domain = domain[:idx]
	}
	// Remove port.
	if idx := strings.Index(domain, ":"); idx >= 0 {
		domain = domain[:idx]
	}
	// Add leading dot for subdomain matching.
	if !strings.HasPrefix(domain, ".") {
		// Strip "www." prefix and add dot.
		domain = strings.TrimPrefix(domain, "www.")
		domain = "." + domain
	}
	return domain
}
