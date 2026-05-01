package strategy

import (
	"math"

	"github.com/pennyee12/Quant/internal/quant"
)

// Step is the pure strategy entrypoint shared by backtest and future paper/live
// modes. It must never perform I/O, network calls, DB access, or use randomness.
//
// Signal is the external market force. InventoryBias is the spring restoring
// force pulling weight toward 0.5. Beta controls sensitivity. Gamma controls
// the strength of the inventory spring.
func Step(input quant.StrategyInput) quant.StrategyOutput {
	p := input.Params
	closes := input.Closes

	if len(closes) == 0 || input.Portfolio.Equity <= 0 || input.Portfolio.LastPrice <= 0 {
		return quant.StrategyOutput{}
	}

	// Require enough bars for the longest indicator
	minBars := max(p.EMAAnchor, p.TMicro, p.TMacro, p.MomentumBars+1)
	if len(closes) < minBars {
		return quant.StrategyOutput{}
	}

	price := closes[len(closes)-1]

	// ── Signal computation ────────────────────────────────────────────────────

	ema := quant.EMA(closes, p.EMAAnchor)
	macroEMA := quant.EMA(closes, p.TMacro)
	sigma := math.Max(quant.StdDevLogReturns(closes, p.TMicro), p.SigmaFloor)
	if sigma <= 0 || ema <= 0 || price <= 0 {
		return quant.StrategyOutput{}
	}

	// X1: price z-score relative to EMA — positive means price above EMA (sell signal)
	meanReversionSignal := math.Log(price/ema) / sigma

	// X2: momentum — positive return → negative signal (contrarian default, KV sign controls direction)
	momentumSignal := 0.0
	if p.MomentumBars > 0 && len(closes) > p.MomentumBars {
		prev := closes[len(closes)-1-p.MomentumBars]
		if prev > 0 {
			momentumSignal = -math.Log(price/prev) / (sigma * math.Sqrt(float64(p.MomentumBars)))
		}
	}

	// X3: acceleration (change in momentum)
	accelSignal := 0.0
	if p.MomentumBars > 1 && len(closes) > p.MomentumBars*2 {
		nowRet := math.Log(price / closes[len(closes)-1-p.MomentumBars])
		prevRet := math.Log(closes[len(closes)-1-p.MomentumBars] / closes[len(closes)-1-p.MomentumBars*2])
		accelSignal = -(nowRet - prevRet) / (sigma * math.Sqrt(float64(p.MomentumBars)))
	}

	// X4: macro trend relative to long EMA
	trendSignal := 0.0
	if macroEMA > 0 {
		trendSignal = -math.Log(price/macroEMA) / sigma
	}

	// X5: breakout / range position within TDeadline window
	breakoutSignal := 0.0
	if p.TDeadline > 0 && len(closes) > p.TDeadline {
		hi, lo := windowHighLow(closes[len(closes)-p.TDeadline:])
		if hi > lo {
			pos := (price - lo) / (hi - lo)
			breakoutSignal = 1 - 2*pos // +1 at bottom, -1 at top
		}
	}

	// X6: short-term vs long-term volatility ratio
	volSignal := 0.0
	longSigma := math.Max(quant.StdDevLogReturns(closes, p.TMacro), p.SigmaFloor)
	if longSigma > 0 {
		volSignal = sigma/longSigma - 1
	}

	signal := p.KP*meanReversionSignal +
		p.KV*momentumSignal +
		p.KA*accelSignal +
		p.TrendWeight*trendSignal +
		p.BreakoutWeight*breakoutSignal +
		p.VolatilityWeight*volSignal

	// ── Sigmoid target weight ─────────────────────────────────────────────────

	currentWeight := 0.0
	if input.Portfolio.Equity > 0 {
		currentWeight = quant.ClipFloat64(
			input.Portfolio.Shares*input.Portfolio.LastPrice/input.Portfolio.Equity,
			0, 1,
		)
	}

	effectiveBeta := math.Max(0.01, p.Beta*p.SigmoidScale)
	inventoryBias := currentWeight - 0.5
	exponent := effectiveBeta*signal + p.Gamma*inventoryBias

	// Accelerate trimming when signal is a strong sell and we're overweight
	if p.ProfitTrimThreshold > 0 && signal > p.ProfitTrimThreshold && currentWeight > 0.5 {
		exponent += 0.5
	}

	maxWeight := quant.ClipFloat64(p.MaxPositionWeight, 0.01, 1.0)
	targetWeight := quant.ClipFloat64(1/(1+math.Exp(exponent)), 0, maxWeight)

	// ── Order sizing ──────────────────────────────────────────────────────────

	deltaWeight := targetWeight - currentWeight
	theoreticalUSD := deltaWeight * input.Portfolio.Equity
	orderUSD := theoreticalUSD

	// Force a small buy if we've been idle too long (avoids permanent cash trap)
	if p.DeadlineForcePct > 0 && p.TDeadline > 0 &&
		input.Portfolio.BarsSinceTrade > p.TDeadline &&
		deltaWeight > 0 {
		forceAdd := p.DeadlineForcePct * input.Portfolio.Equity
		orderUSD = math.Max(orderUSD, forceAdd)
	}

	// Rebalance cooldown — skip unless delta is urgent
	if input.Portfolio.BarsSinceTrade < p.RebalanceCooldownBars &&
		math.Abs(deltaWeight) < p.UrgentRebalanceWeight {
		orderUSD = 0
	}

	// Deadband filter
	if math.Abs(deltaWeight) < p.MinTradeThreshold || math.Abs(orderUSD) < p.MinTradeUSD {
		orderUSD = 0
	}

	// Cap single trade size
	if orderUSD != 0 && p.MicroReserveRate > 0 {
		maxTrade := input.Portfolio.Equity * quant.ClipFloat64(p.MicroReserveRate, 0, 1)
		orderUSD = quant.ClipFloat64(orderUSD, -maxTrade, maxTrade)
	}

	return quant.StrategyOutput{
		TargetWeight:   targetWeight,
		CurrentWeight:  currentWeight,
		Signal:         signal,
		TheoreticalUSD: theoreticalUSD,
		OrderUSD:       orderUSD,
	}
}

func windowHighLow(values []float64) (float64, float64) {
	hi, lo := values[0], values[0]
	for _, v := range values[1:] {
		if v > hi {
			hi = v
		}
		if v < lo {
			lo = v
		}
	}
	return hi, lo
}
