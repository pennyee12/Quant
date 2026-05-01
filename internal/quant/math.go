package quant

import "math"

func ClipFloat64(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func EMA(values []float64, period int) float64 {
	if len(values) == 0 {
		return 0
	}
	if period <= 1 {
		return values[len(values)-1]
	}
	alpha := 2.0 / float64(period+1)
	ema := values[0]
	for _, v := range values[1:] {
		ema = alpha*v + (1-alpha)*ema
	}
	return ema
}

func StdDevLogReturns(closes []float64, period int) float64 {
	if len(closes) < 2 {
		return 0
	}
	start := len(closes) - period - 1
	if start < 0 {
		start = 0
	}
	returns := make([]float64, 0, len(closes)-start-1)
	for i := start + 1; i < len(closes); i++ {
		if closes[i-1] > 0 && closes[i] > 0 {
			returns = append(returns, math.Log(closes[i]/closes[i-1]))
		}
	}
	if len(returns) < 2 {
		return 0
	}
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))
	var ss float64
	for _, r := range returns {
		d := r - mean
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(returns)-1))
}

func MaxDrawdown(equity []float64) float64 {
	var peak, maxDD float64
	for i, v := range equity {
		if i == 0 || v > peak {
			peak = v
		}
		if peak > 0 {
			dd := (peak - v) / peak
			if dd > maxDD {
				maxDD = dd
			}
		}
	}
	return maxDD
}

func SharpeDaily(equity []float64) float64 {
	if len(equity) < 3 {
		return 0
	}
	rets := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1] > 0 && equity[i] > 0 {
			rets = append(rets, math.Log(equity[i]/equity[i-1]))
		}
	}
	if len(rets) < 2 {
		return 0
	}
	var sum float64
	for _, r := range rets {
		sum += r
	}
	mean := sum / float64(len(rets))
	var ss float64
	for _, r := range rets {
		d := r - mean
		ss += d * d
	}
	sd := math.Sqrt(ss / float64(len(rets)-1))
	if sd == 0 {
		return 0
	}
	return mean / sd * math.Sqrt(252)
}
