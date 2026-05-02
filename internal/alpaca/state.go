package alpaca

import (
	"encoding/json"
	"os"
	"time"
)

type RunState struct {
	LastRunDate string       `json:"last_run_date"`
	Decisions   []Decision   `json:"decisions"`
	Orders      []OrderEvent `json:"orders"`
}

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
			return &RunState{}, nil
		}
		return nil, err
	}
	var state RunState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
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
