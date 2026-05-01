package backtest

import (
	"time"

	"github.com/pennyee12/Quant/internal/quant"
)

type GhostDCAResult struct {
	FinalEquity float64
	ROI         float64
	MaxDrawdown float64
	TotalCashIn float64
}

func SimulateGhostDCA(bars []quant.Bar, initialCapital, monthlyInject, expenseRatio float64) GhostDCAResult {
	if len(bars) == 0 || initialCapital <= 0 || bars[0].Open <= 0 {
		return GhostDCAResult{}
	}
	cashIn := initialCapital
	shares := initialCapital / bars[0].Open
	var lastMonth time.Month
	var lastYear int
	curve := make([]float64, 0, len(bars))

	for i, bar := range bars {
		if bar.Close <= 0 {
			continue
		}
		t := time.UnixMilli(bar.TimeMS)
		if i > 0 && monthlyInject > 0 && (t.Month() != lastMonth || t.Year() != lastYear) {
			shares += monthlyInject / bar.Open
			cashIn += monthlyInject
		}
		lastMonth = t.Month()
		lastYear = t.Year()

		if expenseRatio > 0 && shares > 0 {
			shares *= 1 - expenseRatio/252.0
		}
		curve = append(curve, shares*bar.Close)
	}
	if len(curve) == 0 {
		return GhostDCAResult{}
	}
	final := curve[len(curve)-1]
	return GhostDCAResult{
		FinalEquity: final,
		ROI:         final/cashIn - 1,
		MaxDrawdown: quant.MaxDrawdown(curve),
		TotalCashIn: cashIn,
	}
}
