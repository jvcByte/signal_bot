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
	Email       string            `yaml:"email"`
	Password    string            `yaml:"password"`
	DemoMode    bool              `yaml:"demo_mode"`
	BaseURL     string            `yaml:"base_url"`
	Headless    bool              `yaml:"headless"`
	CookiesFile string            `yaml:"cookies_file"`
	Coordinates CoordinatesConfig `yaml:"coordinates"`
}

type CoordinatesConfig struct {
	// Canvas UI coordinates (calibrate based on your screen resolution)
	AssetX        int `yaml:"asset_x"`          // Click to open asset dropdown
	AssetY        int `yaml:"asset_y"`
	AssetSelectX  int `yaml:"asset_select_x"`   // Click asset in list (first trade)
	AssetSelectY  int `yaml:"asset_select_y"`
	AssetSelectX2 int `yaml:"asset_select_x2"`  // Click asset in list (subsequent trades - UI shifts)
	AssetSelectY2 int `yaml:"asset_select_y2"`
	ExpiryX       int `yaml:"expiry_x"`         // Click to open expiry dropdown
	ExpiryY       int `yaml:"expiry_y"`
	ExpirySelectX int `yaml:"expiry_select_x"`  // Click the actual expiry option
	ExpirySelectY int `yaml:"expiry_select_y"`
	AmountX       int `yaml:"amount_x"`         // Click amount field
	AmountY       int `yaml:"amount_y"`
	CallX         int `yaml:"call_x"`           // Green CALL/BUY button
	CallY         int `yaml:"call_y"`
	PutX          int `yaml:"put_x"`            // Red PUT/SELL button
	PutY          int `yaml:"put_y"`
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
	return nil
}
