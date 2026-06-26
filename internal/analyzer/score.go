package analyzer

import (
	"fmt"

	"signal-bot/internal/indicators"
	"signal-bot/internal/wstrader"
)

// Indicator weights - higher weight = more influence on final score
const (
	weightRSI       = 1.5 // RSI at extreme = high conviction
	weightRSIExtreme = 2.5 // RSI < 20 or > 80
	weightEMACross  = 1.0
	weightEMAAlign  = 0.5 // weak on 1m, noisy
	weightBB        = 1.2
	weightMACD      = 0.8
	weightEngulfing = 1.3
	weightPinBar    = 1.1
	weightMTF5m     = 1.5
	weightMTF15m    = 2.0 // highest - higher TF has more authority
	weightVolume    = 0.3
	weightStoch     = 1.0
	weightADX       = 1.5
)

// ScoreInput bundles all data needed for scoring
type ScoreInput struct {
	Closes, Opens, Highs, Lows, Vols []float64
	Candles5m, Candles15m            []wstrader.Candle
}

// ScoreMeta holds key indicator values for logging
type ScoreMeta struct {
	RSI     float64
	ADX     float64
	BBWidth float64
	StochK  float64
}

// ScoreOutput is the result of ComputeWeightedScore
type ScoreOutput struct {
	Score   float64
	Reasons []string
	Meta    ScoreMeta
}

