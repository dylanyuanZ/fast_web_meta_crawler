package config

import (
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Default values for configuration parameters.
const (
	DefaultMaxSearchPage          = 50
	DefaultMaxVideoPerAuthor      = 1000
	DefaultConcurrency            = 2
	DefaultHTTPTimeout            = 15 * time.Second
	DefaultMaxRetries             = 3
	DefaultInitialDelay           = 2 * time.Second
	DefaultMaxDelay               = 15 * time.Second
	DefaultBackoffFactor          = 2.0
	DefaultMaxConsecutiveFailures = 10
	DefaultOutputDir              = "data/"
	DefaultRequestInterval        = 1200 * time.Millisecond
)

// Clamp boundaries.
const (
	MinMaxSearchPage     = 1
	MaxMaxSearchPage     = 50
	MinMaxVideoPerAuthor = 1
	MaxMaxVideoPerAuthor = 5000
	MinConcurrency       = 1
	MaxConcurrency       = 16
)

// HTTPConfig holds HTTP client configuration.
type HTTPConfig struct {
	Timeout       time.Duration `yaml:"timeout"`
	MaxRetries    int           `yaml:"max_retries"`
	InitialDelay  time.Duration `yaml:"initial_delay"`
	MaxDelay      time.Duration `yaml:"max_delay"`
	BackoffFactor float64       `yaml:"backoff_factor"`
}

// Config holds all application configuration.
type Config struct {
	MaxSearchPage          int           `yaml:"max_search_page"`
	MaxVideoPerAuthor      int           `yaml:"max_video_per_author"`
	Concurrency            int           `yaml:"concurrency"`
	HTTP                   HTTPConfig    `yaml:"http"`
	MaxConsecutiveFailures int           `yaml:"max_consecutive_failures"`
	OutputDir              string        `yaml:"output_dir"`
	Cookie                 string        `yaml:"cookie"`
	RequestInterval        time.Duration `yaml:"request_interval"`
}

// global holds the loaded configuration singleton.
var global *Config

// Load reads and parses the configuration file at the given path.
// It fills in default values for missing fields and clamps out-of-range values.
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
// Must be called after Load().
func Get() *Config {
	if global == nil {
		log.Fatal("FATAL: config.Get() called before config.Load()")
	}
	return global
}

// applyDefaults fills in zero-value fields with default values.
func applyDefaults(cfg *Config) {
	if cfg.MaxSearchPage == 0 {
		cfg.MaxSearchPage = DefaultMaxSearchPage
	}
	if cfg.MaxVideoPerAuthor == 0 {
		cfg.MaxVideoPerAuthor = DefaultMaxVideoPerAuthor
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.HTTP.Timeout == 0 {
		cfg.HTTP.Timeout = DefaultHTTPTimeout
	}
	if cfg.HTTP.MaxRetries == 0 {
		cfg.HTTP.MaxRetries = DefaultMaxRetries
	}
	if cfg.HTTP.InitialDelay == 0 {
		cfg.HTTP.InitialDelay = DefaultInitialDelay
	}
	if cfg.HTTP.MaxDelay == 0 {
		cfg.HTTP.MaxDelay = DefaultMaxDelay
	}
	if cfg.HTTP.BackoffFactor == 0 {
		cfg.HTTP.BackoffFactor = DefaultBackoffFactor
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
// Out-of-range values are clamped to the nearest boundary with a WARN log.
func clampValues(cfg *Config) {
	cfg.MaxSearchPage = clampInt("max_search_page", cfg.MaxSearchPage, MinMaxSearchPage, MaxMaxSearchPage)
	cfg.MaxVideoPerAuthor = clampInt("max_video_per_author", cfg.MaxVideoPerAuthor, MinMaxVideoPerAuthor, MaxMaxVideoPerAuthor)
	cfg.Concurrency = clampInt("concurrency", cfg.Concurrency, MinConcurrency, MaxConcurrency)
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
