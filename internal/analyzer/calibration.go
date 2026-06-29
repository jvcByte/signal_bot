package analyzer

import "signal-bot/pkg/models"

// RecordSignalOutcome updates the confidence model when a trade settles.
func RecordSignalOutcome(model *ConfidenceModel, signal *models.Signal, won bool) bool {
	if model == nil || signal == nil || signal.AnalyzerRegime == "" {
		return false
	}
	regime, ok := ParseRegime(signal.AnalyzerRegime)
	if !ok {
		return false
	}
	model.Update(signal.AnalyzerScore, regime, won)
	return true
}

// IsAllowedHourUTC returns true when hour filtering is disabled or the hour matches.
func IsAllowedHourUTC(hour int, allowed []int) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, h := range allowed {
		if h == hour {
			return true
		}
	}
	return false
}
