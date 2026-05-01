package genome

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"

	"quantsaas-local/internal/quant"
)

// Chromosome holds the 20 evolvable genes for the Sigmoid dynamic-balance strategy.
// All fields are float64; integer-semantic genes are rounded in ToParams().
type Chromosome struct {
	// Timing windows
	TMacro       float64 `json:"t_macro"`        // [8, 200]  macro EMA / trend period
	TMicro       float64 `json:"t_micro"`        // [3, 60]   micro EMA / StdDev period
	TDeadline    float64 `json:"t_deadline"`     // [20, 252] breakout window
	EMAAnchor    float64 `json:"ema_anchor"`     // [8, 200]  primary EMA anchor
	MomentumBars float64 `json:"momentum_bars"`  // [3, 60]   momentum lookback

	// Signal weights
	KP               float64 `json:"kp"`                // [-2.5, 2.5]
	KV               float64 `json:"kv"`                // [-2.5, 2.5]
	KA               float64 `json:"ka"`                // [-2.5, 2.5]
	TrendWeight      float64 `json:"trend_weight"`      // [-2.0, 2.0]
	BreakoutWeight   float64 `json:"breakout_weight"`   // [-2.0, 2.0]
	VolatilityWeight float64 `json:"volatility_weight"` // [-2.0, 2.0]

	// Sigmoid parameters
	Beta         float64 `json:"beta"`          // [0.25, 6.0]
	Gamma        float64 `json:"gamma"`         // [-1.5, 2.5]
	SigmoidScale float64 `json:"sigmoid_scale"` // [0.25, 4.0]
	SigmaFloor   float64 `json:"sigma_floor"`   // [0.001, 0.05]

	// Order parameters
	MinTradeThreshold float64 `json:"min_trade_threshold"` // [0.02, 0.25]
	MicroReserveRate  float64 `json:"micro_reserve_rate"`  // [0.05, 0.60]
	MaxPositionWeight float64 `json:"max_position_weight"` // [0.15, 1.0]

	// Risk modifiers
	DeadlineForcePct    float64 `json:"deadline_force_pct"`    // [0.0, 0.30]
	ProfitTrimThreshold float64 `json:"profit_trim_threshold"` // [0.05, 1.50]
}

type Bound struct {
	Min  float64
	Max  float64
	Step float64
}

var Bounds = map[string]Bound{
	"TMacro":              {8, 200, 8},
	"TMicro":              {3, 60, 4},
	"TDeadline":           {20, 252, 12},
	"EMAAnchor":           {8, 200, 10},
	"MomentumBars":        {3, 60, 4},
	"KP":                  {-2.5, 2.5, 0.25},
	"KV":                  {-2.5, 2.5, 0.25},
	"KA":                  {-2.5, 2.5, 0.25},
	"TrendWeight":         {-2.0, 2.0, 0.25},
	"BreakoutWeight":      {-2.0, 2.0, 0.25},
	"VolatilityWeight":    {-2.0, 2.0, 0.25},
	"Beta":                {0.25, 6.0, 0.25},
	"Gamma":               {-1.5, 2.5, 0.25},
	"SigmoidScale":        {0.25, 4.0, 0.25},
	"SigmaFloor":          {0.001, 0.05, 0.005},
	"MinTradeThreshold":   {0.02, 0.25, 0.02},
	"MicroReserveRate":    {0.05, 0.60, 0.05},
	"MaxPositionWeight":   {0.15, 1.0, 0.05},
	"DeadlineForcePct":    {0.0, 0.30, 0.03},
	"ProfitTrimThreshold": {0.05, 1.50, 0.10},
}

// Default is the conservative seed chromosome used when no elite is available.
var Default = Chromosome{
	TMacro:              55,
	TMicro:              21,
	TDeadline:           126,
	EMAAnchor:           89,
	MomentumBars:        10,
	KP:                  0.70,
	KV:                  0.30,
	KA:                  0.10,
	TrendWeight:         0.30,
	BreakoutWeight:      0.10,
	VolatilityWeight:    0.10,
	Beta:                1.35,
	Gamma:               0.70,
	SigmoidScale:        1.0,
	SigmaFloor:          0.005,
	MinTradeThreshold:   0.08,
	MicroReserveRate:    0.25,
	MaxPositionWeight:   0.95,
	DeadlineForcePct:    0.05,
	ProfitTrimThreshold: 0.60,
}

func Sample(rng *rand.Rand) Chromosome {
	c := Chromosome{}
	for name, ptr := range values(&c) {
		b := Bounds[name]
		*ptr = b.Min + rng.Float64()*(b.Max-b.Min)
	}
	return Clamp(c)
}

