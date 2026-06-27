package analyzer

import (
	"encoding/json"
	"math"
	"os"
	"sync"
)

// ConfidenceModel estimates the probability of a winning trade based on
// the current market regime and a running history of outcomes.
//
// It uses a simple Bayesian-inspired update rule:
//   new_wr = ((n-1)*old_wr + outcome) / n
//
// All public methods are safe for concurrent use.
type ConfidenceModel struct {
	mu          sync.RWMutex
	RegimeStats map[Regime]map[int]ScoreTierStats // [regime][score_tier]
}

// NewConfidenceModel returns a model initialised with empty maps.
func NewConfidenceModel() *ConfidenceModel {
	m := &ConfidenceModel{
		RegimeStats: make(map[Regime]map[int]ScoreTierStats),
	}
	for _, r := range []Regime{RegimeUnknown, RegimeTrending, RegimeRanging, RegimeVolatile, RegimeBreakout} {
		m.RegimeStats[r] = make(map[int]ScoreTierStats)
	}
	return m
}

// Estimate returns a calibrated probability in (0, 0.95].
//
// The estimate is formed by:
//  1. Base win-rate from regime+tier history (or 0.60 if no data)
//  2. ATR% quality adjustment (very low ATR → hard to win binary options)
//  3. Score strength adjustment (higher abs score → more conviction)
//  4. Hard cap at 0.95
func (m *ConfidenceModel) Estimate(score float64, regime Regime, features FeatureVector) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tier := int(math.Abs(score))

	// 1. Base win-rate
	base := 0.60
	if tiers, ok := m.RegimeStats[regime]; ok {
		if ts, ok2 := tiers[tier]; ok2 && ts.Trades >= 20 {
			base = ts.WinRate
		}
	}

	// 2. Volatility quality: low ATR% is bad for binary options (price doesn't move)
	atrAdj := 0.0
	if features.ATRPct < 0.05 {
		atrAdj = -0.08 // very flat market, bad for binary options
	} else if features.ATRPct > 0.15 && features.ATRPct < 0.4 {
		atrAdj = +0.03 // sweet spot
	}

	// 3. Score strength: more agreement → higher confidence
	maxScore := weightRSIExtreme + weightStoch + weightEMACross + weightADX + weightBB + weightMACD + weightEngulfing + weightPinBar + weightMTF5m + weightMTF15m
	absScore := math.Abs(score)
	scoreAdj := (absScore/maxScore)*0.15 // up to +15%

	conf := base + atrAdj + scoreAdj

	// 4. Cap
	if conf > 0.95 {
		conf = 0.95
	}
	if conf < 0.10 {
		conf = 0.10
	}
	return conf
}

// Update performs an online Bayesian update for the given regime+tier pair.
// Call this after each trade result is known.
func (m *ConfidenceModel) Update(score float64, regime Regime, won bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tier := int(math.Abs(score))

	if m.RegimeStats[regime] == nil {
		m.RegimeStats[regime] = make(map[int]ScoreTierStats)
	}

	ts := m.RegimeStats[regime][tier]
	ts.Trades++
	outcome := 0.0
	if won {
		outcome = 1.0
	}
	// Incremental mean: new_mean = old_mean + (x - old_mean) / n
	ts.WinRate += (outcome - ts.WinRate) / float64(ts.Trades)
	m.RegimeStats[regime][tier] = ts
}

// confidenceSnapshot is the on-disk JSON representation
type confidenceSnapshot struct {
	RegimeStats map[string]map[int]ScoreTierStats `json:"regime_stats"`
}

// SaveToFile persists the model to a JSON file atomically.
func (m *ConfidenceModel) SaveToFile(path string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap := confidenceSnapshot{
		RegimeStats: make(map[string]map[int]ScoreTierStats),
	}
	for regime, tiers := range m.RegimeStats {
		snap.RegimeStats[regime.String()] = tiers
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadFromFile restores a model previously saved by SaveToFile.
// Unknown regime strings are silently skipped.
func (m *ConfidenceModel) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var snap confidenceSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	// Build a reverse lookup: string → Regime
	nameToRegime := map[string]Regime{
		RegimeUnknown.String():  RegimeUnknown,
		RegimeTrending.String(): RegimeTrending,
		RegimeRanging.String():  RegimeRanging,
		RegimeVolatile.String(): RegimeVolatile,
		RegimeBreakout.String(): RegimeBreakout,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name, tiers := range snap.RegimeStats {
		regime, ok := nameToRegime[name]
		if !ok {
			continue
		}
		m.RegimeStats[regime] = tiers
	}
	return nil
}
