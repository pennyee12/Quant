// paper runs one daily paper-trading step for all configured tickers.
//
// Each run does two things in order:
//  1. Fill yesterday's pending order at today's open price.
//  2. Compute today's signal on today's close and store a new pending order.
//
// This matches the backtest exactly: signal on bar[i] close → fill at bar[i+1] open.
//
// Usage:
//
//	go run ./cmd/paper [-config config.yaml] [-state paper_state.json] [-out docs/data.json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"quantsaas-local/internal/backtest"
	"quantsaas-local/internal/config"
	"quantsaas-local/internal/genome"
	"quantsaas-local/internal/paper"
	"quantsaas-local/internal/quant"
	"quantsaas-local/internal/strategy"
	"quantsaas-local/internal/yahoo"
)

// Top-10 tickers for paper trading (profit-priority selection).
var paperTickers = []string{
	"SOXL", "RIOT", "MARA", "TQQQ", "MSTR",
	"TSLA", "CLSK", "TSLL", "SOXX", "LABU",
}

func main() {
	configPath := flag.String("config", "config.yaml", "config file")
	statePath  := flag.String("state", "paper_state.json", "paper trading state file")
	outPath    := flag.String("out", "docs/data.json", "output JSON for dashboard")
	warmup     := flag.Int("warmup", 200, "warmup bars for indicator seeding")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	state, err := paper.Load(*statePath)
	if err != nil {
		log.Fatalf("state load: %v", err)
	}

	now   := time.Now().UTC()
	today := now.Format("2006-01-02")

	// Fetch enough calendar days to cover warmup bars + weekends/holidays
	fetchStart := now.AddDate(0, 0, -(*warmup+10)*2)

	runCfg := backtest.RunConfig{
		InitialCapital: cfg.Backtest.InitialCapital,
		FeeBPS:         cfg.Backtest.FeeBPS,
		SlippageBPS:    cfg.Backtest.SlippageBPS,
	}

	fmt.Printf("Paper trading run — %s — %d tickers\n\n", today, len(paperTickers))

	for _, symbol := range paperTickers {
		params        := loadParams(symbol, cfg)
		expenseRatio  := expenseRatioFor(symbol, cfg)
		runCfg.ExpenseRatio = expenseRatio

		bars, err := yahoo.FetchDaily(symbol, fetchStart, now)
		if err != nil {
			fmt.Printf("  %-6s  ERROR fetching: %v\n", symbol, err)
			continue
		}
		if len(bars) < *warmup+2 {
			fmt.Printf("  %-6s  not enough bars (%d)\n", symbol, len(bars))
			continue
		}

		pos      := state.FindPosition(symbol, cfg.Backtest.InitialCapital)
		todayBar := bars[len(bars)-1]
		filledUSD := 0.0

		// ── Phase 1: fill yesterday's pending order at today's open ──────────
		if pos.PendingOrderUSD != 0 && todayBar.Open > 0 {
			fillPrice := todayBar.Open
			if pos.PendingOrderUSD > 0 {
				spend := math.Min(pos.PendingOrderUSD, pos.Cash)
				if spend >= params.MinTradeUSD {
					fill    := fillPrice * (1 + runCfg.SlippageBPS/10000.0)
					fee     := spend * runCfg.FeeBPS / 10000.0
					netSpend := math.Min(spend, math.Max(0, pos.Cash-fee))
					pos.Shares += netSpend / fill
					pos.Cash   -= netSpend + fee
					pos.TradeCount++
					pos.BarsSinceTrade = 0
					filledUSD = spend
				}
			} else {
				wantSell := math.Min(-pos.PendingOrderUSD/fillPrice, pos.Shares)
				if wantSell*fillPrice >= params.MinTradeUSD {
					fill  := fillPrice * (1 - runCfg.SlippageBPS/10000.0)
					gross := wantSell * fill
					fee   := gross * runCfg.FeeBPS / 10000.0
					pos.Shares -= wantSell
					pos.Cash   += gross - fee
					pos.TradeCount++
					pos.BarsSinceTrade = 0
					filledUSD = -wantSell * fillPrice
				}
			}
			pos.PendingOrderUSD = 0
		}

		// Apply daily expense ratio drag
		if expenseRatio > 0 && pos.Shares > 0 {
			pos.Shares *= math.Max(0, 1-expenseRatio/252.0)
		}
		if pos.BarsSinceTrade < params.RebalanceCooldownBars {
			pos.BarsSinceTrade++
		}

		// ── Phase 2: compute signal on today's close, store pending order ─────
		closes := make([]float64, 0, len(bars))
		for _, b := range bars {
			if b.Close > 0 {
				closes = append(closes, b.Close)
			}
		}

		equity := pos.Cash + pos.Shares*todayBar.Close
		out := strategy.Step(quant.StrategyInput{
			Closes: closes,
			Portfolio: quant.PortfolioSnapshot{
				Cash:           pos.Cash,
				Shares:         pos.Shares,
				Equity:         equity,
				LastPrice:      todayBar.Close,
				TradeCount:     pos.TradeCount,
				BarsSinceTrade: pos.BarsSinceTrade,
			},
			Params: params,
		})

		// Store order to fill tomorrow at open
		pos.PendingOrderUSD = out.OrderUSD
		pos.UpdateMetrics(todayBar.Close)

		rec := paper.DailyRecord{
			Date:     today,
			Symbol:   symbol,
			Close:    todayBar.Close,
			Equity:   pos.Equity,
			ROI:      pos.ROI,
			Signal:   out.Signal,
			Target:   out.TargetWeight,
			OrderUSD: filledUSD,
			Trades:   pos.TradeCount,
			MaxDD:    pos.MaxDrawdown,
		}
		state.History = append(state.History, rec)

		fillStr := "  —  "
		if filledUSD > 0 {
			fillStr = fmt.Sprintf("filled +$%.0f", filledUSD)
		} else if filledUSD < 0 {
			fillStr = fmt.Sprintf("filled -$%.0f", math.Abs(filledUSD))
		}
		pendStr := "no pending"
		if out.OrderUSD > 0 {
			pendStr = fmt.Sprintf("pending +$%.0f", out.OrderUSD)
		} else if out.OrderUSD < 0 {
			pendStr = fmt.Sprintf("pending -$%.0f", math.Abs(out.OrderUSD))
		}
		fmt.Printf("  %-6s  close=$%8.2f  equity=$%10.2f  ROI=%+6.1f%%  %-18s  %-18s\n",
			symbol, todayBar.Close, pos.Equity, pos.ROI*100, fillStr, pendStr)
	}

	if err := state.Save(*statePath); err != nil {
		log.Fatalf("state save: %v", err)
	}
	if err := writeDashboardJSON(*outPath, state, today); err != nil {
		log.Fatalf("dashboard write: %v", err)
	}

	fmt.Printf("\nState saved → %s\nDashboard  → %s\n", *statePath, *outPath)
}

