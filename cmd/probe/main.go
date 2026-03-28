package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/applog"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/browser"
	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
	"github.com/go-rod/rod/lib/proto"
)

// probe: open a Bilibili author's space page and dump all network responses.
// Usage: go run cmd/probe/main.go <mid>
// Example: go run cmd/probe/main.go 314216

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/probe/main.go <mid>")
		fmt.Println("Example: go run cmd/probe/main.go 314216")
		os.Exit(1)
	}
	mid := os.Args[1]

	// Initialize global log: write all log output to both console and log/ directory.
	if err := applog.Init("log"); err != nil {
		log.Printf("WARN: failed to init global log: %v (logs will only go to console)", err)
	}
	defer applog.Close()

	// Load config for browser settings and cookie.
	if err := config.Load("conf/config.yaml"); err != nil {
		log.Fatalf("FATAL: load config: %v", err)
	}
	cfg := config.Get()

	// Init debug log.
	if err := browser.InitDebugLog("log"); err != nil {
		log.Printf("WARN: init debug log: %v", err)
	}
	defer browser.CloseDebugLog()

	// Create browser manager with concurrency=1 (we only need one page).
	mgr, err := browser.New(browser.Config{
		Headless:    cfg.Browser.IsHeadless(),
		UserDataDir: cfg.Browser.UserDataDir,
		Concurrency: 1,
		BrowserBin:  cfg.Browser.Bin,
	})
	if err != nil {
		log.Fatalf("FATAL: create browser: %v", err)
	}
	defer mgr.Close()

	// Inject cookie if available.
	page := mgr.GetPage()
	defer mgr.PutPage(page)

	if cfg.Cookie != "" {
		if err := browser.InjectCookie(page, ".bilibili.com", cfg.Cookie); err != nil {
			log.Printf("WARN: inject cookie: %v", err)
		} else {
			log.Printf("INFO: Cookie injected")
		}
	}

	// Create output directory for dumps.
	dumpDir := filepath.Join("data", "probe", mid)
	os.MkdirAll(dumpDir, 0o755)

	// Enable network domain.
	_ = proto.NetworkEnable{}.Call(page)

	// Collect all API responses.
	type apiResponse struct {
		URL        string `json:"url"`
		Status     int    `json:"status"`
		Type       string `json:"type"`
		BodyLength int    `json:"body_length"`
	}
	var allResponses []apiResponse

	// Listen for network responses.
	go page.EachEvent(func(e *proto.NetworkResponseReceived) bool {
		url := e.Response.URL

		// Only care about API calls (not images, CSS, JS bundles, etc.)
		isAPI := strings.Contains(url, "api.bilibili.com") ||
			strings.Contains(url, "/x/") ||
			strings.Contains(url, "space.bilibili.com/ajax")

		resp := apiResponse{
			URL:    url,
			Status: e.Response.Status,
			Type:   string(e.Type),
		}

		if isAPI {
			// Try to get response body.
			body, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err == nil && body != nil {
				resp.BodyLength = len(body.Body)

				// Save response body to file.
				// Use a safe filename from the URL path.
				safeName := strings.ReplaceAll(url, "https://", "")
				safeName = strings.ReplaceAll(safeName, "http://", "")
				safeName = strings.ReplaceAll(safeName, "/", "_")
				safeName = strings.ReplaceAll(safeName, "?", "_Q_")
				safeName = strings.ReplaceAll(safeName, "&", "_A_")
				if len(safeName) > 150 {
					safeName = safeName[:150]
				}
				safeName = safeName + ".json"

				fpath := filepath.Join(dumpDir, safeName)
				os.WriteFile(fpath, []byte(body.Body), 0o644)
				log.Printf("INFO: [API] %s → %d bytes → %s", url, len(body.Body), safeName)
			} else {
				log.Printf("INFO: [API] %s → (body unavailable: %v)", url, err)
			}
		} else {
			// Just log non-API requests briefly.
			log.Printf("DEBUG: [other] %s (type=%s, status=%d)", url, e.Type, e.Response.Status)
		}

		allResponses = append(allResponses, resp)
		return false // keep listening
	})()

	// Navigate to the author's space page.
	targetURL := fmt.Sprintf("https://space.bilibili.com/%s", mid)
	log.Printf("INFO: Navigating to %s", targetURL)

	if err := page.Navigate(targetURL); err != nil {
		log.Fatalf("FATAL: navigate: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		log.Printf("WARN: wait load: %v", err)
	}

	// Wait a bit for all API calls to complete.
	log.Printf("INFO: Page loaded, waiting 8 seconds for API calls...")
	time.Sleep(8 * time.Second)

	// Also try to extract SSR data (window.__pinia or other global vars).
	log.Printf("INFO: Trying to extract SSR data...")

	ssrChecks := []struct {
		name string
		expr string
	}{
		{"__pinia", `() => { try { return JSON.stringify(window.__pinia); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"__INITIAL_STATE__", `() => { try { return JSON.stringify(window.__INITIAL_STATE__); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"__NEXT_DATA__", `() => { try { return JSON.stringify(window.__NEXT_DATA__); } catch(e) { return 'ERROR: ' + e.message; } }`},
		{"__NUXT__", `() => { try { return JSON.stringify(window.__NUXT__); } catch(e) { return 'ERROR: ' + e.message; } }`},
	}

	for _, check := range ssrChecks {
		result, err := page.Eval(check.expr)
		if err != nil {
			log.Printf("INFO: SSR %s: eval error: %v", check.name, err)
			continue
		}
		str := result.Value.Str()
		if str == "" || strings.HasPrefix(str, "ERROR:") {
			log.Printf("INFO: SSR %s: %s", check.name, str)
			continue
		}
		fpath := filepath.Join(dumpDir, fmt.Sprintf("ssr_%s.json", check.name))
		os.WriteFile(fpath, []byte(str), 0o644)
		log.Printf("INFO: SSR %s: %d bytes → %s", check.name, len(str), fpath)
	}

	// Now navigate to the video tab.
	videoURL := fmt.Sprintf("https://space.bilibili.com/%s/video", mid)
	log.Printf("INFO: Navigating to video tab: %s", videoURL)

	if err := page.Navigate(videoURL); err != nil {
		log.Printf("WARN: navigate to video tab: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		log.Printf("WARN: wait load video tab: %v", err)
	}

	log.Printf("INFO: Video tab loaded, waiting 8 seconds for API calls...")
	time.Sleep(8 * time.Second)

	// Save summary of all responses.
	summaryPath := filepath.Join(dumpDir, "_summary.json")
	summaryData, _ := json.MarshalIndent(allResponses, "", "  ")
	os.WriteFile(summaryPath, summaryData, 0o644)

	log.Printf("INFO: Done! Dumped %d responses to %s", len(allResponses), dumpDir)
	log.Printf("INFO: Check the files in %s to see what data is available", dumpDir)
}
