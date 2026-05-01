package backtest

import (
	"math"
	"time"

	"quantsaas-local/internal/quant"
	"quantsaas-local/internal/strategy"
)

type Result struct {
	Symbol           string
	Bars             int
	Start            time.Time
	End              time.Time
	InitialCapital   float64
	FinalEquity      float64
	ROI              float64
	BuyHoldROI       float64
	GhostDCAROI      float64
	Alpha            float64
	AlphaDCA         float64
	MaxDrawdown      float64
	BuyHoldMaxDD     float64
	GhostDCAMaxDD    float64
	Sharpe           float64
	Trades           int
	FeesPaid         float64
	SlippagePaid     float64
	ExpenseRatio     float64
	LastTargetWeight float64
	LastSignal       float64
}

type RunConfig struct {
	InitialCapital float64
	MonthlyInject  float64
	ExpenseRatio   float64
	FeeBPS         float64
	SlippageBPS    float64
	// WarmupBars: bars at the start of the slice used only for indicator warmup.
	// No trades execute and no equity is tracked during warmup.
	// Buy-and-hold and GhostDCA baselines start from bar WarmupBars, not bar 0.
	WarmupBars int
}

func Run(symbol string, bars []quant.Bar, params quant.StrategyParams, cfg RunConfig) Result {
	initialCapital := cfg.InitialCapital
	warmup := cfg.WarmupBars
	res := Result{Symbol: symbol, InitialCapital: initialCapital, ExpenseRatio: cfg.ExpenseRatio}

	if len(bars) < max(warmup+2, 40) || initialCapital <= 0 {
		return res
	}

	evalBars := bars[warmup:]
	if len(evalBars) < 2 {
		return res
	}
	res.Start = time.UnixMilli(evalBars[0].TimeMS)
	res.End = time.UnixMilli(evalBars[len(evalBars)-1].TimeMS)
	res.Bars = len(evalBars)

	cash := initialCapital
	shares := 0.0

	// Buy-and-hold baseline: buy at first eval bar's open
	buyHoldShares := 0.0
	if evalBars[0].Open > 0 {
		buyHoldShares = initialCapital / evalBars[0].Open
	}

	closes := make([]float64, 0, len(bars))
	barsSinceTrade := params.RebalanceCooldownBars
	equityCurve := make([]float64, 0, len(evalBars))
	buyHoldCurve := make([]float64, 0, len(evalBars))

	for i := 0; i < len(bars)-1; i++ {
		bar := bars[i]
		next := bars[i+1]

		if bar.Close > 0 {
			closes = append(closes, bar.Close)
		}

		if i < warmup {
			// Warmup: accumulate closes for indicator seeding, no trading.
			continue
		}

		if bar.Close <= 0 || next.Open <= 0 {
			// Record flat equity on bad data
			if len(equityCurve) > 0 {
				equityCurve = append(equityCurve, equityCurve[len(equityCurve)-1])
				buyHoldCurve = append(buyHoldCurve, buyHoldCurve[len(buyHoldCurve)-1])
			}
			continue
		}

		equity := cash + shares*bar.Close
		out := strategy.Step(quant.StrategyInput{
			Closes: closes,
			Portfolio: quant.PortfolioSnapshot{
				Cash:           cash,
				Shares:         shares,
				Equity:         equity,
				LastPrice:      bar.Close,
				TradeCount:     res.Trades,
				BarsSinceTrade: barsSinceTrade,
			},
			Params: params,
		})
		res.LastTargetWeight = out.TargetWeight
		res.LastSignal = out.Signal

		// Fill at next bar's open
		if out.OrderUSD > 0 {
			spend := math.Min(out.OrderUSD, cash)
			if spend >= params.MinTradeUSD {
				fill := next.Open * (1 + cfg.SlippageBPS/10000.0)
				fee := spend * cfg.FeeBPS / 10000.0
				netSpend := math.Min(spend, math.Max(0, cash-fee))
				shares += netSpend / fill
				cash -= netSpend + fee
				res.FeesPaid += fee
				res.SlippagePaid += (fill - next.Open) * (netSpend / fill)
				res.Trades++
				barsSinceTrade = 0
			}
		} else if out.OrderUSD < 0 {
			wantSell := math.Min(-out.OrderUSD/next.Open, shares)
			if wantSell*next.Open >= params.MinTradeUSD {
				fill := next.Open * (1 - cfg.SlippageBPS/10000.0)
				gross := wantSell * fill
				fee := gross * cfg.FeeBPS / 10000.0
				shares -= wantSell
				cash += gross - fee
				res.FeesPaid += fee
				res.SlippagePaid += (next.Open - fill) * wantSell
				res.Trades++
				barsSinceTrade = 0
			}
		}

		if barsSinceTrade < params.RebalanceCooldownBars {
			barsSinceTrade++
		}

		if cfg.ExpenseRatio > 0 && shares > 0 {
			shares *= math.Max(0, 1-cfg.ExpenseRatio/252.0)
		}

		equityCurve = append(equityCurve, cash+shares*next.Close)
		buyHoldCurve = append(buyHoldCurve, buyHoldShares*next.Close)
	}

	if len(equityCurve) == 0 {
		return res
	}

	res.FinalEquity = equityCurve[len(equityCurve)-1]
	res.ROI = res.FinalEquity/initialCapital - 1
	res.MaxDrawdown = quant.MaxDrawdown(equityCurve)
	res.Sharpe = quant.SharpeDaily(equityCurve)

	buyHoldFinal := buyHoldCurve[len(buyHoldCurve)-1]
	res.BuyHoldROI = buyHoldFinal/initialCapital - 1
	res.BuyHoldMaxDD = quant.MaxDrawdown(buyHoldCurve)
	res.Alpha = res.ROI - res.BuyHoldROI

	// GhostDCA baseline starts from the first eval bar (not warmup bars)
	ghost := SimulateGhostDCA(evalBars, initialCapital, cfg.MonthlyInject, cfg.ExpenseRatio)
	res.GhostDCAROI = ghost.ROI
	res.GhostDCAMaxDD = ghost.MaxDrawdown
	res.AlphaDCA = res.ROI - ghost.ROI

	return res
}
