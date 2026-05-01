package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Schwab   SchwabConfig   `yaml:"schwab"`
	Backtest BacktestConfig `yaml:"backtest"`
	Strategy StrategyConfig `yaml:"strategy"`
	Tickers  []TickerConfig `yaml:"tickers"`
}

type SchwabConfig struct {
	TokenFile        string `yaml:"token_file"`
	APIBaseURL       string `yaml:"api_base_url"`
	TokenURL         string `yaml:"token_url"`
	CredentialScript string `yaml:"credential_script"`
}

type BacktestConfig struct {
	InitialCapital float64 `yaml:"initial_capital"`
	MonthlyInject  float64 `yaml:"monthly_inject"`
	FeeBPS         float64 `yaml:"fee_bps"`
	SlippageBPS    float64 `yaml:"slippage_bps"`
	PeriodType     string  `yaml:"period_type"`
	Period         int     `yaml:"period"`
	FrequencyType  string  `yaml:"frequency_type"`
	Frequency      int     `yaml:"frequency"`
	StartDate      string  `yaml:"start_date"`
	EndDate        string  `yaml:"end_date"`
	FillModel      string  `yaml:"fill_model"`
	CacheDir       string  `yaml:"cache_dir"`
}

type StrategyConfig struct {
	EMABars               int     `yaml:"ema_bars"`
	StdDevBars            int     `yaml:"stddev_bars"`
	MomentumBars          int     `yaml:"momentum_bars"`
	MeanReversionWeight   float64 `yaml:"mean_reversion_weight"`
	MomentumWeight        float64 `yaml:"momentum_weight"`
	Beta                  float64 `yaml:"beta"`
	Gamma                 float64 `yaml:"gamma"`
	SigmaFloor            float64 `yaml:"sigma_floor"`
	MinTradeUSD           float64 `yaml:"min_trade_usd"`
	TargetDeadbandWeight  float64 `yaml:"target_deadband_weight"`
	MaxPositionWeight     float64 `yaml:"max_position_weight"`
	MaxTradePctEquity     float64 `yaml:"max_trade_pct_equity"`
	RebalanceCooldownBars int     `yaml:"rebalance_cooldown_bars"`
	UrgentRebalanceWeight float64 `yaml:"urgent_rebalance_weight"`
}

type TickerConfig struct {
	Symbol       string  `yaml:"symbol"`
	Type         string  `yaml:"type"`
	ExpenseRatio float64 `yaml:"expense_ratio"`
	Enabled      *bool   `yaml:"enabled"`
	Note         string  `yaml:"note"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func (c Config) EnabledTickers() []TickerConfig {
	out := make([]TickerConfig, 0, len(c.Tickers))
	for _, t := range c.Tickers {
		if t.Enabled != nil && !*t.Enabled {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (c BacktestConfig) StartTime() (time.Time, error) {
	return time.Parse("2006-01-02", c.StartDate)
}

func (c BacktestConfig) EndTime() (time.Time, error) {
	if c.EndDate == "" {
		return time.Now(), nil
	}
	return time.Parse("2006-01-02", c.EndDate)
}

func applyDefaults(cfg *Config) {
	if cfg.Schwab.APIBaseURL == "" {
		cfg.Schwab.APIBaseURL = "https://api.schwabapi.com"
	}
	if cfg.Schwab.TokenURL == "" {
		cfg.Schwab.TokenURL = "https://api.schwabapi.com/v1/oauth/token"
	}
	if cfg.Backtest.InitialCapital == 0 {
		cfg.Backtest.InitialCapital = 10000
	}
	if cfg.Backtest.FrequencyType == "" {
		cfg.Backtest.FrequencyType = "daily"
	}
	if cfg.Backtest.PeriodType == "" {
		cfg.Backtest.PeriodType = "year"
	}
	if cfg.Backtest.Period == 0 {
		cfg.Backtest.Period = 20
	}
	if cfg.Backtest.Frequency == 0 {
		cfg.Backtest.Frequency = 1
	}
	if cfg.Backtest.StartDate == "" {
		cfg.Backtest.StartDate = "2016-01-01"
	}
	if cfg.Backtest.CacheDir == "" {
		cfg.Backtest.CacheDir = "data/bars"
	}
	if cfg.Strategy.EMABars == 0 {
		cfg.Strategy.EMABars = 21
	}
	if cfg.Strategy.StdDevBars == 0 {
		cfg.Strategy.StdDevBars = 21
	}
	if cfg.Strategy.MomentumBars == 0 {
		cfg.Strategy.MomentumBars = 10
	}
	if cfg.Strategy.Beta == 0 {
		cfg.Strategy.Beta = 1.35
	}
	if cfg.Strategy.SigmaFloor == 0 {
		cfg.Strategy.SigmaFloor = 0.005
	}
	if cfg.Strategy.MinTradeUSD == 0 {
		cfg.Strategy.MinTradeUSD = 10.10
	}
	if cfg.Strategy.TargetDeadbandWeight == 0 {
		cfg.Strategy.TargetDeadbandWeight = 0.08
	}
	if cfg.Strategy.MaxPositionWeight == 0 {
		cfg.Strategy.MaxPositionWeight = 0.95
	}
	if cfg.Strategy.MaxTradePctEquity == 0 {
		cfg.Strategy.MaxTradePctEquity = 0.25
	}
	if cfg.Strategy.RebalanceCooldownBars == 0 {
		cfg.Strategy.RebalanceCooldownBars = 5
	}
	if cfg.Strategy.UrgentRebalanceWeight == 0 {
		cfg.Strategy.UrgentRebalanceWeight = 0.20
	}
}
