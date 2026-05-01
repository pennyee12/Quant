package quant

type Bar struct {
	TimeMS int64   `json:"datetime"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int64   `json:"volume"`
}

type PortfolioSnapshot struct {
	Cash           float64
	Shares         float64
	Equity         float64
	LastPrice      float64
	TradeCount     int
	BarsSinceTrade int
}

type StrategyInput struct {
	Closes    []float64
	Portfolio PortfolioSnapshot
	Params    StrategyParams
}

// StrategyParams is the single authoritative parameter set.
// All fields map 1-to-1 from Chromosome.ToParams() — no duplicates.
type StrategyParams struct {
	// Timing windows
	TMacro       int // macro EMA / trend period
	TMicro       int // micro EMA / StdDev period
	TDeadline    int // breakout window + force-rebalance cooldown
	EMAAnchor    int // primary EMA anchor period
	MomentumBars int // momentum lookback (independent of TMicro)

	// Signal weights
	KP               float64 // mean reversion weight
	KV               float64 // momentum weight
	KA               float64 // acceleration weight
	TrendWeight      float64 // macro trend signal weight
	BreakoutWeight   float64 // breakout / range-position weight
	VolatilityWeight float64 // volatility ratio weight

	// Sigmoid parameters
	Beta         float64 // stiffness
	Gamma        float64 // inventory bias (0 = pure signal, >0 = mean-revert weight)
	SigmoidScale float64 // multiplies effective beta
	SigmaFloor   float64 // minimum sigma for z-score denominator

	// Order parameters
	MinTradeThreshold     float64 // deadband: skip trade if |deltaWeight| < this
	MicroReserveRate      float64 // max single trade as fraction of equity
	MaxPositionWeight     float64 // hard cap on target weight
	RebalanceCooldownBars int     // minimum bars between trades
	UrgentRebalanceWeight float64 // bypass cooldown if |deltaWeight| >= this

	// Risk modifiers
	DeadlineForcePct    float64 // fraction of equity to force-add if idle > TDeadline bars
	ProfitTrimThreshold float64 // signal strength at which to accelerate trimming

	// Computed constant — not in chromosome
	MinTradeUSD float64 // minimum trade in absolute USD
}

type StrategyOutput struct {
	TargetWeight   float64
	CurrentWeight  float64
	Signal         float64
	TheoreticalUSD float64
	OrderUSD       float64
}
