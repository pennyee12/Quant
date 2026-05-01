package paper

import (
	"encoding/json"
	"math"
	"os"
	"time"
)

// Position holds the current state of one paper-traded ticker.
type Position struct {
	Symbol         string    `json:"symbol"`
	Shares         float64   `json:"shares"`
	Cash           float64   `json:"cash"`
	InitialCapital float64   `json:"initial_capital"`
	LastPrice      float64   `json:"last_price"`
	Equity         float64   `json:"equity"`
	ROI            float64   `json:"roi"`
	TradeCount     int       `json:"trade_count"`
	BarsSinceTrade int       `json:"bars_since_trade"`
	PeakEquity     float64   `json:"peak_equity"`
	MaxDrawdown    float64   `json:"max_drawdown"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// DailyRecord is one row in the history log.
type DailyRecord struct {
	Date      string  `json:"date"`
	Symbol    string  `json:"symbol"`
	Close     float64 `json:"close"`
	Equity    float64 `json:"equity"`
	ROI       float64 `json:"roi"`
	Signal    float64 `json:"signal"`
	Target    float64 `json:"target"`
	OrderUSD  float64 `json:"order_usd"`
	Trades    int     `json:"trades"`
	MaxDD     float64 `json:"max_dd"`
}

// State is the full persisted paper trading state.
type State struct {
	Positions []Position    `json:"positions"`
	History   []DailyRecord `json:"history"`
}

func Load(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (s *State) Save(path string) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

// FindPosition returns the position for a symbol, or a fresh one.
func (s *State) FindPosition(symbol string, initialCapital float64) *Position {
	for i := range s.Positions {
		if s.Positions[i].Symbol == symbol {
			return &s.Positions[i]
		}
	}
	s.Positions = append(s.Positions, Position{
		Symbol:         symbol,
		Cash:           initialCapital,
		InitialCapital: initialCapital,
		PeakEquity:     initialCapital,
		StartedAt:      time.Now().UTC(),
	})
	return &s.Positions[len(s.Positions)-1]
}

// UpdateMetrics recalculates equity, ROI, and max drawdown after a bar.
func (p *Position) UpdateMetrics(price float64) {
	p.LastPrice = price
	p.Equity = p.Cash + p.Shares*price
	p.ROI = p.Equity/p.InitialCapital - 1
	if p.Equity > p.PeakEquity {
		p.PeakEquity = p.Equity
	}
	if p.PeakEquity > 0 {
		dd := 1 - p.Equity/p.PeakEquity
		p.MaxDrawdown = math.Max(p.MaxDrawdown, dd)
	}
	p.UpdatedAt = time.Now().UTC()
}
