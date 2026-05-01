# Quant — Handover Document

Last updated: 2026-05-01. Written for Codex / Claude to continue development.

---

## What This Project Is

A local-first quantitative paper trading system that:
- Runs a **Sigmoid Dynamic Balance** strategy on daily bars
- Evolves strategy parameters per ticker using a **Genetic Algorithm (GA)**
- Paper-trades the **top 10 tickers** automatically via **GitHub Actions** (free)
- Displays results on a **GitHub Pages** dashboard (free)
- Uses **Yahoo Finance** for market data (free, no auth)
- Uses **Schwab API** only for local backtesting and historical data caching

---

## Architecture

```
cmd/
  quant/main.go       — CLI: compare, train, account commands (local use)
  paper/main.go       — Paper trading step (runs in GitHub Actions daily)

internal/
  backtest/           — backtest.go (Run), ghost_dca.go (SimulateGhostDCA)
  config/config.go    — YAML config loader, TickerConfig includes leverage field
  ga/
    engine.go         — GA loop: tournament selection, crossover, mutation ramp
    fitness.go        — Multi-window fitness: 6m/2y/5y/all, alpha vs ghost DCA
  genome/
    chromosome.go     — 20-gene chromosome, Default, Sample, Mutate, Crossover, ToParams
  paper/
    state.go          — Position, DailyRecord, State; Load/Save JSON
  quant/
    types.go          — Bar, StrategyParams, StrategyInput, StrategyOutput
    math.go           — MaxDrawdown, SharpeDaily
  schwab/client.go    — Schwab OAuth2 client (local only, not used in GitHub Actions)
  strategy/step.go    — Pure Step() function: signal → target weight → order USD
  yahoo/fetch.go      — FetchDaily(): fetches OHLCV bars from Yahoo Finance

docs/
  index.html          — GitHub Pages dashboard (reads data.json, Chart.js)
  data.json           — Written by cmd/paper each run (committed by Actions)

.github/workflows/
  paper-trade.yml     — Runs cmd/paper at 4:05 PM ET weekdays, commits results

reports/
  champions/          — Per-ticker trained chromosomes: {SYMBOL}.json
  champions_100_30/   — Backup of pop=100/gen=30 run
  training.log        — pop=100/gen=30 training log
  training_300_100.log — pop=300/gen=100 training log (current champions)
  training_leveraged.log — Leveraged ETF training log
  yearly_compare.csv  — Per-year backtest results

paper_state.json      — Live paper trading state (committed to repo, updated daily)
config.yaml           — All settings; tickers include leverage: field for leveraged ETFs
```

---

## The Strategy (Iron Rules — Do Not Break)

1. **`strategy.Step()`** is pure: no I/O, no globals, no side effects.
2. Signal computed on **bar close**; order fills at **next bar's open** (no lookahead).
3. `StrategyParams` maps 1-to-1 from `genome.Chromosome.ToParams()` — no fallback(), no duplicate fields.
4. Backtest and paper paths use the same `Step()` function.

---

## Top 10 Paper Trading Tickers

Selected for profit-priority (high alpha vs DCA, accepts higher drawdown):

| # | Symbol | Type | Leverage | Avg Annual Alpha |
|---|--------|------|----------|-----------------|
| 1 | SOXL | Semiconductor ETF | 3x | +50% |
| 2 | RIOT | Bitcoin miner stock | — | -90%* |
| 3 | MARA | Bitcoin miner stock | — | +27% |
| 4 | TQQQ | Nasdaq-100 ETF | 3x | +6% |
| 5 | MSTR | Bitcoin treasury stock | — | +7% |
| 6 | TSLA | EV/tech stock | — | -19%* |
| 7 | CLSK | Bitcoin miner stock | — | +71% |
| 8 | TSLL | Tesla ETF | 2x | +128% |
| 9 | SOXX | Semiconductor ETF | — | +8% |
| 10 | LABU | Biotech ETF | 3x | +43% |

*RIOT and TSLA: strategy underperforms buy-and-hold but still included for absolute return.

---

## Champion Chromosomes

Trained with pop=300, gens=100 (early-stopped ~gen 13–39 per ticker).
Stored in `reports/champions/{SYMBOL}.json`.

The paper trading step (`cmd/paper`) loads these automatically.
If a champion file is missing, it falls back to `genome.Default`.

---

## GitHub Actions Setup (One-Time Steps for Codex/User)

1. Create a **public** GitHub repo (e.g. `github.com/pennyee/quant`)
2. Push this repo: `git remote add origin <url> && git push -u origin main`
3. In GitHub repo Settings → Pages → Source: **Deploy from branch** → `main` → `/docs`
4. The workflow file `.github/workflows/paper-trade.yml` runs automatically at 4:05 PM ET weekdays
5. Dashboard URL: `https://pennyee.github.io/quant/`

No secrets needed — Yahoo Finance requires no API key.

---

## How the Daily Paper Trade Works

`cmd/paper/main.go`:
1. Loads `paper_state.json` (positions: cash, shares, trade count, peak equity)
2. For each of the 10 tickers:
   - Fetches ~200 days of bars from Yahoo Finance (for indicator warmup)
   - Loads trained champion params from `reports/champions/{SYMBOL}.json`
   - Runs `strategy.Step()` to get target weight and order USD
   - Simulates fill at today's close (approximation; real fill would be next open)
   - Updates position: cash, shares, equity, max drawdown
   - Appends a `DailyRecord` to history
3. Saves updated `paper_state.json`
4. Writes `docs/data.json` for the dashboard

GitHub Actions then commits both files back to the repo.

---

## Known Limitations / Next Steps

- **Fill price**: paper trade fills at today's close, not next open. Introduces slight lookahead vs backtest. Fix: carry pending orders, fill at next day's open.
- **Short selling**: strategy weights are [0, 1] (long-only). Extending to [-1, 1] would allow shorting — see earlier design discussion.
- **Deeper history**: Schwab provides up to 20 years; current cache starts 2016-01-01. For tickers launched before 2006 (QQQ, SOXX, etc.), retraining with full 20-year history would improve robustness.
- **Real trading**: when ready, replace the paper fill simulation in `cmd/paper` with Schwab order placement via `internal/schwab/client.go`.
- **Expense ratio drag**: currently applied daily in backtest but not in paper trade fills. Add to `cmd/paper` for accuracy.

---

## Key Config Values (config.yaml)

```yaml
backtest:
  initial_capital: 10000   # per ticker
  fee_bps: 1               # 0.01% per trade
  slippage_bps: 5          # 0.05% per trade
  start_date: "2016-01-01" # local Schwab cache start

tickers:
  - symbol: SOXL
    type: "Semiconductors 3x leveraged ETF"
    expense_ratio: 0.0075
    leverage: 3             # informational label only; strategy doesn't use it
```

---

## Running Locally

```bash
# Backtest all tickers with trained champions
go run ./cmd/quant compare -trained

# Backtest YTD only
go run ./cmd/quant compare -trained -start 2026-01-01

# Per-year breakdown to CSV
go run ./cmd/quant compare -trained -yearly

# Train top-10 paper tickers
go run ./cmd/quant train -tickers SOXL,RIOT,MARA,TQQQ,MSTR,TSLA,CLSK,TSLL,SOXX,LABU -pop 300 -gens 100

# Run one paper trading step locally
go run ./cmd/paper -state paper_state.json -out docs/data.json

# Run all tests
go test ./...
```