func loadParams(symbol string, cfg *config.Config) quant.StrategyParams {
	path := filepath.Join("reports", "champions", symbol+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return genome.Default.ToParams()
	}
	var c genome.Chromosome
	if err := json.Unmarshal(raw, &c); err != nil {
		return genome.Default.ToParams()
	}
	params := c.ToParams()
	if params.MinTradeUSD < 10 {
		params.MinTradeUSD = 10.10
	}
	return params
}

func expenseRatioFor(symbol string, cfg *config.Config) float64 {
	for _, t := range cfg.Tickers {
		if t.Symbol == symbol {
			return t.ExpenseRatio
		}
	}
	return 0
}

// DashboardData is the JSON structure consumed by the GitHub Pages dashboard.
type DashboardData struct {
	UpdatedAt string              `json:"updated_at"`
	Positions []paper.Position    `json:"positions"`
	History   []paper.DailyRecord `json:"history"`
	Summary   []TickerSummary     `json:"summary"`
}

type TickerSummary struct {
	Symbol     string  `json:"symbol"`
	Equity     float64 `json:"equity"`
	ROI        float64 `json:"roi"`
	MaxDD      float64 `json:"max_dd"`
	TradeCount int     `json:"trade_count"`
	DaysSince  int     `json:"days_since_start"`
}

func writeDashboardJSON(path string, state *paper.State, today string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	summary := make([]TickerSummary, 0, len(state.Positions))
	for _, p := range state.Positions {
		days := int(time.Since(p.StartedAt).Hours() / 24)
		summary = append(summary, TickerSummary{
			Symbol:     p.Symbol,
			Equity:     p.Equity,
			ROI:        p.ROI,
			MaxDD:      p.MaxDrawdown,
			TradeCount: p.TradeCount,
			DaysSince:  days,
		})
	}
	data := DashboardData{
		UpdatedAt: today,
		Positions: state.Positions,
		History:   state.History,
		Summary:   summary,
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}
