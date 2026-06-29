package analyzer

import (
	"testing"
	"time"
)

func TestRegimeWhitelistFilter(t *testing.T) {
	filter := &RegimeWhitelistFilter{Allowed: []Regime{RegimeTrending, RegimeRanging}}

	pass, _ := filter.Apply(FeatureVector{Regime: RegimeTrending}, AnalyzerConfig{})
	if !pass {
		t.Fatal("expected Trending to pass")
	}

	pass, _ = filter.Apply(FeatureVector{Regime: RegimeVolatile}, AnalyzerConfig{})
	if pass {
		t.Fatal("expected Volatile to be rejected")
	}
}

func TestHourFilter(t *testing.T) {
	current := time.Now().UTC().Hour()
	filter := &HourFilter{AllowedHours: []int{current}}

	pass, _ := filter.Apply(FeatureVector{}, AnalyzerConfig{})
	if !pass {
		t.Fatal("expected current UTC hour to pass")
	}

	other := (current + 1) % 24
	filter = &HourFilter{AllowedHours: []int{other}}
	pass, _ = filter.Apply(FeatureVector{}, AnalyzerConfig{})
	if pass {
		t.Fatal("expected non-matching hour to be rejected")
	}
}

func TestParseRegimeNames(t *testing.T) {
	regimes := ParseRegimeNames([]string{"Trending", "Ranging", "Bad"})
	if len(regimes) != 2 {
		t.Fatalf("expected 2 regimes, got %d", len(regimes))
	}
}

func TestRecommendHours(t *testing.T) {
	byHour := map[int]HourStats{
		8:  {Trades: 10, Wins: 8, WinRate: 0.80},
		9:  {Trades: 2, Wins: 2, WinRate: 1.00},
		14: {Trades: 10, Wins: 5, WinRate: 0.50},
	}
	hours := RecommendHours(byHour, 0.70, 3)
	if len(hours) != 1 || hours[0] != 8 {
		t.Fatalf("expected [8], got %v", hours)
	}
}

func TestIsAllowedHourUTC(t *testing.T) {
	if !IsAllowedHourUTC(10, nil) {
		t.Fatal("empty allow-list should pass all hours")
	}
	if IsAllowedHourUTC(10, []int{8, 9}) {
		t.Fatal("hour 10 should be rejected")
	}
}
