// alpaca-paper evaluates the top-10 strategy using Alpaca data and can submit
// paper orders to Alpaca when explicitly run with -execute.
//
// Default mode is dry-run: it prints intended orders but places nothing.
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
	allocation := flag.Float64("allocation", 10000, "virtual strategy allocation per ticker")
	tickersFlag := flag.String("tickers", "", "comma-separated tickers; default top 10")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	client, err := alpaca.NewFromEnv(*envPath)
	if err != nil {
		log.Fatalf("alpaca env: %v", err)
	}

	account, requestID, err := client.GetAccount()
	if err != nil {
		log.Fatalf("alpaca account: %v", err)
	}
	fmt.Printf("Alpaca paper account %s — status=%s portfolio=%s cash=%s buying_power=%s request_id=%s\n",
		account.AccountNumber, account.Status, account.PortfolioValue, account.Cash, account.BuyingPower, requestID)
	if account.TradingBlocked || account.AccountBlocked {
		log.Fatalf("alpaca account is blocked: trading_blocked=%v account_blocked=%v", account.TradingBlocked, account.AccountBlocked)
	}

	positions, _, err := client.GetPositions()
	if err != nil {
		log.Fatalf("alpaca positions: %v", err)
	}
	posBySymbol := make(map[string]alpaca.Position)
	for _, p := range positions {
		posBySymbol[p.Symbol] = p
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
	fmt.Printf("%s — %s — %d tickers — feed=%s\n\n", mode, now.Format("2006-01-02"), len(tickers), *feed)

	for _, symbol := range tickers {
		params := loadParams(symbol)
		params.MinTradeUSD = math.Max(params.MinTradeUSD, cfg.Strategy.MinTradeUSD)

		bars, reqID, err := client.FetchDailyBars(symbol, fetchStart, now, *feed)
		if err != nil {
			fmt.Printf("  %-6s ERROR bars: %v\n", symbol, err)
			continue
		}
		if len(bars) < *warmup+2 {
			fmt.Printf("  %-6s not enough bars from Alpaca (%d) request_id=%s\n", symbol, len(bars), reqID)
			continue
		}
		last := bars[len(bars)-1]
		shares := 0.0
		if p, ok := posBySymbol[symbol]; ok {
			shares = alpaca.ParseFloat(p.Qty)
		}
		stockValue := shares * last.Close
		cashForTicker := math.Max(0, *allocation-stockValue)
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
				Shares:         shares,
				Equity:         equity,
				LastPrice:      last.Close,
				TradeCount:     0,
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
		fmt.Printf("  %-6s close=$%8.2f shares=%10.4f stock=$%9.2f alloc_cash=$%9.2f target=%5.1f%% action=%s\n",
			symbol, last.Close, shares, stockValue, cashForTicker, out.TargetWeight*100, action)

		if !*execute || math.Abs(out.OrderUSD) < params.MinTradeUSD {
			continue
		}
		if out.OrderUSD > 0 {
			order, orderReqID, err := client.SubmitMarketNotional(symbol, "buy", out.OrderUSD)
			if err != nil {
				fmt.Printf("         ORDER ERROR buy request_id=%s err=%v\n", orderReqID, err)
				continue
			}
			fmt.Printf("         ORDER SUBMITTED buy id=%s status=%s request_id=%s\n", order.ID, order.Status, orderReqID)
		} else {
			qty := math.Min(math.Abs(out.OrderUSD)/last.Close, shares)
			if qty*last.Close < params.MinTradeUSD {
				continue
			}
			order, orderReqID, err := client.SubmitMarketQty(symbol, "sell", qty)
			if err != nil {
				fmt.Printf("         ORDER ERROR sell request_id=%s err=%v\n", orderReqID, err)
				continue
			}
			fmt.Printf("         ORDER SUBMITTED sell id=%s status=%s request_id=%s\n", order.ID, order.Status, orderReqID)
		}
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
