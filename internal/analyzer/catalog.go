package analyzer

import "strings"

// AssetType classifies an asset into a trading category
type AssetType string

const (
	AssetTypeForex       AssetType = "forex"
	AssetTypeCrypto      AssetType = "crypto"
	AssetTypeStocks      AssetType = "stocks"
	AssetTypeIndices     AssetType = "indices"
	AssetTypeCommodities AssetType = "commodities"
	AssetTypeUnknown     AssetType = "unknown"
)

// assetCatalog maps OTC asset names to their type
var assetCatalog = map[string]AssetType{
	// ── Forex OTC
	"EURUSD": AssetTypeForex, "GBPUSD": AssetTypeForex, "AUDUSD": AssetTypeForex,
	"USDJPY": AssetTypeForex, "USDCAD": AssetTypeForex, "USDCHF": AssetTypeForex,
	"EURJPY": AssetTypeForex, "GBPJPY": AssetTypeForex, "AUDJPY": AssetTypeForex,
	"AUDCAD": AssetTypeForex, "EURGBP": AssetTypeForex, "EURAUD": AssetTypeForex,
	"EURCAD": AssetTypeForex, "GBPAUD": AssetTypeForex, "GBPCAD": AssetTypeForex,
	"CHFJPY": AssetTypeForex, "CADCHF": AssetTypeForex, "AUDCHF": AssetTypeForex,
	"AUDNZD": AssetTypeForex, "NZDCAD": AssetTypeForex, "NZDJPY": AssetTypeForex,
	"GBPCHF": AssetTypeForex, "GBPNZD": AssetTypeForex, "EURCHF": AssetTypeForex,
	"EURNZD": AssetTypeForex, "CADJPY": AssetTypeForex, "USDNOK": AssetTypeForex,
	"USDSEK": AssetTypeForex, "USDPLN": AssetTypeForex, "USDTRY": AssetTypeForex,
	"USDSGD": AssetTypeForex, "USDHKD": AssetTypeForex, "USDINR": AssetTypeForex,
	"USDMYR": AssetTypeForex, "USDNGN": AssetTypeForex, "USDZAR": AssetTypeForex,
	"USDTHB": AssetTypeForex, "EURTHB": AssetTypeForex, "JPYTHB": AssetTypeForex,

	// ── Crypto OTC
	"BTCUSD": AssetTypeCrypto, "ETHUSD": AssetTypeCrypto, "XRPUSD": AssetTypeCrypto,
	"SOLUSD": AssetTypeCrypto, "DOGECOIN": AssetTypeCrypto, "CARDANO": AssetTypeCrypto,
	"LTCUSD": AssetTypeCrypto, "TONUSD": AssetTypeCrypto, "BCHUSD": AssetTypeCrypto,
	"ARBUSD": AssetTypeCrypto, "ATOMUSD": AssetTypeCrypto, "BONKUSD": AssetTypeCrypto,
	"DASHUSD": AssetTypeCrypto, "DOTUSD": AssetTypeCrypto, "DYDXUSD": AssetTypeCrypto,
	"EOSUSD": AssetTypeCrypto, "FETUSD": AssetTypeCrypto, "FLOKIUSD": AssetTypeCrypto,
	"GRTUSD": AssetTypeCrypto, "HBARUSD": AssetTypeCrypto, "ICPUSD": AssetTypeCrypto,
	"IMXUSD": AssetTypeCrypto, "INJUSD": AssetTypeCrypto, "IOTAUSD": AssetTypeCrypto,
	"JUPUSD": AssetTypeCrypto, "LINKUSD": AssetTypeCrypto, "LUNA": AssetTypeCrypto,
	"MANAUSD": AssetTypeCrypto, "ORDIUSD": AssetTypeCrypto, "PENGUUSD": AssetTypeCrypto,
	"PENUSD": AssetTypeCrypto, "PEPEUSD": AssetTypeCrypto, "PYTHUSD": AssetTypeCrypto,
	"RENDERUSD": AssetTypeCrypto, "RONINUSD": AssetTypeCrypto, "SANDUSD": AssetTypeCrypto,
	"SEIUSD": AssetTypeCrypto, "SHIBUSD": AssetTypeCrypto, "STXUSD": AssetTypeCrypto,
	"SUIUSD": AssetTypeCrypto, "TAOUSD": AssetTypeCrypto, "TIAUSD": AssetTypeCrypto,
	"TRON": AssetTypeCrypto, "TRUMPUSD": AssetTypeCrypto, "WIFUSD": AssetTypeCrypto,
	"WLDUSD": AssetTypeCrypto, "FARTCOINUSD": AssetTypeCrypto, "MELANIAUSD": AssetTypeCrypto,

	// ── Stocks OTC
	"OPENAI": AssetTypeStocks, "ANTHROPIC": AssetTypeStocks, "TESLA": AssetTypeStocks,
	"APPLE": AssetTypeStocks, "AMAZON": AssetTypeStocks, "GOOGLE": AssetTypeStocks,
	"MSFT": AssetTypeStocks, "NVDA": AssetTypeStocks, "FB": AssetTypeStocks,
	"SNAP": AssetTypeStocks, "SPACEX": AssetTypeStocks, "PLTR": AssetTypeStocks,
	"AIG": AssetTypeStocks, "ALIBABA": AssetTypeStocks, "BIDU": AssetTypeStocks,
	"CITI": AssetTypeStocks, "COKE": AssetTypeStocks, "GS": AssetTypeStocks,
	"INTEL": AssetTypeStocks, "JPM": AssetTypeStocks, "MCDON": AssetTypeStocks,
	"MORSTAN": AssetTypeStocks, "NIKE": AssetTypeStocks, "VISA": AssetTypeStocks,
	"KLARNA": AssetTypeStocks, "GEV": AssetTypeStocks, "MU": AssetTypeStocks,
	"SNDK": AssetTypeStocks, "WDC": AssetTypeStocks, "FWONA": AssetTypeStocks,

	// ── Indices OTC
	"SP500": AssetTypeIndices, "US30": AssetTypeIndices, "USNDAQ100": AssetTypeIndices,
	"GER30": AssetTypeIndices, "UK100": AssetTypeIndices, "EU50": AssetTypeIndices,
	"JP225": AssetTypeIndices, "AUS200": AssetTypeIndices, "HK33": AssetTypeIndices,
	"FR40": AssetTypeIndices, "US2000": AssetTypeIndices,

	// ── Commodities OTC
	"XAUUSD": AssetTypeCommodities, "XAGUSD": AssetTypeCommodities,
	"USOUSD": AssetTypeCommodities, "UKOUSD": AssetTypeCommodities,
	"XNGUSD": AssetTypeCommodities, "XPDUSD": AssetTypeCommodities,
	"XPTUSD": AssetTypeCommodities, "COCOA": AssetTypeCommodities,
	"COFFEE": AssetTypeCommodities, "COTTON": AssetTypeCommodities,
	"SUGAR": AssetTypeCommodities, "URANIUM": AssetTypeCommodities,
}

// ClassifyAsset returns the type of an asset, stripping -OTC suffix
func ClassifyAsset(asset string) AssetType {
	asset = strings.ToUpper(strings.TrimSuffix(strings.TrimSpace(asset), "-OTC"))
	if t, ok := assetCatalog[asset]; ok {
		return t
	}
	return AssetTypeUnknown
}

// FilterByTypes returns assets that match any of the requested types.
// If types is empty, all assets are returned unchanged.
func FilterByTypes(assets []string, types []string) []string {
	if len(types) == 0 {
		return assets
	}

	// Build a set of requested types
	want := make(map[AssetType]bool, len(types))
	for _, t := range types {
		want[AssetType(strings.ToLower(t))] = true
	}

	filtered := make([]string, 0, len(assets))
	for _, asset := range assets {
		if want[ClassifyAsset(asset)] {
			filtered = append(filtered, asset)
		}
	}
	return filtered
}
