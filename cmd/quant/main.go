package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pennyee12/Quant/internal/backtest"
	"github.com/pennyee12/Quant/internal/config"
	"github.com/pennyee12/Quant/internal/ga"
	"github.com/pennyee12/Quant/internal/genome"
	"github.com/pennyee12/Quant/internal/quant"
	"github.com/pennyee12/Quant/internal/schwab"
)

func main() {
	command, args := commandAndArgs()
	fs := flag.NewFlagSet(command, flag.ExitOnError)

	configPath := fs.String("config", "config.yaml", "config file")
	refresh := fs.Bool("refresh", false, "force refetch bars from Schwab instead of cache")
	startOverride := fs.String("start", "", "override backtest start date YYYY-MM-DD")
	endOverride := fs.String("end", "", "override backtest end date YYYY-MM-DD")
	yearly := fs.Bool("yearly", false, "write full-range per-year comparison to CSV")
	simFrom := fs.String("sim-from", "", "run strategy from this date YYYY-MM-DD with 200-bar warmup (reports only from this date forward)")
	trained := fs.Bool("trained", false, "use champion chromosomes from reports/champions/ if available")
	tickersFlag := fs.String("tickers", "", "comma-separated list of tickers (default: all enabled)")
	popSize := fs.Int("pop", 100, "GA population size")
	maxGens := fs.Int("gens", 30, "GA max generations")
	accountSuffix := fs.String("account-suffix", "", "account-number suffix for account command")

	if err := fs.Parse(args); err != nil {
		log.Fatal(err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	client := schwab.New(cfg.Schwab.APIBaseURL, cfg.Schwab.TokenURL, cfg.Schwab.TokenFile, cfg.Schwab.CredentialScript)

	switch command {
	case "compare", "backtest":
		runCompare(cfg, client, *refresh, *startOverride, *endOverride, *yearly, *simFrom, *trained, *tickersFlag)
	case "train":
		runTrain(cfg, client, *tickersFlag, *popSize, *maxGens, *refresh)
	case "account":
		runAccount(client, *accountSuffix)
	default:
		log.Fatalf("unknown command %q; use: compare, train, account", command)
	}
}

func commandAndArgs() (string, []string) {
	args := os.Args[1:]
	if len(args) == 0 {
		return "compare", nil
	}
	if len(args[0]) > 0 && args[0][0] != '-' {
		return args[0], args[1:]
	}
	return "compare", args
}

// ── compare ───────────────────────────────────────────────────────────────────

func runCompare(cfg *config.Config, client *schwab.Client, refresh bool, startOverride, endOverride string, yearly bool, simFrom string, trained bool, tickersFlag string) {
	start, err := cfg.Backtest.StartTime()
	if err != nil {
		log.Fatal(err)
	}
	end, err := cfg.Backtest.EndTime()
	if err != nil {
		log.Fatal(err)
	}
	if startOverride != "" {
		if start, err = time.Parse("2006-01-02", startOverride); err != nil {
			log.Fatal(err)
		}
	}
	if endOverride != "" {
		if end, err = time.Parse("2006-01-02", endOverride); err != nil {
			log.Fatal(err)
		}
	}

	var simFromTime time.Time
	if simFrom != "" {
		if simFromTime, err = time.Parse("2006-01-02", simFrom); err != nil {
			log.Fatalf("invalid -sim-from date: %v", err)
		}
	}

	defaultParams := genome.Default.ToParams()

	tickers := cfg.EnabledTickers()
	if tickersFlag != "" {
		want := map[string]bool{}
		for _, s := range strings.Split(tickersFlag, ",") {
			want[strings.TrimSpace(strings.ToUpper(s))] = true
		}
		filtered := tickers[:0]
		for _, t := range tickers {
			if want[t.Symbol] {
				filtered = append(filtered, t)
			}
		}
		tickers = filtered
	}

	var results []backtest.Result
	var yearlyResults []backtest.Result

	// Load all bars from the very beginning when using sim-from, to get warmup bars
	loadStart := start
	if !simFromTime.IsZero() {
		loadStart = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	for _, ticker := range tickers {
		bars, err := loadBars(client, cfg, ticker.Symbol, loadStart, end, refresh)
		if err != nil {
			fmt.Printf("WARN %-5s %v\n", ticker.Symbol, err)
			continue
		}
		if len(bars) < 40 {
			fmt.Printf("WARN %-5s only %d bars\n", ticker.Symbol, len(bars))
			continue
		}

		params := defaultParams
		if trained {
			if c, err := genome.Load(championPath(ticker.Symbol)); err == nil {
				params = c.ToParams()
			}
		}

		runCfg := backtest.RunConfig{
			InitialCapital: cfg.Backtest.InitialCapital,
			MonthlyInject:  cfg.Backtest.MonthlyInject,
			ExpenseRatio:   ticker.ExpenseRatio,
			FeeBPS:         cfg.Backtest.FeeBPS,
			SlippageBPS:    cfg.Backtest.SlippageBPS,
		}

		if !simFromTime.IsZero() {
			res := runFromDate(ticker.Symbol, bars, params, runCfg, simFromTime)
			results = append(results, res)
		} else {
			res := backtest.Run(ticker.Symbol, bars, params, runCfg)
			results = append(results, res)
		}

		if yearly {
			yearlyResults = append(yearlyResults, runYearly(ticker.Symbol, bars, params, runCfg)...)
		}
		time.Sleep(120 * time.Millisecond)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].AlphaDCA == results[j].AlphaDCA {
			return results[i].ROI > results[j].ROI
		}
		return results[i].AlphaDCA > results[j].AlphaDCA
	})
	printResults(results, trained)

	if yearly {
		path := "reports/yearly_compare.csv"
		if err := writeYearlyCSV(path, yearlyResults); err != nil {
			log.Fatal(err)
		}
		printYearlySummary(yearlyResults, path)
	}
}

// runFromDate runs the strategy from simFrom using up to 200 prior bars as warmup.
func runFromDate(symbol string, allBars []quant.Bar, params quant.StrategyParams, cfg backtest.RunConfig, simFrom time.Time) backtest.Result {
	simFromMS := simFrom.UnixMilli()

	// Find first bar on or after simFrom
	startIdx := len(allBars)
	for i, b := range allBars {
		if b.TimeMS >= simFromMS {
			startIdx = i
			break
		}
	}
	if startIdx >= len(allBars) {
		return backtest.Result{Symbol: symbol}
	}

	const warmup = 200
	warmupStart := startIdx - warmup
	if warmupStart < 0 {
		warmupStart = 0
	}
	combined := allBars[warmupStart:]
	actualWarmup := startIdx - warmupStart

	cfg.WarmupBars = actualWarmup
	return backtest.Run(symbol, combined, params, cfg)
}

func runYearly(symbol string, bars []quant.Bar, params quant.StrategyParams, cfg backtest.RunConfig) []backtest.Result {
	byYear := map[int][]quant.Bar{}
	var years []int
	seen := map[int]bool{}
	for _, bar := range bars {
		year := time.UnixMilli(bar.TimeMS).Year()
		byYear[year] = append(byYear[year], bar)
		if !seen[year] {
			seen[year] = true
			years = append(years, year)
		}
	}
	sort.Ints(years)

	const warmup = 200
	out := make([]backtest.Result, 0, len(years))
	for yi, year := range years {
		yearBars := byYear[year]

		// Collect warmup bars from preceding years
		var warmupBars []quant.Bar
		for _, prevYear := range years[:yi] {
			warmupBars = append(warmupBars, byYear[prevYear]...)
		}
		if len(warmupBars) > warmup {
			warmupBars = warmupBars[len(warmupBars)-warmup:]
		}

		combined := append(warmupBars, yearBars...)
		if len(combined) < warmup+20 {
			continue
		}

		yCfg := cfg
		yCfg.WarmupBars = len(warmupBars)
		res := backtest.Run(symbol, combined, params, yCfg)
		out = append(out, res)
	}
	return out
}

// ── train ─────────────────────────────────────────────────────────────────────

func runTrain(cfg *config.Config, client *schwab.Client, tickersFlag string, popSize, maxGens int, refresh bool) {
	tickers := cfg.EnabledTickers()
	if tickersFlag != "" {
		want := map[string]bool{}
		for _, s := range strings.Split(tickersFlag, ",") {
			want[strings.TrimSpace(strings.ToUpper(s))] = true
		}
		filtered := tickers[:0]
		for _, t := range tickers {
			if want[t.Symbol] {
				filtered = append(filtered, t)
			}
		}
		tickers = filtered
	}
	if len(tickers) == 0 {
		log.Fatal("no tickers to train")
	}

	ncpu := runtime.NumCPU()
	gaCfg := ga.Config{
		PopSize:           popSize,
		MaxGenerations:    maxGens,
		TournamentSize:    3,
		Elitism:           max(2, popSize/20),
		MutationProb:      0.15,
		MutationScale:     1.0,
		RampFactor:        1.25,
		EarlyStopPatience: 5,
		EarlyStopDelta:    0.001,
		ProbMax:           0.55,
		ScaleMax:          3.0,
		Workers:           ncpu,
	}

	trainStart, _ := time.Parse("2006-01-02", "2000-01-01")
	trainEnd := time.Now()
	total := len(tickers)

	type trainResult struct {
		Symbol string
		Score  float64
	}
	var trainResults []trainResult

	fmt.Printf("\n╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║  Quant GA Training  —  %d tickers  pop=%-3d  gens=%-3d  cpu=%-2d  ║\n",
		total, popSize, maxGens, ncpu)
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n\n")

	started := time.Now()

	for ti, ticker := range tickers {
		// ── ticker header ─────────────────────────────────────────────────────
		overallBar := makeBar(ti, total, 20)
		fmt.Printf("[%s] %2d/%-2d  %-6s\n", overallBar, ti+1, total, ticker.Symbol)

		allBars, err := loadBars(client, cfg, ticker.Symbol, trainStart, trainEnd, refresh)
		if err != nil {
			fmt.Printf("  ✗ could not load bars: %v\n\n", err)
			continue
		}
		if len(allBars) < ga.DefaultWarmupBars+40 {
			fmt.Printf("  ✗ only %d bars — skipping\n\n", len(allBars))
			continue
		}
		fmt.Printf("  %d bars  %s → %s\n",
			len(allBars),
			time.UnixMilli(allBars[0].TimeMS).Format("2006-01-02"),
			time.UnixMilli(allBars[len(allBars)-1].TimeMS).Format("2006-01-02"),
		)

		runCfg := backtest.RunConfig{
			InitialCapital: cfg.Backtest.InitialCapital,
			MonthlyInject:  cfg.Backtest.MonthlyInject,
			ExpenseRatio:   ticker.ExpenseRatio,
			FeeBPS:         cfg.Backtest.FeeBPS,
			SlippageBPS:    cfg.Backtest.SlippageBPS,
		}

		eval := ga.MakeEvaluator(ticker.Symbol, allBars, runCfg)
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		seed := genome.Default
		var elites []genome.Chromosome
		if c, err := genome.Load(championPath(ticker.Symbol)); err == nil {
			seed = c
			elites = []genome.Chromosome{c}
			fmt.Printf("  ↺ seeding from existing champion\n")
		}

		tickerStart := time.Now()
		champion := ga.Run(eval, seed, elites, gaCfg, rng, func(gen int, bestScore float64) {
			bar := makeBar(gen+1, maxGens, 28)
			elapsed := time.Since(tickerStart).Round(time.Second)
			fmt.Printf("\r  [%s] gen %2d/%-2d  best=%+.4f  %v   ",
				bar, gen+1, maxGens, bestScore, elapsed)
		})

		finalScore := eval(champion)
		detail := ga.Evaluate(ticker.Symbol, allBars, champion, runCfg)

		fmt.Printf("\r  [%s] done  score=%+.4f  %v         \n",
			makeBar(maxGens, maxGens, 28), finalScore, time.Since(tickerStart).Round(time.Second))

		// Per-window breakdown
		for _, w := range ga.EvalWindows {
			if s, ok := detail.WindowScores[w.Label]; ok {
				fmt.Printf("    %-4s  score=%+.4f  roi=%+6.2f%%  maxdd=%5.2f%%\n",
					w.Label, s,
					100*detail.WindowROIs[w.Label],
					100*detail.WindowMaxDDs[w.Label],
				)
			}
		}

		if err := genome.Save(championPath(ticker.Symbol), champion); err != nil {
			fmt.Printf("  ✗ save failed: %v\n", err)
		} else {
			fmt.Printf("  ✓ champion saved → %s\n", championPath(ticker.Symbol))
		}
		fmt.Println()

		trainResults = append(trainResults, trainResult{ticker.Symbol, finalScore})
	}

	// ── final summary ─────────────────────────────────────────────────────────
	sort.Slice(trainResults, func(i, j int) bool {
		return trainResults[i].Score > trainResults[j].Score
	})
	elapsed := time.Since(started).Round(time.Second)
	fmt.Printf("[%s] %2d/%2d  Complete — %v total\n\n",
		makeBar(total, total, 20), total, total, elapsed)

	fmt.Printf("%-4s %-6s %10s\n", "Rank", "Ticker", "FitScore")
	fmt.Printf("%-4s %-6s %10s\n", "----", "------", "----------")
	for i, r := range trainResults {
		fmt.Printf("%-4d %-6s %+10.4f\n", i+1, r.Symbol, r.Score)
	}
	fmt.Printf("\n→  go run ./cmd/quant compare -trained\n")
}

func makeBar(current, total, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := current * width / total
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// ── account ───────────────────────────────────────────────────────────────────

func runAccount(client *schwab.Client, accountSuffix string) {
	accounts, err := client.AccountNumbers()
	if err != nil {
		log.Fatal(err)
	}
	if len(accounts) == 0 {
		log.Fatal("no Schwab accounts returned")
	}
	selected := accounts[0]
	if accountSuffix != "" {
		found := false
		for _, acct := range accounts {
			if strings.HasSuffix(acct.AccountNumber, accountSuffix) {
				selected = acct
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("Available accounts:")
			for _, acct := range accounts {
				fmt.Printf(" %s", maskAccount(acct.AccountNumber))
			}
			fmt.Println()
			log.Fatalf("no Schwab account ending in %q", accountSuffix)
		}
	}
	acct, err := client.Account(selected.HashValue, "positions")
	if err != nil {
		log.Fatal(err)
	}
	s := acct.SecuritiesAccount
	b := s.CurrentBalances
	fmt.Printf("Account type: %s\n", s.Type)
	fmt.Printf("Account: %s\n\n", maskAccount(s.AccountNumber))
	fmt.Println("Balances")
	fmt.Printf("  Cash available for trading:  %12s\n", money(b.CashAvailableForTrading))
	fmt.Printf("  Cash available for withdraw: %12s\n", money(b.CashAvailableForWithdrawal))
	fmt.Printf("  Total cash:                  %12s\n", money(b.TotalCash))
	fmt.Printf("  Long market value:           %12s\n", money(b.LongMarketValue))
	fmt.Printf("  Liquidation value:           %12s\n", money(b.LiquidationValue))
	fmt.Println()

	sort.Slice(s.Positions, func(i, j int) bool {
		return s.Positions[i].MarketValue > s.Positions[j].MarketValue
	})
	fmt.Printf("%-8s %10s %12s %13s %12s %8s\n", "Symbol", "Qty", "Avg Cost", "Market Value", "Day P&L", "Day %")
	fmt.Printf("%-8s %10s %12s %13s %12s %8s\n", "------", "----------", "------------", "-------------", "------------", "--------")
	for _, p := range s.Positions {
		fmt.Printf("%-8s %10.2f %12s %13s %12s %7.2f%%\n",
			p.Instrument.Symbol,
			p.LongQuantity,
			money(p.AveragePrice),
			money(p.MarketValue),
			signedMoney(p.CurrentDayProfitLoss),
			p.CurrentDayProfitLossPercentage,
		)
	}
}

// ── bar loading ───────────────────────────────────────────────────────────────

func loadBars(client *schwab.Client, cfg *config.Config, symbol string, start, end time.Time, refresh bool) ([]quant.Bar, error) {
	if !refresh {
		if bars, err := schwab.LoadCachedBars(cfg.Backtest.CacheDir, symbol); err == nil && len(bars) > 0 {
			return filterBars(bars, start, end), nil
		}
	}
	bars, err := client.PriceHistory(symbol, cfg.Backtest.PeriodType, cfg.Backtest.Period,
		cfg.Backtest.FrequencyType, cfg.Backtest.Frequency, start, end)
	if err != nil {
		return nil, err
	}
	if err := schwab.SaveCachedBars(cfg.Backtest.CacheDir, symbol, bars); err != nil {
		return nil, err
	}
	return filterBars(bars, start, end), nil
}

func filterBars(bars []quant.Bar, start, end time.Time) []quant.Bar {
	startMS := start.UnixMilli()
	endMS := end.UnixMilli()
	out := make([]quant.Bar, 0, len(bars))
	for _, bar := range bars {
		if bar.TimeMS >= startMS && bar.TimeMS <= endMS {
			out = append(out, bar)
		}
	}
	return out
}

// ── output helpers ────────────────────────────────────────────────────────────

func printResults(results []backtest.Result, trained bool) {
	label := "fixed default"
	if trained {
		label = "trained champions"
	}
	fmt.Println()
	fmt.Printf("Local Schwab Daily Backtest — %s params, $10,000 per ticker\n", label)
	fmt.Println("Signal formed on close; filled at next open.")
	fmt.Println()
	fmt.Printf("%-4s %-6s %6s %11s %9s %9s %9s %9s %8s %8s %7s\n",
		"Rank", "Ticker", "Bars", "Final", "ROI", "GhostDCA", "AlphaDCA", "B&H", "MaxDD", "Sharpe", "Trades")
	fmt.Printf("%-4s %-6s %6s %11s %9s %9s %9s %9s %8s %8s %7s\n",
		"----", "------", "------", "-----------", "---------", "---------", "---------", "---------", "--------", "--------", "-------")
	for i, r := range results {
		fmt.Printf("%-4d %-6s %6d %11s %8.2f%% %8.2f%% %8.2f%% %8.2f%% %7.2f%% %8.2f %7d\n",
			i+1, r.Symbol, r.Bars,
			money(r.FinalEquity),
			100*r.ROI, 100*r.GhostDCAROI, 100*r.AlphaDCA, 100*r.BuyHoldROI,
			100*r.MaxDrawdown, r.Sharpe, r.Trades,
		)
	}
}

func writeYearlyCSV(path string, results []backtest.Result) error {
	if err := os.MkdirAll("reports", 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"ticker", "year", "bars", "final_equity", "roi_pct", "ghost_dca_pct", "alpha_dca_pct", "buy_hold_pct", "max_dd_pct", "sharpe", "trades", "fees_paid", "slippage_paid"})
	sort.Slice(results, func(i, j int) bool {
		yi, yj := results[i].Start.Year(), results[j].Start.Year()
		if yi != yj {
			return yi < yj
		}
		return results[i].Symbol < results[j].Symbol
	})
	for _, r := range results {
		_ = w.Write([]string{
			r.Symbol, strconv.Itoa(r.Start.Year()), strconv.Itoa(r.Bars),
			fmt.Sprintf("%.2f", r.FinalEquity),
			fmt.Sprintf("%.4f", 100*r.ROI),
			fmt.Sprintf("%.4f", 100*r.GhostDCAROI),
			fmt.Sprintf("%.4f", 100*r.AlphaDCA),
			fmt.Sprintf("%.4f", 100*r.BuyHoldROI),
			fmt.Sprintf("%.4f", 100*r.MaxDrawdown),
			fmt.Sprintf("%.4f", r.Sharpe),
			strconv.Itoa(r.Trades),
			fmt.Sprintf("%.2f", r.FeesPaid),
			fmt.Sprintf("%.2f", r.SlippagePaid),
		})
	}
	return w.Error()
}

func printYearlySummary(results []backtest.Result, path string) {
	type summary struct {
		Symbol        string
		Years         int
		AvgROI        float64
		AvgAlphaDCA   float64
		PositiveROI   int
		PositiveAlpha int
		WorstDD       float64
	}
	bySymbol := map[string][]backtest.Result{}
	for _, r := range results {
		bySymbol[r.Symbol] = append(bySymbol[r.Symbol], r)
	}
	var rows []summary
	for symbol, items := range bySymbol {
		var s summary
		s.Symbol = symbol
		s.Years = len(items)
		for _, r := range items {
			s.AvgROI += r.ROI
			s.AvgAlphaDCA += r.AlphaDCA
			if r.ROI > 0 {
				s.PositiveROI++
			}
			if r.AlphaDCA > 0 {
				s.PositiveAlpha++
			}
			if r.MaxDrawdown > s.WorstDD {
				s.WorstDD = r.MaxDrawdown
			}
		}
		if s.Years > 0 {
			s.AvgROI /= float64(s.Years)
			s.AvgAlphaDCA /= float64(s.Years)
		}
		rows = append(rows, s)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AvgAlphaDCA != rows[j].AvgAlphaDCA {
			return rows[i].AvgAlphaDCA > rows[j].AvgAlphaDCA
		}
		return rows[i].AvgROI > rows[j].AvgROI
	})
	fmt.Println()
	fmt.Printf("Per-year detail written to %s\n", path)
	fmt.Println("Per-ticker yearly summary, ranked by average yearly AlphaDCA")
	fmt.Println()
	fmt.Printf("%-4s %-6s %5s %9s %11s %9s %11s %9s\n", "Rank", "Ticker", "Years", "AvgROI", "AvgAlphaDCA", "ROI>0", "AlphaDCA>0", "WorstDD")
	fmt.Printf("%-4s %-6s %5s %9s %11s %9s %11s %9s\n", "----", "------", "-----", "---------", "-----------", "---------", "-----------", "---------")
	for i, r := range rows {
		fmt.Printf("%-4d %-6s %5d %8.2f%% %10.2f%% %4d/%-4d %6d/%-4d %8.2f%%\n",
			i+1, r.Symbol, r.Years,
			100*r.AvgROI, 100*r.AvgAlphaDCA,
			r.PositiveROI, r.Years,
			r.PositiveAlpha, r.Years,
			100*r.WorstDD,
		)
	}
}

func championPath(symbol string) string {
	return filepath.Join("reports", "champions", symbol+".json")
}

func money(v float64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	whole, frac := math.Modf(v)
	s := strconv.FormatInt(int64(whole), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return fmt.Sprintf("%s$%s.%02d", sign, s, int(math.Round(frac*100)))
}

func signedMoney(v float64) string {
	if v >= 0 {
		return "+" + money(v)
	}
	return "-" + money(-v)
}

func maskAccount(v string) string {
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

