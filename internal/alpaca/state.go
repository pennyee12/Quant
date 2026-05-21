package alpaca

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// RunState is the full persisted local state for the Alpaca paper trading app.
type RunState struct {
	LastRunDate string                  `json:"last_run_date"`
	Positions   map[string]*TickerState `json:"positions"`
	Decisions   []Decision              `json:"decisions"`
	Orders      []OrderEvent            `json:"orders"`
}

// TickerState tracks the isolated $10k portfolio for one ticker.
type TickerState struct {
	Symbol        string        `json:"symbol"`
	InitialCapital float64      `json:"initial_capital"`
	Cash          float64       `json:"cash"`
	Shares        float64       `json:"shares"`
	AvgEntryPrice float64       `json:"avg_entry_price"`
	LastPrice     float64       `json:"last_price"`
	Equity        float64       `json:"equity"`
	PeakEquity    float64       `json:"peak_equity"`
	MaxDrawdown   float64       `json:"max_drawdown"`
	ROI           float64       `json:"roi"`
	TradeCount    int           `json:"trade_count"`
	StartedAt     time.Time     `json:"started_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	Transactions  []Transaction `json:"transactions"`
}

// Transaction records a single executed buy or sell.
type Transaction struct {
	TimeUTC   time.Time `json:"time_utc"`
	Date      string    `json:"date"`
	Side      string    `json:"side"`
	Notional  float64   `json:"notional"`
	Qty       float64   `json:"qty"`
	Price     float64   `json:"price"`
	OrderID   string    `json:"order_id,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	CashAfter float64   `json:"cash_after"`
}

