package config

import (
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Default values for configuration parameters.
const (
	DefaultMaxSearchVideos        = 100
	DefaultMaxVideoPerAuthor      = 1000
	DefaultConcurrency            = 3
	DefaultMaxConsecutiveFailures = 10
	DefaultOutputDir              = "data/"
	DefaultRequestInterval        = 500 * time.Millisecond
	DefaultBrowserHeadless        = true
	DefaultBrowserUserDataDir     = "data/browser-profile/"
)

// Clamp boundaries.
const (
	MinMaxSearchVideos   = 1
	MaxMaxSearchVideos   = 10000
	MinMaxVideoPerAuthor = 1
	MaxMaxVideoPerAuthor = 5000
	MinConcurrency       = 1
	MaxConcurrency       = 16
)

// BrowserConfig holds browser automation configuration.
type BrowserConfig struct {
	Headless    *bool  `yaml:"headless"`      // headless mode (default true, use pointer to distinguish unset from false)
	UserDataDir string `yaml:"user_data_dir"` // browser profile directory for login persistence
	Bin         string `yaml:"bin"`           // custom browser binary path (skip auto-download if set)
}

// IsHeadless returns the effective headless value (defaults to true if not set).
func (b BrowserConfig) IsHeadless() bool {
	if b.Headless == nil {
		return DefaultBrowserHeadless
	}
	return *b.Headless
}

// BilibiliConfig holds Bilibili platform-specific configuration.
type BilibiliConfig struct {
	Cookie          string        `yaml:"cookie"`           // browser cookie for authenticated access
	Concurrency     int           `yaml:"concurrency"`      // platform-level concurrency (0 = use global)
	RequestInterval time.Duration `yaml:"request_interval"` // platform-level request interval (0 = use global)
}

// YouTubeConfig holds YouTube platform-specific configuration.
type YouTubeConfig struct {
	// Filter options for search (Stage 0).
	FilterType      string        `yaml:"filter_type"`      // "video" or "short" (empty = all)
	FilterDuration  string        `yaml:"filter_duration"`  // "short" (<4min), "medium" (4-20min), "long" (>20min)
	FilterUpload    string        `yaml:"filter_upload"`    // "today", "week", "month", "year"
	SortBy          string        `yaml:"sort_by"`          // "relevance", "date", "view_count", "rating"
	Concurrency     int           `yaml:"concurrency"`      // platform-level concurrency (0 = use global)
	RequestInterval time.Duration `yaml:"request_interval"` // platform-level request interval (0 = use global)
}

// PlatformConfig holds per-platform configuration.
type PlatformConfig struct {
	Bilibili BilibiliConfig `yaml:"bilibili"`
	YouTube  YouTubeConfig  `yaml:"youtube"`
}

// Config holds all application configuration.
type Config struct {
	// Global settings.
	OutputDir string        `yaml:"output_dir"`
	Browser   BrowserConfig `yaml:"browser"`

	// Per-platform settings.
	Platform PlatformConfig `yaml:"platform"`

	// Global search/crawl settings.
	MaxSearchVideos        int           `yaml:"max_search_videos"`
	MaxVideoPerAuthor      int           `yaml:"max_video_per_author"`
	Concurrency            int           `yaml:"concurrency"`
	MaxConsecutiveFailures int           `yaml:"max_consecutive_failures"`
	RequestInterval        time.Duration `yaml:"request_interval"`
	Cookie                 string        `yaml:"cookie"` // legacy: use platform.bilibili.cookie instead
}

// global holds the loaded configuration singleton.
var global *Config

// Load reads and parses the configuration file at the given path.
func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return err
	}

	applyDefaults(cfg)
	clampValues(cfg)

	global = cfg
	return nil
}

// Get returns the global configuration object.
func Get() *Config {
	if global == nil {
		log.Fatal("FATAL: config.Get() called before config.Load()")
	}
	return global
}

// GetPlatformConcurrency returns the effective concurrency for the given platform.
// Priority: platform config > global fallback.
func (c *Config) GetPlatformConcurrency(platform string) int {
	switch platform {
	case "bilibili":
		if c.Platform.Bilibili.Concurrency > 0 {
			return c.Platform.Bilibili.Concurrency
		}
	case "youtube":
		if c.Platform.YouTube.Concurrency > 0 {
			return c.Platform.YouTube.Concurrency
		}
	}
	return c.Concurrency
}

// GetPlatformRequestInterval returns the effective request interval for the given platform.
// Priority: platform config > global fallback.
func (c *Config) GetPlatformRequestInterval(platform string) time.Duration {
	switch platform {
	case "bilibili":
		if c.Platform.Bilibili.RequestInterval > 0 {
			return c.Platform.Bilibili.RequestInterval
		}
	case "youtube":
		if c.Platform.YouTube.RequestInterval > 0 {
			return c.Platform.YouTube.RequestInterval
		}
	}
	return c.RequestInterval
}

// GetBilibiliCookie returns the effective Bilibili cookie.
// Prefers platform.bilibili.cookie, falls back to legacy global cookie.
func (c *Config) GetBilibiliCookie() string {
	if c.Platform.Bilibili.Cookie != "" {
		return c.Platform.Bilibili.Cookie
	}
	return c.Cookie
}

// applyDefaults fills in zero-value fields with default values.
func applyDefaults(cfg *Config) {
	if cfg.MaxSearchVideos == 0 {
		cfg.MaxSearchVideos = DefaultMaxSearchVideos
	}
	if cfg.MaxVideoPerAuthor == 0 {
		cfg.MaxVideoPerAuthor = DefaultMaxVideoPerAuthor
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.Browser.UserDataDir == "" {
		cfg.Browser.UserDataDir = DefaultBrowserUserDataDir
	}
	if cfg.MaxConsecutiveFailures == 0 {
		cfg.MaxConsecutiveFailures = DefaultMaxConsecutiveFailures
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = DefaultOutputDir
	}
	if cfg.RequestInterval == 0 {
		cfg.RequestInterval = DefaultRequestInterval
	}
}

// clampValues ensures configuration values are within valid ranges.
func clampValues(cfg *Config) {
	cfg.MaxSearchVideos = clampInt("max_search_videos", cfg.MaxSearchVideos, MinMaxSearchVideos, MaxMaxSearchVideos)
	cfg.MaxVideoPerAuthor = clampInt("max_video_per_author", cfg.MaxVideoPerAuthor, MinMaxVideoPerAuthor, MaxMaxVideoPerAuthor)
	cfg.Concurrency = clampInt("concurrency", cfg.Concurrency, MinConcurrency, MaxConcurrency)

	// Clamp per-platform concurrency if set.
	if cfg.Platform.Bilibili.Concurrency > 0 {
		cfg.Platform.Bilibili.Concurrency = clampInt("platform.bilibili.concurrency", cfg.Platform.Bilibili.Concurrency, MinConcurrency, MaxConcurrency)
	}
	if cfg.Platform.YouTube.Concurrency > 0 {
		cfg.Platform.YouTube.Concurrency = clampInt("platform.youtube.concurrency", cfg.Platform.YouTube.Concurrency, MinConcurrency, MaxConcurrency)
	}
}

// clampInt clamps an integer value to [min, max] and logs a warning if clamped.
func clampInt(name string, value, min, max int) int {
	if value < min {
		log.Printf("WARN: config %s=%d below minimum, clamped to %d", name, value, min)
		return min
	}
	if value > max {
		log.Printf("WARN: config %s=%d above maximum, clamped to %d", name, value, max)
		return max
	}
	return value
}
