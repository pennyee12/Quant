// paper runs one daily paper-trading step for all configured tickers.
// It fetches the latest bar from Yahoo Finance, runs the strategy using
// the trained champion chromosome, updates positions, and writes JSON
// results for the GitHub Pages dashboard.
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
	statePath := flag.String("state", "paper_state.json", "paper trading state file")
	outPath := flag.String("out", "docs/data.json", "output JSON for dashboard")
	warmup := flag.Int("warmup", 200, "warmup bars to fetch for indicator seeding")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	state, err := paper.Load(*statePath)
	if err != nil {
		log.Fatalf("state load: %v", err)
	}

	now := time.Now().UTC()
	today := now.Format("2006-01-02")

	// Fetch enough history to seed indicators: warmup + last 5 days buffer
	fetchStart := now.AddDate(0, 0, -(*warmup+10)*2) // 2x calendar days to account for weekends/holidays

	runCfg := backtest.RunConfig{
		InitialCapital: cfg.Backtest.InitialCapital,
		FeeBPS:         cfg.Backtest.FeeBPS,
		SlippageBPS:    cfg.Backtest.SlippageBPS,
		WarmupBars:     *warmup,
	}

	fmt.Printf("Paper trading run — %s — %d tickers\n\n", today, len(paperTickers))

	for _, symbol := range paperTickers {
		params := loadParams(symbol, cfg)
		expenseRatio := expenseRatioFor(symbol, cfg)
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

		// Use all bars for warmup, then step on the last bar
		pos := state.FindPosition(symbol, cfg.Backtest.InitialCapital)

		// Build closes slice from all bars for indicator state
		closes := make([]float64, 0, len(bars))
		for _, b := range bars {
			if b.Close > 0 {
				closes = append(closes, b.Close)
			}
		}

		lastBar := bars[len(bars)-1]
		equity := pos.Cash + pos.Shares*lastBar.Close

		out := strategy.Step(quant.StrategyInput{
			Closes: closes,
			Portfolio: quant.PortfolioSnapshot{
				Cash:           pos.Cash,
				Shares:         pos.Shares,
				Equity:         equity,
				LastPrice:      lastBar.Close,
				TradeCount:     pos.TradeCount,
				BarsSinceTrade: pos.BarsSinceTrade,
			},
			Params: params,
		})

		orderUSD := 0.0
		// Simulate fill at last bar's close (next open not available yet — will adjust tomorrow)
		fillPrice := lastBar.Close
		if out.OrderUSD > 0 {
			spend := math.Min(out.OrderUSD, pos.Cash)
			if spend >= params.MinTradeUSD {
				fill := fillPrice * (1 + runCfg.SlippageBPS/10000.0)
				fee := spend * runCfg.FeeBPS / 10000.0
				netSpend := math.Min(spend, math.Max(0, pos.Cash-fee))
				pos.Shares += netSpend / fill
				pos.Cash -= netSpend + fee
				pos.TradeCount++
				pos.BarsSinceTrade = 0
				orderUSD = spend
			}
		} else if out.OrderUSD < 0 {
			wantSell := math.Min(-out.OrderUSD/fillPrice, pos.Shares)
			if wantSell*fillPrice >= params.MinTradeUSD {
				fill := fillPrice * (1 - runCfg.SlippageBPS/10000.0)
				gross := wantSell * fill
				fee := gross * runCfg.FeeBPS / 10000.0
				pos.Shares -= wantSell
				pos.Cash += gross - fee
				pos.TradeCount++
				pos.BarsSinceTrade = 0
				orderUSD = -wantSell * fillPrice
			}
		}

		if expenseRatio > 0 && pos.Shares > 0 {
			pos.Shares *= math.Max(0, 1-expenseRatio/252.0)
		}
		if pos.BarsSinceTrade < params.RebalanceCooldownBars {
			pos.BarsSinceTrade++
		}

		pos.UpdateMetrics(lastBar.Close)

		rec := paper.DailyRecord{
			Date:     today,
			Symbol:   symbol,
			Close:    lastBar.Close,
			Equity:   pos.Equity,
			ROI:      pos.ROI,
			Signal:   out.Signal,
			Target:   out.TargetWeight,
			OrderUSD: orderUSD,
			Trades:   pos.TradeCount,
			MaxDD:    pos.MaxDrawdown,
		}
		state.History = append(state.History, rec)

		sign := " "
		if orderUSD > 0 {
			sign = "+"
		} else if orderUSD < 0 {
			sign = "-"
		}
		fmt.Printf("  %-6s  close=%.2f  equity=$%9.2f  ROI=%+.1f%%  signal=%+.3f  target=%.0f%%  order=%s$%.0f\n",
			symbol, lastBar.Close, pos.Equity, pos.ROI*100, out.Signal, out.TargetWeight*100, sign, math.Abs(orderUSD))
	}

	if err := state.Save(*statePath); err != nil {
		log.Fatalf("state save: %v", err)
	}

	if err := writeDashboardJSON(*outPath, state, today); err != nil {
		log.Fatalf("dashboard write: %v", err)
	}

	fmt.Printf("\nState saved → %s\nDashboard  → %s\n", *statePath, *outPath)
}

// loadParams loads the trained champion chromosome for a symbol, falling back to default.
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
	params.MinTradeUSD = cfg.Backtest.FeeBPS // reuse fee as floor proxy
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
	UpdatedAt   string              `json:"updated_at"`
	Positions   []paper.Position    `json:"positions"`
	History     []paper.DailyRecord `json:"history"`
	Summary     []TickerSummary     `json:"summary"`
}

type TickerSummary struct {
	Symbol      string  `json:"symbol"`
	Equity      float64 `json:"equity"`
	ROI         float64 `json:"roi"`
	MaxDD       float64 `json:"max_dd"`
	TradeCount  int     `json:"trade_count"`
	DaysSince   int     `json:"days_since_start"`
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