func Mutate(c Chromosome, prob, scale float64, rng *rand.Rand) Chromosome {
	for name, ptr := range values(&c) {
		if rng.Float64() >= prob {
			continue
		}
		b := Bounds[name]
		*ptr += rng.NormFloat64() * b.Step * scale
	}
	return Clamp(c)
}

func Crossover(a, b Chromosome, rng *rand.Rand) Chromosome {
	child := a
	bVals := values(&b)
	for name, ptr := range values(&child) {
		if rng.Float64() < 0.5 {
			*ptr = *bVals[name]
		}
	}
	return Clamp(child)
}

// Clamp enforces hard bounds and structural constraints.
func Clamp(c Chromosome) Chromosome {
	for name, ptr := range values(&c) {
		b := Bounds[name]
		*ptr = quant.ClipFloat64(*ptr, b.Min, b.Max)
	}
	// TMicro must be shorter than TMacro
	if c.TMicro >= c.TMacro {
		c.TMicro = math.Max(Bounds["TMicro"].Min, c.TMacro*0.4)
	}
	// MomentumBars must not exceed TMicro
	if c.MomentumBars > c.TMicro {
		c.MomentumBars = c.TMicro
	}
	// EMAAnchor should be within [TMicro, TMacro]
	c.EMAAnchor = quant.ClipFloat64(c.EMAAnchor, c.TMicro, c.TMacro)
	// TDeadline >= TMacro
	if c.TDeadline < c.TMacro {
		c.TDeadline = c.TMacro
	}
	return c
}

// ToParams converts the chromosome to strategy parameters with clean 1-to-1 mapping.
func (c Chromosome) ToParams() quant.StrategyParams {
	c = Clamp(c)
	tmicro := int(math.Round(c.TMicro))
	return quant.StrategyParams{
		TMacro:                int(math.Round(c.TMacro)),
		TMicro:                tmicro,
		TDeadline:             int(math.Round(c.TDeadline)),
		EMAAnchor:             int(math.Round(c.EMAAnchor)),
		MomentumBars:          int(math.Round(c.MomentumBars)),
		KP:                    c.KP,
		KV:                    c.KV,
		KA:                    c.KA,
		TrendWeight:           c.TrendWeight,
		BreakoutWeight:        c.BreakoutWeight,
		VolatilityWeight:      c.VolatilityWeight,
		Beta:                  c.Beta,
		Gamma:                 c.Gamma,
		SigmoidScale:          c.SigmoidScale,
		SigmaFloor:            c.SigmaFloor,
		MinTradeThreshold:     c.MinTradeThreshold,
		MicroReserveRate:      c.MicroReserveRate,
		MaxPositionWeight:     c.MaxPositionWeight,
		RebalanceCooldownBars: max(1, tmicro/4),
		UrgentRebalanceWeight: c.MinTradeThreshold * 2.5,
		DeadlineForcePct:      c.DeadlineForcePct,
		ProfitTrimThreshold:   c.ProfitTrimThreshold,
		MinTradeUSD:           10.0,
	}
}

func Save(path string, c Chromosome) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(Clamp(c), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func Load(path string) (Chromosome, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Chromosome{}, err
	}
	var c Chromosome
	if err := json.Unmarshal(raw, &c); err != nil {
		return Chromosome{}, err
	}
	return Clamp(c), nil
}

func values(c *Chromosome) map[string]*float64 {
	return map[string]*float64{
		"TMacro":              &c.TMacro,
		"TMicro":              &c.TMicro,
		"TDeadline":           &c.TDeadline,
		"EMAAnchor":           &c.EMAAnchor,
		"MomentumBars":        &c.MomentumBars,
		"KP":                  &c.KP,
		"KV":                  &c.KV,
		"KA":                  &c.KA,
		"TrendWeight":         &c.TrendWeight,
		"BreakoutWeight":      &c.BreakoutWeight,
		"VolatilityWeight":    &c.VolatilityWeight,
		"Beta":                &c.Beta,
		"Gamma":               &c.Gamma,
		"SigmoidScale":        &c.SigmoidScale,
		"SigmaFloor":          &c.SigmaFloor,
		"MinTradeThreshold":   &c.MinTradeThreshold,
		"MicroReserveRate":    &c.MicroReserveRate,
		"MaxPositionWeight":   &c.MaxPositionWeight,
		"DeadlineForcePct":    &c.DeadlineForcePct,
		"ProfitTrimThreshold": &c.ProfitTrimThreshold,
	}
}
