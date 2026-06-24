package wstrader

import "strings"

// IsOTCAvailable returns true if the asset has a 24/7 OTC version on IQ Option.
// Only 9 pairs have OTC versions. All others are market-hours only.
func IsOTCAvailable(asset string) bool {
	asset = strings.ToUpper(strings.TrimSpace(asset))
	asset = strings.Replace(asset, "-OTC", "", 1)
	return otcPairs[asset]
}

// GetOTCPairs returns all pairs that have 24/7 OTC versions
func GetOTCPairs() []string {
	pairs := make([]string, 0, len(otcPairs))
	for k := range otcPairs {
		pairs = append(pairs, k)
	}
	return pairs
}
