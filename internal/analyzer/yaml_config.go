package analyzer

import (
	"encoding/json"
	"os"

	"signal-bot/internal/config"
)

// ApplyYAMLConfig maps YAML analyzer settings onto runtime AnalyzerConfig.
func ApplyYAMLConfig(base AnalyzerConfig, cfg config.AnalyzerConfig) AnalyzerConfig {
	if cfg.SignalThreshold > 0 {
		base.SignalThreshold = float64(cfg.SignalThreshold)
	}
	if cfg.SignalCooldown > 0 {
		base.SignalCooldown = cfg.SignalCooldown
	}
	if cfg.ExpirySeconds > 0 {
		base.ExpiryMinutes = cfg.ExpirySeconds
	}
	if cfg.MinConfidence > 0 {
		base.MinConfidence = cfg.MinConfidence
	}
	if len(cfg.AllowedRegimes) > 0 {
		base.AllowedRegimes = ParseRegimeNames(cfg.AllowedRegimes)
	}
	if len(cfg.AllowedHoursUTC) > 0 {
		base.AllowedHoursUTC = append([]int(nil), cfg.AllowedHoursUTC...)
	}
	if cfg.AllowedHoursFile != "" {
		if hours, err := LoadAllowedHoursFile(cfg.AllowedHoursFile); err == nil && len(hours) > 0 {
			base.AllowedHoursUTC = hours
		}
	}
	return base
}

// LoadAllowedHoursFile reads a JSON array of UTC hours from backtest export.
func LoadAllowedHoursFile(path string) ([]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var hours []int
	if err := json.Unmarshal(data, &hours); err != nil {
		return nil, err
	}
	return hours, nil
}

// RecommendHours returns UTC hours meeting min win rate with enough samples.
func RecommendHours(byHour map[int]HourStats, minWinRate float64, minTrades int) []int {
	var hours []int
	for h, stats := range byHour {
		if stats.Trades >= minTrades && stats.WinRate >= minWinRate {
			hours = append(hours, h)
		}
	}
	for i := 0; i < len(hours); i++ {
		for j := i + 1; j < len(hours); j++ {
			if hours[j] < hours[i] {
				hours[i], hours[j] = hours[j], hours[i]
			}
		}
	}
	return hours
}

// SaveAllowedHoursFile writes recommended UTC hours for config consumption.
func SaveAllowedHoursFile(path string, hours []int) error {
	data, err := json.MarshalIndent(hours, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
