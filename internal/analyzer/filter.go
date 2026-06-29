package analyzer

import "time"

// Filter is a composable signal gate. Each filter can reject a signal
// before it reaches the next stage.
type Filter interface {
	Name() string
	// Apply returns (pass, reason). If pass is false, signal is rejected.
	Apply(fv FeatureVector, cfg AnalyzerConfig) (pass bool, reason string)
}

// FilterChain runs filters sequentially, short-circuiting on first rejection.
type FilterChain struct {
	filters []Filter
}

func NewFilterChain(filters ...Filter) *FilterChain {
	return &FilterChain{filters: filters}
}

func (fc *FilterChain) Apply(fv FeatureVector, cfg AnalyzerConfig) (pass bool, reason string) {
	for _, f := range fc.filters {
		if ok, r := f.Apply(fv, cfg); !ok {
			return false, f.Name() + ": " + r
		}
	}
	return true, ""
}

// LowVolatilityFilter rejects signals when ATR% is below threshold (flat market)
type LowVolatilityFilter struct{ MinATRPct float64 }

func (f *LowVolatilityFilter) Name() string { return "LowVolatility" }

func (f *LowVolatilityFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	if fv.ATRPct < f.MinATRPct {
		return false, "market too flat for binary options"
	}
	return true, ""
}

// HighVolatilityFilter rejects signals when ATR% is above threshold (news spike)
type HighVolatilityFilter struct{ MaxATRPct float64 }

func (f *HighVolatilityFilter) Name() string { return "HighVolatility" }

func (f *HighVolatilityFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	if fv.ATRPct > f.MaxATRPct {
		return false, "market too volatile (news spike)"
	}
	return true, ""
}

// RegimeFilter rejects signals when regime is unknown
type RegimeFilter struct{}

func (f *RegimeFilter) Name() string { return "Regime" }

func (f *RegimeFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	if fv.Regime == RegimeUnknown {
		return false, "regime is unknown, no tradeable structure"
	}
	return true, ""
}

// ADXFilter rejects signals when ADX is below threshold
type ADXFilter struct{ MinADX float64 }

func (f *ADXFilter) Name() string { return "ADX" }

func (f *ADXFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	if fv.ADX < f.MinADX {
		return false, "ADX too low (choppy/ranging market)"
	}
	return true, ""
}

// BuildFilterChain constructs the live signal filter pipeline from config.
func BuildFilterChain(cfg AnalyzerConfig) *FilterChain {
	filters := []Filter{
		&RegimeFilter{},
		&LowVolatilityFilter{MinATRPct: 0.03},
		&HighVolatilityFilter{MaxATRPct: 0.8},
		&ADXFilter{MinADX: 15},
	}
	if len(cfg.AllowedRegimes) > 0 {
		filters = append([]Filter{&RegimeWhitelistFilter{Allowed: cfg.AllowedRegimes}}, filters[1:]...)
	}
	return NewFilterChain(filters...)
}

// RegimeWhitelistFilter rejects regimes not in the configured allow-list.
type RegimeWhitelistFilter struct {
	Allowed []Regime
}

func (f *RegimeWhitelistFilter) Name() string { return "RegimeWhitelist" }

func (f *RegimeWhitelistFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	for _, allowed := range f.Allowed {
		if fv.Regime == allowed {
			return true, ""
		}
	}
	return false, "regime " + fv.Regime.String() + " not in allow-list"
}

// HourFilter rejects signals outside configured UTC trading hours.
type HourFilter struct {
	AllowedHours []int
}

func (f *HourFilter) Name() string { return "Hour" }

func (f *HourFilter) Apply(_ FeatureVector, _ AnalyzerConfig) (bool, string) {
	if len(f.AllowedHours) == 0 {
		return true, ""
	}
	hour := time.Now().UTC().Hour()
	for _, allowed := range f.AllowedHours {
		if hour == allowed {
			return true, ""
		}
	}
	return false, "UTC hour not in allow-list"
}

// MTFConflictFilter rejects when 5m and 15m trends disagree
type MTFConflictFilter struct{}

func (f *MTFConflictFilter) Name() string { return "MTFConflict" }

func (f *MTFConflictFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	if fv.Trend5m != 0 && fv.Trend15m != 0 && fv.Trend5m != fv.Trend15m {
		return false, "5m and 15m trends disagree"
	}
	return true, ""
}
