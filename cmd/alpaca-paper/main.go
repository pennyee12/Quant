// alpaca-paper evaluates the top-10 strategy using Alpaca data and can submit
// paper orders to Alpaca when explicitly run with -execute.
//
// Default mode is dry-run: it prints intended orders but places nothing.
// Per-ticker cash, shares, equity, ROI, drawdown, and full transaction history
// are persisted locally in -state file (default alpaca_state.json).
// Duplicate-date guard skips re-runs for today unless -force is set.
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

	"github.com/pennyee12/Quant/internal/alpaca"
	"github.com/pennyee12/Quant/internal/config"
	"github.com/pennyee12/Quant/internal/genome"
	"github.com/pennyee12/Quant/internal/quant"
	"github.com/pennyee12/Quant/internal/strategy"
)

var topTickers = []string{
	"SOXL", "RIOT", "MARA", "TQQQ", "MSTR",
	"TSLA", "CLSK", "TSLL", "SOXX", "LABU",
}

func main() {
	configPath := flag.String("config", "config.yaml", "config file")
	envPath := flag.String("env", ".env", "local env file for Alpaca credentials")
	execute := flag.Bool("execute", false, "submit paper orders to Alpaca; default is dry-run")
	feed := flag.String("feed", "iex", "Alpaca data feed: iex for free/basic, sip for paid")
	warmup := flag.Int("warmup", 200, "warmup bars for indicator seeding")
	allocation := flag.Float64("allocation", 10000, "initial capital per ticker (used only on first run for that ticker)")
	tickersFlag := flag.String("tickers", "", "comma-separated tickers; default top 10")
	statePath := flag.String("state", "alpaca_state.json", "path to local run-state JSON file")
	force := flag.Bool("force", false, "bypass duplicate-date guard and re-run today")
	allowNonTradingDay := flag.Bool("allow-non-trading-day", false, "run even when today is not a regular NYSE trading day")
	saveDryRun := flag.Bool("save-dry-run", false, "persist dry-run decisions; default dry runs are read-only")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	client, err := alpaca.NewFromEnv(*envPath)
	if err != nil {
		log.Fatalf("alpaca env: %v", err)
	}

	// Safety: enforce paper endpoint regardless of env override
	if *execute && !strings.Contains(client.TradingBaseURL, "paper") {
		log.Fatalf("SAFETY: -execute is only allowed against paper-api.alpaca.markets (got %s)", client.TradingBaseURL)
	}

	account, accountReqID, err := client.GetAccount()
	if err != nil {
		log.Fatalf("alpaca account: %v", err)
	}
	fmt.Printf("Alpaca paper account %s — status=%s portfolio=%s cash=%s buying_power=%s request_id=%s\n",
		account.AccountNumber, account.Status, account.PortfolioValue, account.Cash, account.BuyingPower, accountReqID)
	if account.TradingBlocked || account.AccountBlocked {
		log.Fatalf("alpaca account is blocked: trading_blocked=%v account_blocked=%v", account.TradingBlocked, account.AccountBlocked)
	}

	// Load persisted local state
	state, err := alpaca.LoadRunState(*statePath)
	if err != nil {
		log.Fatalf("load state %s: %v", *statePath, err)
	}

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatalf("load America/New_York timezone: %v", err)
	}
	nowET := time.Now().In(ny)
	today := nowET.Format("2006-01-02")
	if !*allowNonTradingDay {
		cal, calReqID, err := client.GetCalendar(today, today)
		if err != nil {
			log.Fatalf("alpaca calendar: %v", err)
		}
		if len(cal) == 0 {
			fmt.Printf("%s is not an Alpaca/NYSE trading day request_id=%s. Exiting without orders.\n", today, calReqID)
			return
		}
	}

	// Duplicate-date guard
	if !*force && state.LastRunDate == today {
		fmt.Printf("Already ran for %s. Use -force to re-run. Exiting.\n", today)
		return
	}

	// Get actual Alpaca positions to reconcile shares
	positions, _, err := client.GetPositions()
	if err != nil {
		log.Fatalf("alpaca positions: %v", err)
	}
	posBySymbol := make(map[string]alpaca.Position)
	for _, p := range positions {
		posBySymbol[p.Symbol] = p
	}
	openOrderBySymbol := map[string][]alpaca.Order{}
	if *execute {
		openOrders, openReqID, err := client.GetOpenOrders()
		if err != nil {
			log.Fatalf("alpaca open orders: %v", err)
		}
		for _, o := range openOrders {
			openOrderBySymbol[strings.ToUpper(o.Symbol)] = append(openOrderBySymbol[strings.ToUpper(o.Symbol)], o)
		}
		if len(openOrderBySymbol) > 0 {
			fmt.Printf("Open-order preflight found existing open orders request_id=%s; affected symbols will be skipped.\n", openReqID)
		}
	}

	tickers := topTickers
	if *tickersFlag != "" {
		tickers = splitTickers(*tickersFlag)
	}
	now := time.Now().UTC()
	fetchStart := now.AddDate(0, 0, -(*warmup+10)*2)
	mode := "DRY RUN"
	if *execute {
		mode = "EXECUTE PAPER ORDERS"
	}
	fmt.Printf("%s — %s — %d tickers — feed=%s\n\n", mode, today, len(tickers), *feed)

	var decisions []alpaca.Decision
	var orders []alpaca.OrderEvent

	for _, symbol := range tickers {
		params := loadParams(symbol)
		params.MinTradeUSD = math.Max(params.MinTradeUSD, cfg.Strategy.MinTradeUSD)

		// Get or initialise per-ticker state (first run seeds with allocation)
		ts := state.GetOrInitTicker(symbol, *allocation)

		bars, reqID, err := client.FetchDailyBars(symbol, fetchStart, now, *feed)
		if err != nil {
			fmt.Printf("  %-6s ERROR bars: %v\n", symbol, err)
			decisions = append(decisions, alpaca.Decision{
				TimeUTC: now, Date: today, Symbol: symbol, Action: "error", Error: err.Error(), DryRun: !*execute,
			})
			continue
		}
		if len(bars) < *warmup+2 {
			msg := fmt.Sprintf("not enough bars from Alpaca (%d) request_id=%s", len(bars), reqID)
			fmt.Printf("  %-6s %s\n", symbol, msg)
			decisions = append(decisions, alpaca.Decision{
				TimeUTC: now, Date: today, Symbol: symbol, Action: "error", Error: msg, DryRun: !*execute,
			})
			continue
		}
		last := bars[len(bars)-1]

		// Reconcile shares and cash from Alpaca's actual fills (source of truth).
		// If Alpaca has no position for this ticker, zero out shares and restore cash.
		if p, ok := posBySymbol[symbol]; ok {
			ts.ReconcileFromAlpaca(alpaca.ParseFloat(p.Qty), alpaca.ParseFloat(p.AvgEntryPrice))
		} else {
			ts.ReconcileFromAlpaca(0, 0)
		}

		// Update equity/ROI/drawdown with latest price
		ts.UpdatePrice(last.Close)

		// Strategy sees per-ticker cash from local state (not a fixed $10k cap)
		stockValue := ts.Shares * last.Close
		cashForTicker := ts.Cash
		equity := cashForTicker + stockValue

		closes := make([]float64, 0, len(bars))
		for _, b := range bars {
			if b.Close > 0 {
				closes = append(closes, b.Close)
			}
		}
		out := strategy.Step(quant.StrategyInput{
			Closes: closes,
			Portfolio: quant.PortfolioSnapshot{
				Cash:           cashForTicker,
				Shares:         ts.Shares,
				Equity:         equity,
				LastPrice:      last.Close,
				TradeCount:     ts.TradeCount,
				BarsSinceTrade: params.RebalanceCooldownBars,
			},
			Params: params,
		})

		action := "hold"
		if out.OrderUSD > 0 {
			action = fmt.Sprintf("buy $%.2f", out.OrderUSD)
		} else if out.OrderUSD < 0 {
			action = fmt.Sprintf("sell $%.2f", math.Abs(out.OrderUSD))
		}
		fmt.Printf("  %-6s close=$%8.2f shares=%10.4f stock=$%9.2f cash=$%9.2f equity=$%9.2f ROI=%+.1f%% target=%5.1f%% signal=%+.4f action=%s\n",
			symbol, last.Close, ts.Shares, stockValue, cashForTicker, equity,
			ts.ROI*100, out.TargetWeight*100, out.Signal, action)

		dec := alpaca.Decision{
			TimeUTC:    now,
			Date:       today,
			Symbol:     symbol,
			Close:      last.Close,
			Shares:     ts.Shares,
			StockValue: stockValue,
			AllocCash:  cashForTicker,
			Equity:     equity,
			Target:     out.TargetWeight,
			Signal:     out.Signal,
			OrderUSD:   out.OrderUSD,
			Action:     action,
			DryRun:     !*execute,
		}

		if !*execute || math.Abs(out.OrderUSD) < params.MinTradeUSD {
			decisions = append(decisions, dec)
			continue
		}
		if existing := openOrderBySymbol[symbol]; len(existing) > 0 {
			ids := make([]string, 0, len(existing))
			for _, o := range existing {
				ids = append(ids, o.ID)
			}
			msg := "skip: existing open Alpaca order(s): " + strings.Join(ids, ",")
			fmt.Printf("         %s\n", msg)
			dec.Action = "skip_open_order"
			dec.Error = msg
			decisions = append(decisions, dec)
			continue
		}

		if out.OrderUSD > 0 {
			// Cannot spend more than available cash for this ticker
			notional := math.Min(out.OrderUSD, ts.Cash)
			if notional < params.MinTradeUSD {
				decisions = append(decisions, dec)
				continue
			}

			order, orderReqID, orderErr := client.SubmitMarketNotional(symbol, "buy", notional)
			if orderErr != nil {
				fmt.Printf("         ORDER ERROR buy request_id=%s err=%v\n", orderReqID, orderErr)
				dec.Error = orderErr.Error()
				dec.RequestID = orderReqID
				decisions = append(decisions, dec)
				orders = append(orders, alpaca.OrderEvent{
					TimeUTC: now, Date: today, Symbol: symbol, Side: "buy",
					Notional: notional, RequestID: orderReqID, Error: orderErr.Error(),
				})
				continue
			}
			fmt.Printf("         ORDER SUBMITTED buy $%.2f id=%s status=%s request_id=%s\n", notional, order.ID, order.Status, orderReqID)

			ts.RecordBuy(today, notional, last.Close, order.ID, orderReqID)
			dec.RequestID = orderReqID
			dec.SubmittedID = order.ID
			decisions = append(decisions, dec)
			orders = append(orders, alpaca.OrderEvent{
				TimeUTC: now, Date: today, Symbol: symbol, Side: "buy",
				Notional: notional, OrderID: order.ID, Status: order.Status, RequestID: orderReqID,
			})
		} else {
			// Sell: cap qty at shares actually owned — never short
			qty := math.Min(math.Abs(out.OrderUSD)/last.Close, ts.Shares)
			if qty <= 0 {
				fmt.Printf("         SKIP: no shares to sell\n")
				decisions = append(decisions, dec)
				continue
			}
			if qty*last.Close < params.MinTradeUSD {
				decisions = append(decisions, dec)
				continue
			}

			order, orderReqID, orderErr := client.SubmitMarketQty(symbol, "sell", qty)
			if orderErr != nil {
				fmt.Printf("         ORDER ERROR sell request_id=%s err=%v\n", orderReqID, orderErr)
				dec.Error = orderErr.Error()
				dec.RequestID = orderReqID
				decisions = append(decisions, dec)
				orders = append(orders, alpaca.OrderEvent{
					TimeUTC: now, Date: today, Symbol: symbol, Side: "sell",
					Qty: qty, RequestID: orderReqID, Error: orderErr.Error(),
				})
				continue
			}
			fmt.Printf("         ORDER SUBMITTED sell qty=%.4f id=%s status=%s request_id=%s\n", qty, order.ID, order.Status, orderReqID)
			ts.RecordSell(today, qty, last.Close, order.ID, orderReqID)
			dec.RequestID = orderReqID
			dec.SubmittedID = order.ID
			decisions = append(decisions, dec)
			orders = append(orders, alpaca.OrderEvent{
				TimeUTC: now, Date: today, Symbol: symbol, Side: "sell",
				Qty: qty, OrderID: order.ID, Status: order.Status, RequestID: orderReqID,
			})
		}
	}

	// Print per-ticker summary
	fmt.Println()
	fmt.Println("── Per-ticker summary ──────────────────────────────────────────────────────")
	for _, symbol := range tickers {
		if ts, ok := state.Positions[symbol]; ok {
			fmt.Println(" ", ts.Summary())
		}
	}
	fmt.Println()
	fmt.Println(" ", state.AccountSummary())
	fmt.Println()

	// Persist state. Dry runs are read-only by default so a verification run
	// cannot accidentally consume the duplicate-date guard.
	if !*execute && !*saveDryRun {
		fmt.Println("Dry run complete; state not saved. Use -save-dry-run to persist dry-run decisions.")
		return
	}

	state.LastRunDate = today
	state.Decisions = append(state.Decisions, decisions...)
	state.Orders = append(state.Orders, orders...)
	if err := state.Save(*statePath); err != nil {
		log.Printf("WARN: could not save state to %s: %v", *statePath, err)
	} else {
		fmt.Printf("State saved to %s\n", *statePath)
	}
}

func loadParams(symbol string) quant.StrategyParams {
	path := filepath.Join("reports", "champions", symbol+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return genome.Default.ToParams()
	}
	var c genome.Chromosome
	if err := json.Unmarshal(raw, &c); err != nil {
		return genome.Default.ToParams()
	}
	return c.ToParams()
}

func splitTickers(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
