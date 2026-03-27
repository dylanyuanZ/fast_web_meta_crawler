package test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dylanyuanZ/fast_web_meta_crawler/src/config"
)

// ==================== Config Load & Default Values Tests ====================

// createTempConfig writes a YAML config to a temp file and returns the path.
func createTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestConfigLoad_AllDefaults(t *testing.T) {
	// Empty config file — all fields should get default values.
	path := createTempConfig(t, "{}")

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()

	if cfg.MaxSearchPage != config.DefaultMaxSearchPage {
		t.Errorf("MaxSearchPage = %d, want %d", cfg.MaxSearchPage, config.DefaultMaxSearchPage)
	}
	if cfg.MaxVideoPerAuthor != config.DefaultMaxVideoPerAuthor {
		t.Errorf("MaxVideoPerAuthor = %d, want %d", cfg.MaxVideoPerAuthor, config.DefaultMaxVideoPerAuthor)
	}
	if cfg.Concurrency != config.DefaultConcurrency {
		t.Errorf("Concurrency = %d, want %d", cfg.Concurrency, config.DefaultConcurrency)
	}
	if cfg.MaxConsecutiveFailures != config.DefaultMaxConsecutiveFailures {
		t.Errorf("MaxConsecutiveFailures = %d, want %d", cfg.MaxConsecutiveFailures, config.DefaultMaxConsecutiveFailures)
	}
	if cfg.OutputDir != config.DefaultOutputDir {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, config.DefaultOutputDir)
	}
	if cfg.RequestInterval != config.DefaultRequestInterval {
		t.Errorf("RequestInterval = %v, want %v", cfg.RequestInterval, config.DefaultRequestInterval)
	}
	if cfg.Browser.UserDataDir != config.DefaultBrowserUserDataDir {
		t.Errorf("Browser.UserDataDir = %q, want %q", cfg.Browser.UserDataDir, config.DefaultBrowserUserDataDir)
	}
}

func TestConfigLoad_ExplicitValues(t *testing.T) {
	yaml := `
max_search_page: 10
max_video_per_author: 500
concurrency: 5
browser:
  headless: false
  user_data_dir: "/tmp/test-profile/"
max_consecutive_failures: 5
output_dir: "output/"
cookie: "SESSDATA=abc123"
request_interval: 1s
`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()

	if cfg.MaxSearchPage != 10 {
		t.Errorf("MaxSearchPage = %d, want 10", cfg.MaxSearchPage)
	}
	if cfg.MaxVideoPerAuthor != 500 {
		t.Errorf("MaxVideoPerAuthor = %d, want 500", cfg.MaxVideoPerAuthor)
	}
	if cfg.Concurrency != 5 {
		t.Errorf("Concurrency = %d, want 5", cfg.Concurrency)
	}
	if cfg.Browser.IsHeadless() {
		t.Error("Browser.IsHeadless() = true, want false")
	}
	if cfg.Browser.UserDataDir != "/tmp/test-profile/" {
		t.Errorf("Browser.UserDataDir = %q, want %q", cfg.Browser.UserDataDir, "/tmp/test-profile/")
	}
	if cfg.MaxConsecutiveFailures != 5 {
		t.Errorf("MaxConsecutiveFailures = %d, want 5", cfg.MaxConsecutiveFailures)
	}
	if cfg.OutputDir != "output/" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "output/")
	}
	if cfg.Cookie != "SESSDATA=abc123" {
		t.Errorf("Cookie = %q, want %q", cfg.Cookie, "SESSDATA=abc123")
	}
	if cfg.RequestInterval != 1*time.Second {
		t.Errorf("RequestInterval = %v, want 1s", cfg.RequestInterval)
	}
}

// ==================== Clamp Values Tests ====================

func TestConfigLoad_ClampMaxSearchPage_TooHigh(t *testing.T) {
	yaml := `max_search_page: 999`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.MaxSearchPage != config.MaxMaxSearchPage {
		t.Errorf("MaxSearchPage = %d, want %d (clamped)", cfg.MaxSearchPage, config.MaxMaxSearchPage)
	}
}