// ComputeWeightedScore is the single source of truth for signal scoring.
// Used by both AnalyzeAsset (live) and BacktestAsset.
func ComputeWeightedScore(in ScoreInput, cfg AnalyzerConfig) ScoreOutput {
	out := ScoreOutput{Reasons: []string{}}

	if len(in.Closes) < cfg.SlowMAPeriod+5 {
		return out
	}

	closes := in.Closes
	opens  := in.Opens
	highs  := in.Highs
	lows   := in.Lows
	vols   := in.Vols

	// ── ADX: gate ranging markets before anything else
	adx, diPlus, diMinus := indicators.ADX(highs, lows, closes, 14)
	out.Meta.ADX = adx

	if adx < 15 {
		// Market is ranging - no reliable signals
		out.Reasons = append(out.Reasons, fmt.Sprintf("ADX=%.1f (ranging, skip)", adx))
		out.Score = 0
		return out
	}

	// ── RSI (Wilder's method)
	rsi := indicators.RSI(closes, cfg.RSIPeriod)
	out.Meta.RSI = rsi

	if rsi < 20 {
		out.Score += weightRSIExtreme
		out.Reasons = append(out.Reasons, fmt.Sprintf("RSI extremely oversold (%.1f)", rsi))
	} else if rsi < cfg.RSIOversold {
		out.Score += weightRSI
		out.Reasons = append(out.Reasons, fmt.Sprintf("RSI oversold (%.1f)", rsi))
	} else if rsi > 80 {
		out.Score -= weightRSIExtreme
		out.Reasons = append(out.Reasons, fmt.Sprintf("RSI extremely overbought (%.1f)", rsi))
	} else if rsi > cfg.RSIOverbought {
		out.Score -= weightRSI
		out.Reasons = append(out.Reasons, fmt.Sprintf("RSI overbought (%.1f)", rsi))
	}

	// ── Stochastic confirmation
	stochK, stochD := indicators.Stochastic(highs, lows, closes, 14, 3)
	out.Meta.StochK = stochK
	if stochK < 20 && rsi < 40 {
		out.Score += weightStoch
		out.Reasons = append(out.Reasons, fmt.Sprintf("Stoch+RSI oversold (K=%.1f)", stochK))
	} else if stochK > 80 && rsi > 60 {
		out.Score -= weightStoch
		out.Reasons = append(out.Reasons, fmt.Sprintf("Stoch+RSI overbought (K=%.1f)", stochK))
	}
	// Stoch crossover
	if stochK > stochD && stochK < 50 {
		out.Score += weightStoch * 0.5
	} else if stochK < stochD && stochK > 50 {
		out.Score -= weightStoch * 0.5
	}

	// ── EMA crossover + alignment
	fastMA     := indicators.EMA(closes, cfg.FastMAPeriod)
	slowMA     := indicators.EMA(closes, cfg.SlowMAPeriod)
	fastMAPrev := indicators.EMA(closes[:len(closes)-1], cfg.FastMAPeriod)
	slowMAPrev := indicators.EMA(closes[:len(closes)-1], cfg.SlowMAPeriod)

	if indicators.IsBullishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		out.Score += weightEMACross
		out.Reasons = append(out.Reasons, "EMA bullish crossover")
	} else if indicators.IsBearishCrossover(fastMAPrev, slowMAPrev, fastMA, slowMA) {
		out.Score -= weightEMACross
		out.Reasons = append(out.Reasons, "EMA bearish crossover")
	} else if fastMA > slowMA && rsi < 50 {
		out.Score += weightEMAAlign
	} else if fastMA < slowMA && rsi > 50 {
		out.Score -= weightEMAAlign
	}

	// ── ADX directional weight (only when trending)
	if adx > 25 {
		if diPlus > diMinus {
			out.Score += weightADX * 0.5
			out.Reasons = append(out.Reasons, fmt.Sprintf("ADX trending bullish (%.1f)", adx))
		} else {
			out.Score -= weightADX * 0.5
			out.Reasons = append(out.Reasons, fmt.Sprintf("ADX trending bearish (%.1f)", adx))
		}
	}

	// ── Bollinger Bands
	bbMid, bbUpper, bbLower := indicators.BollingerBands(closes, cfg.BBPeriod, cfg.BBStdDev)
	bbWidth := indicators.BandWidth(bbMid, bbUpper, bbLower)
	out.Meta.BBWidth = bbWidth
	currentPrice := closes[len(closes)-1]

	if currentPrice <= bbLower {
		out.Score += weightBB
		out.Reasons = append(out.Reasons, fmt.Sprintf("Price at lower BB (w=%.4f%%)", bbWidth))
	} else if currentPrice >= bbUpper {
		out.Score -= weightBB
		out.Reasons = append(out.Reasons, fmt.Sprintf("Price at upper BB (w=%.4f%%)", bbWidth))
	}

	isSqueeze := bbWidth < 0.05
	if isSqueeze {
		out.Reasons = append(out.Reasons, fmt.Sprintf("BB squeeze (w=%.4f%%)", bbWidth))
	}

	// ── MACD (single-pass O(n))
	macdLine, macdSignal, macdHist := indicators.MACDSeries(closes, 12, 26, 9)
	if macdHist > 0 && macdLine > macdSignal {
		out.Score += weightMACD
		out.Reasons = append(out.Reasons, fmt.Sprintf("MACD bullish (h=%.5f)", macdHist))
	} else if macdHist < 0 && macdLine < macdSignal {
		out.Score -= weightMACD
		out.Reasons = append(out.Reasons, fmt.Sprintf("MACD bearish (h=%.5f)", macdHist))
	}

	// ── Volume
	avgVol  := indicators.AvgVolume(vols, cfg.VolumePeriod)
	lastVol := vols[len(vols)-1]
	if avgVol > 0 && lastVol >= avgVol*cfg.VolumeMultiplier {
		// Volume confirms direction but doesn't add directional score
		out.Reasons = append(out.Reasons, fmt.Sprintf("Volume surge (%.1fx)", lastVol/avgVol))
	}

	// ── Candlestick patterns
	if indicators.IsBullishEngulfing(opens, closes) {
		out.Score += weightEngulfing
		out.Reasons = append(out.Reasons, "Bullish engulfing")
	} else if indicators.IsBearishEngulfing(opens, closes) {
		out.Score -= weightEngulfing
		out.Reasons = append(out.Reasons, "Bearish engulfing")
	}
	n := len(opens)
	if n > 0 {
		if indicators.IsBullishPinBar(opens[n-1], highs[n-1], lows[n-1], closes[n-1]) {
			out.Score += weightPinBar
			out.Reasons = append(out.Reasons, "Bullish pin bar")
		} else if indicators.IsBearishPinBar(opens[n-1], highs[n-1], lows[n-1], closes[n-1]) {
			out.Score -= weightPinBar
			out.Reasons = append(out.Reasons, "Bearish pin bar")
		}
	}

	// ── MTF 5m (only counts if doesn't contradict RSI extreme)
	t5 := 0
	if len(in.Candles5m) >= 20 {
		c5 := extractField(in.Candles5m, func(c wstrader.Candle) float64 { return c.Close })
		t5 = indicators.Trend(c5, 8, 21)
		rsiExtreme := rsi < cfg.RSIOversold || rsi > cfg.RSIOverbought
		if !rsiExtreme || (rsiExtreme && t5 > 0 && rsi < cfg.RSIOversold) || (rsiExtreme && t5 < 0 && rsi > cfg.RSIOverbought) {
			if t5 > 0 {
				out.Score += weightMTF5m
				out.Reasons = append(out.Reasons, "5m trend: BULLISH")
			} else if t5 < 0 {
				out.Score -= weightMTF5m
				out.Reasons = append(out.Reasons, "5m trend: BEARISH")
			}
		}
	}

	// ── MTF 15m
	t15 := 0
	if len(in.Candles15m) >= 20 {
		c15 := extractField(in.Candles15m, func(c wstrader.Candle) float64 { return c.Close })
		t15 = indicators.Trend(c15, 8, 21)
		rsiExtreme := rsi < cfg.RSIOversold || rsi > cfg.RSIOverbought
		if !rsiExtreme || (rsiExtreme && t15 > 0 && rsi < cfg.RSIOversold) || (rsiExtreme && t15 < 0 && rsi > cfg.RSIOverbought) {
			if t15 > 0 {
				out.Score += weightMTF15m
				out.Reasons = append(out.Reasons, "15m trend: BULLISH")
			} else if t15 < 0 {
				out.Score -= weightMTF15m
				out.Reasons = append(out.Reasons, "15m trend: BEARISH")
			}
		}
	}

	// MTF conflict: 5m and 15m disagree → zero out their contribution
	if t5 != 0 && t15 != 0 && t5 != t15 {
		// Remove MTF weight from score
		if t5 > 0 {
			out.Score -= weightMTF5m
		} else {
			out.Score += weightMTF5m
		}
		if t15 > 0 {
			out.Score -= weightMTF15m
		} else {
			out.Score += weightMTF15m
		}
		out.Reasons = append(out.Reasons, "MTF conflict (5m/15m disagree)")
	}

	return out
}
