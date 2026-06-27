package analyzer

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

// MTFConflictFilter rejects when 5m and 15m trends disagree
type MTFConflictFilter struct{}

func (f *MTFConflictFilter) Name() string { return "MTFConflict" }

func (f *MTFConflictFilter) Apply(fv FeatureVector, _ AnalyzerConfig) (bool, string) {
	if fv.Trend5m != 0 && fv.Trend15m != 0 && fv.Trend5m != fv.Trend15m {
		return false, "5m and 15m trends disagree"
	}
	return true, ""
}