func TestConfigLoad_ClampMaxSearchPage_TooLow(t *testing.T) {
	yaml := `max_search_page: -1`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.MaxSearchPage != config.MinMaxSearchPage {
		t.Errorf("MaxSearchPage = %d, want %d (clamped)", cfg.MaxSearchPage, config.MinMaxSearchPage)
	}
}

func TestConfigLoad_ClampConcurrency_TooHigh(t *testing.T) {
	yaml := `concurrency: 100`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.Concurrency != config.MaxConcurrency {
		t.Errorf("Concurrency = %d, want %d (clamped)", cfg.Concurrency, config.MaxConcurrency)
	}
}

func TestConfigLoad_ClampMaxVideoPerAuthor_TooHigh(t *testing.T) {
	yaml := `max_video_per_author: 99999`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.MaxVideoPerAuthor != config.MaxMaxVideoPerAuthor {
		t.Errorf("MaxVideoPerAuthor = %d, want %d (clamped)", cfg.MaxVideoPerAuthor, config.MaxMaxVideoPerAuthor)
	}
}

func TestConfigLoad_BoundaryValues(t *testing.T) {
	// Test exact boundary values — should NOT be clamped.
	yaml := `
max_search_page: 1
max_video_per_author: 1
concurrency: 1
`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.MaxSearchPage != 1 {
		t.Errorf("MaxSearchPage = %d, want 1", cfg.MaxSearchPage)
	}
	if cfg.MaxVideoPerAuthor != 1 {
		t.Errorf("MaxVideoPerAuthor = %d, want 1", cfg.MaxVideoPerAuthor)
	}
	if cfg.Concurrency != 1 {
		t.Errorf("Concurrency = %d, want 1", cfg.Concurrency)
	}
}

func TestConfigLoad_MaxBoundaryValues(t *testing.T) {
	yaml := `
max_search_page: 50
max_video_per_author: 5000
concurrency: 16
`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.MaxSearchPage != 50 {
		t.Errorf("MaxSearchPage = %d, want 50", cfg.MaxSearchPage)
	}
	if cfg.MaxVideoPerAuthor != 5000 {
		t.Errorf("MaxVideoPerAuthor = %d, want 5000", cfg.MaxVideoPerAuthor)
	}
	if cfg.Concurrency != 16 {
		t.Errorf("Concurrency = %d, want 16", cfg.Concurrency)
	}
}

// ==================== BrowserConfig.IsHeadless Tests ====================

func TestBrowserConfig_IsHeadless_Default(t *testing.T) {
	// When headless is not set (nil pointer), should default to true.
	yaml := `
browser:
  user_data_dir: "/tmp/test/"
`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if !cfg.Browser.IsHeadless() {
		t.Error("Browser.IsHeadless() = false, want true (default)")
	}
}

func TestBrowserConfig_IsHeadless_ExplicitTrue(t *testing.T) {
	yaml := `
browser:
  headless: true
`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if !cfg.Browser.IsHeadless() {
		t.Error("Browser.IsHeadless() = false, want true")
	}
}

func TestBrowserConfig_IsHeadless_ExplicitFalse(t *testing.T) {
	yaml := `
browser:
  headless: false
`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.Browser.IsHeadless() {
		t.Error("Browser.IsHeadless() = true, want false")
	}
}

// ==================== Error Cases ====================

func TestConfigLoad_FileNotFound(t *testing.T) {
	err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestConfigLoad_InvalidYAML(t *testing.T) {
	path := createTempConfig(t, "invalid: yaml: [broken")

	err := config.Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

func TestConfigLoad_EmptyCookie(t *testing.T) {
	yaml := `cookie: ""`
	path := createTempConfig(t, yaml)

	if err := config.Load(path); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg := config.Get()
	if cfg.Cookie != "" {
		t.Errorf("Cookie = %q, want empty", cfg.Cookie)
	}
}
