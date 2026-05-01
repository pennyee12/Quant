package ga

import (
	"math"

	"quantsaas-local/internal/backtest"
	"quantsaas-local/internal/genome"
	"quantsaas-local/internal/quant"
)

const (
	FatalScore          = -99999.0
	MaxDrawdownFatal    = 0.88
	DefaultWarmupBars   = 200
	DrawdownPenaltyMult = 1.5
)

// Window defines one evaluation slice for the multi-window fitness function.
type Window struct {
	Label  string
	Days   int     // calendar days of eval period; -1 means use all available bars
	Weight float64
}

// EvalWindows matches the design doc: full-history / 5y / 2y / 6m.
// Cascade order is short→long so a fatal in 6m short-circuits the rest.
var EvalWindows = []Window{
	{Label: "6m", Days: 183, Weight: 0.10},
	{Label: "2y", Days: 730, Weight: 0.20},
	{Label: "5y", Days: 1825, Weight: 0.30},
	{Label: "all", Days: -1, Weight: 0.40},
}

// Detail holds per-window scores and aggregate fitness.
type Detail struct {
	ScoreTotal    float64
	WindowScores  map[string]float64
	WindowROIs    map[string]float64
	WindowMaxDDs  map[string]float64
	Fatal         bool
	AvailableWgt  float64 // sum of weights of windows that had enough data
}

// MakeEvaluator returns a scalar EvalFunc suitable for ga.Run.
func MakeEvaluator(symbol string, allBars []quant.Bar, runCfg backtest.RunConfig) EvalFunc {
	return func(c genome.Chromosome) float64 {
		return Evaluate(symbol, allBars, c, runCfg).ScoreTotal
	}
}

// Evaluate runs the multi-window fitness calculation for a single chromosome.
func Evaluate(symbol string, allBars []quant.Bar, c genome.Chromosome, runCfg backtest.RunConfig) Detail {
	params := c.ToParams()
	detail := Detail{
		WindowScores: make(map[string]float64),
		WindowROIs:   make(map[string]float64),
		WindowMaxDDs: make(map[string]float64),
	}

	totalWeightedScore := 0.0
	totalWeight := 0.0

	for _, w := range EvalWindows {
		slice := windowSlice(allBars, w.Days, DefaultWarmupBars)
		if slice == nil {
			detail.WindowScores[w.Label] = 0
			continue
		}

		cfg := runCfg
		cfg.WarmupBars = DefaultWarmupBars

		res := backtest.Run(symbol, slice, params, cfg)

		detail.WindowROIs[w.Label] = res.ROI
		detail.WindowMaxDDs[w.Label] = res.MaxDrawdown

		// Fatal: catastrophic drawdown disqualifies this chromosome
		if res.MaxDrawdown >= MaxDrawdownFatal {
			detail.Fatal = true
			detail.ScoreTotal = FatalScore
			return detail
		}

		alpha := res.ROI - res.GhostDCAROI
		ddPenalty := DrawdownPenaltyMult * math.Max(0, res.MaxDrawdown-res.GhostDCAMaxDD)
		sliceScore := alpha - ddPenalty

		detail.WindowScores[w.Label] = sliceScore
		totalWeightedScore += w.Weight * sliceScore
		totalWeight += w.Weight
	}

	detail.AvailableWgt = totalWeight
	if totalWeight > 0 {
		// Normalize by available weight so skipped windows don't unfairly penalize
		// short-history tickers (e.g. IBIT launched Jan 2024, no 5y window yet)
		detail.ScoreTotal = totalWeightedScore / totalWeight
	}
	return detail
}

// windowSlice returns the bar slice for a given window.
// It prepends warmupBars bars before the eval period.
// Returns nil if there isn't enough history.
func windowSlice(allBars []quant.Bar, evalDays, warmupBars int) []quant.Bar {
	if len(allBars) == 0 {
		return nil
	}

	if evalDays < 0 {
		// "all" window: use everything, but still need minimum bars
		if len(allBars) < warmupBars+20 {
			return nil
		}
		return allBars
	}

	need := evalDays + warmupBars
	if len(allBars) < need {
		return nil
	}
	return allBars[len(allBars)-need:]
}
