package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram TelegramConfig `yaml:"telegram"`
	IQOption IQOptionConfig `yaml:"iqoption"`
	Trading  TradingConfig  `yaml:"trading"`
	Risk     RiskConfig     `yaml:"risk"`
	Analyzer AnalyzerConfig `yaml:"analyzer"`
	Logging  LoggingConfig  `yaml:"logging"`
	Database DatabaseConfig `yaml:"database"`
}

type TelegramConfig struct {
	ApiID       int    `yaml:"api_id"`
	ApiHash     string `yaml:"api_hash"`
	Phone       string `yaml:"phone"`
	ChannelID   int64  `yaml:"channel_id"`
	SessionFile string `yaml:"session_file"`
}

type IQOptionConfig struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
	DemoMode bool   `yaml:"demo_mode"`
}

type TradingConfig struct {
	DefaultAmount       float64 `yaml:"default_amount"`
	MaxConcurrentTrades int     `yaml:"max_concurrent_trades"`
	MinBalance          float64 `yaml:"min_balance"`
	TradeDelayMs        int     `yaml:"trade_delay_ms"`
	MaxDailyLoss        float64 `yaml:"max_daily_loss"`
}

type RiskConfig struct {
	Enabled             bool    `yaml:"enabled"`
	MaxTradesPerDay     int     `yaml:"max_trades_per_day"`  // 0 = unlimited
	MinSignalConfidence float64 `yaml:"min_signal_confidence"`
	Martingale          bool    `yaml:"martingale"`
	MartingaleMax       int     `yaml:"martingale_max"`
}

type AnalyzerConfig struct {
	SignalThreshold int      `yaml:"signal_threshold"`
	MinConfidence   float64  `yaml:"min_confidence"`
	IntervalSeconds int      `yaml:"interval_seconds"`
	SignalCooldown  int      `yaml:"signal_cooldown"`
	ExpirySeconds   int      `yaml:"expiry_seconds"`
	Assets          []string `yaml:"assets"`
	// AssetTypes filters which categories to analyze.
	// Valid values: forex, crypto, stocks, indices, commodities
	// Empty = use Assets list directly (no filtering)
	AssetTypes []string `yaml:"asset_types"`
	// AllowedRegimes whitelists tradeable regimes (Trending, Ranging, etc.).
	// Empty = allow any non-Unknown regime.
	AllowedRegimes []string `yaml:"allowed_regimes"`
	// AllowedHoursUTC restricts signals to these UTC hours (0-23).
	AllowedHoursUTC []int `yaml:"allowed_hours_utc"`
	// AllowedHoursFile loads UTC hours exported by backtest -export-hours.
	AllowedHoursFile string `yaml:"allowed_hours_file"`
	// CalibrationPath stores live outcome updates from executed trades.
	CalibrationPath string `yaml:"calibration_path"`
}

type LoggingConfig struct {
	Level   string `yaml:"level"`
	File    string `yaml:"file"`
	Console bool   `yaml:"console"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.Telegram.ApiID == 0 {
		return fmt.Errorf("telegram.api_id is required")
	}
	if c.Telegram.ApiHash == "" {
		return fmt.Errorf("telegram.api_hash is required")
	}
	if c.IQOption.Email == "" {
		return fmt.Errorf("iqoption.email is required")
	}
	if c.IQOption.Password == "" {
		return fmt.Errorf("iqoption.password is required")
	}

	// Trading validation
	if c.Trading.DefaultAmount < 0 {
		return fmt.Errorf("trading.default_amount must be >= 0")
	}
	if c.Trading.MaxConcurrentTrades <= 0 {
		c.Trading.MaxConcurrentTrades = 1 // safe default
	}

	// Risk validation
	if c.Risk.MinSignalConfidence < 0 || c.Risk.MinSignalConfidence > 1.0 {
		return fmt.Errorf("risk.min_signal_confidence must be between 0 and 1 (got %.2f)", c.Risk.MinSignalConfidence)
	}

	// Analyzer validation
	if c.Analyzer.SignalThreshold < 0 {
		return fmt.Errorf("analyzer.signal_threshold must be >= 0")
	}
	if c.Analyzer.MinConfidence < 0 || c.Analyzer.MinConfidence > 1.0 {
		return fmt.Errorf("analyzer.min_confidence must be between 0 and 1 (got %.2f)", c.Analyzer.MinConfidence)
	}
	if c.Analyzer.ExpirySeconds < 0 {
		return fmt.Errorf("analyzer.expiry_seconds must be >= 0")
	}
	if c.Analyzer.IntervalSeconds < 0 {
		return fmt.Errorf("analyzer.interval_seconds must be >= 0")
	}
	for _, h := range c.Analyzer.AllowedHoursUTC {
		if h < 0 || h > 23 {
			return fmt.Errorf("analyzer.allowed_hours_utc: %d is not a valid UTC hour (0-23)", h)
		}
	}

	return nil
}
