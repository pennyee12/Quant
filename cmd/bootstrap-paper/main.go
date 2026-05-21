// bootstrap-paper simulates paper trading for a list of tickers over a date
// range using cached bar data, and writes the resulting paper_state.json.
// Use this to seed historical records when switching tickers or initial capital.
//
// Usage:
//
//	go run ./cmd/bootstrap-paper -from 2026-05-01 -out paper_state.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pennyee12/Quant/internal/backtest"
	"github.com/pennyee12/Quant/internal/config"
	"github.com/pennyee12/Quant/internal/genome"
	"github.com/pennyee12/Quant/internal/paper"
	"github.com/pennyee12/Quant/internal/quant"
	"github.com/pennyee12/Quant/internal/strategy"
)

const warmupBars = 200

func main() {
	fromStr := flag.String("from", "2026-05-01", "start date YYYY-MM-DD")
	toStr := flag.String("to", "", "end date YYYY-MM-DD (default: today)")
	outPath := flag.String("out", "paper_state.json", "output state file")
	configPath := flag.String("config", "config.yaml", "config file")
	tickersFlag := flag.String("tickers", "RIOT,SOXL,LABU,CHAT,TAN", "comma-separated tickers")
	capitalFlag := flag.Float64("capital", 20000, "initial capital per ticker")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	fromDate, err := time.Parse("2006-01-02", *fromStr)
	if err != nil {
		log.Fatalf("invalid -from date: %v", err)
	}
	toDate := time.Now().UTC().Truncate(24 * time.Hour)
	if *toStr != "" {
		if toDate, err = time.Parse("2006-01-02", *toStr); err != nil {
			log.Fatalf("invalid -to date: %v", err)
		}
	}

	tickers := strings.Split(*tickersFlag, ",")
	for i, t := range tickers {
		tickers[i] = strings.TrimSpace(strings.ToUpper(t))
	}

	runCfg := backtest.RunConfig{
		InitialCapital: *capitalFlag,
		FeeBPS:         cfg.Backtest.FeeBPS,
		SlippageBPS:    cfg.Backtest.SlippageBPS,
	}

	state := &paper.State{}

	fmt.Printf("Bootstrap paper trading: %s → %s  capital=$%.0f\n\n",
		fromDate.Format("2006-01-02"), toDate.Format("2006-01-02"), *capitalFlag)

	for _, sym := range tickers {
		bars, err := loadCachedBars(sym)
		if err != nil {
			log.Fatalf("load bars %s: %v", sym, err)
		}

		expRatio := expenseRatioFor(sym, cfg)
		runCfg.ExpenseRatio = expRatio
		params := loadParams(sym)

		records, pos := simulate(sym, bars, params, runCfg, fromDate, toDate, *capitalFlag)
		state.Positions = append(state.Positions, pos)
		state.History = append(state.History, records...)

		fmt.Printf("  %-6s  %d records  equity=$%.2f  ROI=%+.2f%%  trades=%d\n",
			sym, len(records), pos.Equity, pos.ROI*100, pos.TradeCount)
	}

	if err := state.Save(*outPath); err != nil {
		log.Fatalf("save state: %v", err)
	}
	fmt.Printf("\nWrote %s\n", *outPath)
}