// Decision records the strategy signal and intended action for one ticker on one run.
type Decision struct {
	TimeUTC     time.Time `json:"time_utc"`
	Date        string    `json:"date"`
	Symbol      string    `json:"symbol"`
	Close       float64   `json:"close"`
	Shares      float64   `json:"shares"`
	StockValue  float64   `json:"stock_value"`
	AllocCash   float64   `json:"alloc_cash"`
	Equity      float64   `json:"equity"`
	Target      float64   `json:"target"`
	Signal      float64   `json:"signal"`
	OrderUSD    float64   `json:"order_usd"`
	Action      string    `json:"action"`
	DryRun      bool      `json:"dry_run"`
	RequestID   string    `json:"request_id,omitempty"`
	SubmittedID string    `json:"submitted_id,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// OrderEvent records every submitted Alpaca order with its API response.
type OrderEvent struct {
	TimeUTC   time.Time `json:"time_utc"`
	Date      string    `json:"date"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Notional  float64   `json:"notional,omitempty"`
	Qty       float64   `json:"qty,omitempty"`
	OrderID   string    `json:"order_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func LoadRunState(path string) (*RunState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RunState{
				Positions: make(map[string]*TickerState),
			}, nil
		}
		return nil, err
	}
	var state RunState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if state.Positions == nil {
		state.Positions = make(map[string]*TickerState)
	}
	return &state, nil
}

func (s *RunState) Save(path string) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}

// GetOrInitTicker returns the TickerState for symbol, creating it with
// initialCapital cash if it doesn't exist yet.
func (s *RunState) GetOrInitTicker(symbol string, initialCapital float64) *TickerState {
	if ts, ok := s.Positions[symbol]; ok {
		return ts
	}
	now := time.Now().UTC()
	ts := &TickerState{
		Symbol:         symbol,
		InitialCapital: initialCapital,
		Cash:           initialCapital,
		Shares:         0,
		PeakEquity:     initialCapital,
		Equity:         initialCapital,
		StartedAt:      now,
		UpdatedAt:      now,
		Transactions:   []Transaction{},
	}
	s.Positions[symbol] = ts
	return ts
}

// UpdatePrice refreshes equity, ROI, and max drawdown from the latest price.
func (ts *TickerState) UpdatePrice(price float64) {
	ts.LastPrice = price
	ts.Equity = ts.Cash + ts.Shares*price
	if ts.Equity > ts.PeakEquity {
		ts.PeakEquity = ts.Equity
	}
	if ts.PeakEquity > 0 {
		dd := (ts.PeakEquity - ts.Equity) / ts.PeakEquity
		if dd > ts.MaxDrawdown {
			ts.MaxDrawdown = dd
		}
	}
	if ts.InitialCapital > 0 {
		ts.ROI = (ts.Equity - ts.InitialCapital) / ts.InitialCapital
	}
	ts.UpdatedAt = time.Now().UTC()
}

// RecordBuy deducts cash after a buy order is submitted.
// Shares and avg entry are intentionally left unchanged here — they will be
// corrected on the next run by ReconcileFromAlpaca once Alpaca has the actual fill.
func (ts *TickerState) RecordBuy(date string, notional, lastPrice float64, orderID, requestID string) {
	ts.Cash -= notional
	if ts.Cash < 0 {
		ts.Cash = 0
	}
	ts.TradeCount++
	ts.UpdatedAt = time.Now().UTC()
	ts.Transactions = append(ts.Transactions, Transaction{
		TimeUTC:   time.Now().UTC(),
		Date:      date,
		Side:      "buy",
		Notional:  notional,
		Qty:       0, // unknown until Alpaca fills; reconciled next run
		Price:     lastPrice,
		OrderID:   orderID,
		RequestID: requestID,
		CashAfter: ts.Cash,
	})
}

// ReconcileFromAlpaca corrects shares, avg entry price, and cash using Alpaca's
// actual filled position. Called each run before the strategy executes.
// actualShares and avgEntryPrice come directly from the Alpaca positions API.
func (ts *TickerState) ReconcileFromAlpaca(actualShares, avgEntryPrice float64) {
	if actualShares == ts.Shares {
		return
	}
	// Recompute cash as: initial capital minus what was actually spent on stock
	actualCostBasis := actualShares * avgEntryPrice
	ts.Cash = ts.InitialCapital - actualCostBasis
	if ts.Cash < 0 {
		ts.Cash = 0
	}
	ts.Shares = actualShares
	ts.AvgEntryPrice = avgEntryPrice
	ts.UpdatedAt = time.Now().UTC()
}

// RecordSell credits cash and records the transaction after a sell order is submitted.
func (ts *TickerState) RecordSell(date string, qty, lastPrice float64, orderID, requestID string) {
	proceeds := qty * lastPrice
	ts.Cash += proceeds
	ts.Shares -= qty
	if ts.Shares < 0 {
		ts.Shares = 0
	}
	if ts.Shares == 0 {
		ts.AvgEntryPrice = 0
	}
	ts.TradeCount++
	ts.UpdatedAt = time.Now().UTC()
	ts.Transactions = append(ts.Transactions, Transaction{
		TimeUTC:   time.Now().UTC(),
		Date:      date,
		Side:      "sell",
		Notional:  proceeds,
		Qty:       qty,
		Price:     lastPrice,
		OrderID:   orderID,
		RequestID: requestID,
		CashAfter: ts.Cash,
	})
}

// Summary returns a one-line string for printing.
func (ts *TickerState) Summary() string {
	return fmt.Sprintf(
		"%-6s  cash=$%9.2f  shares=%10.4f  equity=$%9.2f  ROI=%+.2f%%  maxDD=%.2f%%  trades=%d",
		ts.Symbol, ts.Cash, ts.Shares, ts.Equity,
		ts.ROI*100, ts.MaxDrawdown*100, ts.TradeCount,
	)
}

// AccountSummary returns totals across all tickers.
func (s *RunState) AccountSummary() string {
	var totalInitial, totalEquity float64
	for _, ts := range s.Positions {
		totalInitial += ts.InitialCapital
		totalEquity += ts.Equity
	}
	roi := 0.0
	if totalInitial > 0 {
		roi = (totalEquity - totalInitial) / totalInitial * 100
	}
	return fmt.Sprintf(
		"TOTAL  initial=$%.2f  equity=$%.2f  ROI=%+.2f%%  tickers=%d",
		totalInitial, totalEquity, roi, len(s.Positions),
	)
}