func simulate(sym string, allBars []quant.Bar, params quant.StrategyParams, runCfg backtest.RunConfig,
	fromDate, toDate time.Time, initialCapital float64) ([]paper.DailyRecord, paper.Position) {

	fromMS := fromDate.UnixMilli()
	toMS := toDate.Add(24*time.Hour - time.Millisecond).UnixMilli()

	// Find index of first bar on or after fromDate
	startIdx := -1
	for i, b := range allBars {
		if b.TimeMS >= fromMS {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		log.Fatalf("%s: no bars found on or after %s", sym, fromDate.Format("2006-01-02"))
	}

	// Ensure we have enough warmup bars
	warmupStart := startIdx - warmupBars
	if warmupStart < 0 {
		warmupStart = 0
	}
	actualWarmup := startIdx - warmupStart

	pos := paper.Position{
		Symbol:         sym,
		Cash:           initialCapital,
		InitialCapital: initialCapital,
		PeakEquity:     initialCapital,
		StartedAt:      fromDate.UTC(),
	}

	var records []paper.DailyRecord
	pendingOrderUSD := 0.0

	// Walk bars from warmupStart through end of sim range
	for i := warmupStart; i < len(allBars); i++ {
		bar := allBars[i]
		if bar.TimeMS > toMS {
			break
		}

		isLive := i >= startIdx
		date := time.UnixMilli(bar.TimeMS).UTC().Format("2006-01-02")

		filledUSD := 0.0
		actualFillPrice := 0.0

		// Phase 1: fill pending order at today's open (only during live range)
		if isLive && pendingOrderUSD != 0 && bar.Open > 0 {
			filledUSD = fillOrder(&pos, params, runCfg, bar.Open, pendingOrderUSD)
			if filledUSD != 0 {
				actualFillPrice = bar.Open
			}
			pendingOrderUSD = 0
		}

		// Apply daily expense ratio drag
		if runCfg.ExpenseRatio > 0 && pos.Shares > 0 {
			pos.Shares *= math.Max(0, 1-runCfg.ExpenseRatio/252.0)
		}
		if pos.BarsSinceTrade < params.RebalanceCooldownBars {
			pos.BarsSinceTrade++
		}

		// Build closes slice up to and including current bar
		closes := make([]float64, 0, i-warmupStart+1)
		for j := warmupStart; j <= i; j++ {
			if allBars[j].Close > 0 {
				closes = append(closes, allBars[j].Close)
			}
		}

		equity := pos.Cash + pos.Shares*bar.Close
		out := strategy.Step(quant.StrategyInput{
			Closes: closes,
			Portfolio: quant.PortfolioSnapshot{
				Cash:           pos.Cash,
				Shares:         pos.Shares,
				Equity:         equity,
				LastPrice:      bar.Close,
				TradeCount:     pos.TradeCount,
				BarsSinceTrade: pos.BarsSinceTrade,
			},
			Params: params,
		})

		pendingOrderUSD = out.OrderUSD
		pos.UpdateMetrics(bar.Close)

		if !isLive {
			continue
		}

		rec := paper.DailyRecord{
			Date:      date,
			Symbol:    sym,
			Close:     bar.Close,
			Equity:    pos.Equity,
			ROI:       pos.ROI,
			Signal:    out.Signal,
			Target:    out.TargetWeight,
			OrderUSD:  filledUSD,
			FillPrice: actualFillPrice,
			Trades:    pos.TradeCount,
			MaxDD:     pos.MaxDrawdown,
		}
		if filledUSD != 0 {
			rec.FillTime = date
			rec.FillMode = "open"
		}
		records = append(records, rec)
	}

	// Store pending for tomorrow
	pos.PendingOrderUSD = pendingOrderUSD
	_ = actualWarmup
	return records, pos
}

func fillOrder(pos *paper.Position, params quant.StrategyParams, runCfg backtest.RunConfig, fillPrice, orderUSD float64) float64 {
	if orderUSD > 0 {
		spend := math.Min(orderUSD, pos.Cash)
		if spend < params.MinTradeUSD {
			return 0
		}
		fill := fillPrice * (1 + runCfg.SlippageBPS/10000.0)
		fee := spend * runCfg.FeeBPS / 10000.0
		netSpend := math.Min(spend, math.Max(0, pos.Cash-fee))
		pos.Shares += netSpend / fill
		pos.Cash -= netSpend + fee
		pos.TradeCount++
		pos.BarsSinceTrade = 0
		return spend
	}
	wantSell := math.Min(-orderUSD/fillPrice, pos.Shares)
	if wantSell*fillPrice < params.MinTradeUSD {
		return 0
	}
	fill := fillPrice * (1 - runCfg.SlippageBPS/10000.0)
	gross := wantSell * fill
	fee := gross * runCfg.FeeBPS / 10000.0
	pos.Shares -= wantSell
	pos.Cash += gross - fee
	pos.TradeCount++
	pos.BarsSinceTrade = 0
	return -wantSell * fillPrice
}

func loadCachedBars(sym string) ([]quant.Bar, error) {
	path := filepath.Join("data", "bars", sym+"_daily.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bars []quant.Bar
	if err := json.Unmarshal(raw, &bars); err != nil {
		return nil, err
	}
	return bars, nil
}

func loadParams(sym string) quant.StrategyParams {
	path := filepath.Join("reports", "champions", sym+".json")
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

func expenseRatioFor(sym string, cfg *config.Config) float64 {
	for _, t := range cfg.Tickers {
		if t.Symbol == sym {
			return t.ExpenseRatio
		}
	}
	return 0
}
