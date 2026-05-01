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
  quant/main.go       — CLI: compare, train, account commands (local use only)
  paper/main.go       — Daily paper trading step (runs in GitHub Actions)

internal/
  backtest/           — backtest.go (Run), ghost_dca.go (SimulateGhostDCA)
  config/config.go    — YAML config loader; TickerConfig has leverage field (informational)
  ga/
    engine.go         — GA loop: tournament selection, crossover, mutation ramp
    fitness.go        — Multi-window fitness: 6m/2y/5y/all, alpha vs ghost DCA
  genome/
    chromosome.go     — 20-gene chromosome, Default, Sample, Mutate, Crossover, ToParams
  paper/
    state.go          — Position (incl. PendingOrderUSD), DailyRecord, State; Load/Save JSON
  quant/
    types.go          — Bar, StrategyParams, StrategyInput, StrategyOutput
    math.go           — MaxDrawdown, SharpeDaily
  schwab/client.go    — Schwab OAuth2 client (local only, not used in GitHub Actions)
  strategy/step.go    — Pure Step() function: signal → target weight → order USD
  yahoo/fetch.go      — FetchDaily(): fetches OHLCV bars from Yahoo Finance (no auth)

docs/
  index.html          — GitHub Pages dashboard (reads data.json, uses Chart.js CDN)
  data.json           — Written by cmd/paper each run; committed back by Actions

.github/workflows/
  paper-trade.yml     — Cron: 4:05 PM ET weekdays; runs cmd/paper, commits results

reports/
  champions/          — Per-ticker trained chromosomes: {SYMBOL}.json (30 tickers)
  champions_100_30/   — Backup of earlier pop=100/gen=30 run
  training_300_100.log — pop=300/gen=100 training log (current champions)
  training_leveraged.log — Leveraged ETF training log
  yearly_compare.csv  — Per-year backtest results

paper_state.json      — Live paper trading state (committed to repo, updated daily)
config.yaml           — All settings; 30 tickers total (24 original + 6 leveraged ETFs)
```

---

## The Strategy (Iron Rules — Do Not Break)

1. **`strategy.Step()`** is pure: no I/O, no globals, no side effects.
2. Signal computed on **bar close**; order fills at **next bar's open** (no lookahead).
3. `StrategyParams` maps 1-to-1 from `genome.Chromosome.ToParams()` — no fallback(), no duplicate fields.
4. Backtest and paper paths use the same `Step()` function.

---

## How the Daily Paper Trade Works

`cmd/paper/main.go` runs once at 4:05 PM ET each weekday via GitHub Actions.

**Two-phase execution per ticker:**

**Phase 1 — Fill yesterday's pending order at today's open:**
- Reads `pos.PendingOrderUSD` from `paper_state.json`
- Fills at `todayBar.Open` with slippage and fee applied
- Resets `PendingOrderUSD = 0`

**Phase 2 — Compute today's signal on today's close:**
- Fetches ~200 days of bars from Yahoo Finance for indicator warmup
- Loads trained champion params from `reports/champions/{SYMBOL}.json`
- Runs `strategy.Step()` on today's close
- Stores result in `pos.PendingOrderUSD` (fills tomorrow at open)

This matches the backtest exactly: signal on bar[i] close → fill at bar[i+1] open.

**After all tickers:**
- Saves `paper_state.json`
- Writes `docs/data.json` for the dashboard
- GitHub Actions commits both files back to the repo

---

## Top 10 Paper Trading Tickers

Hardcoded in `cmd/paper/main.go` as `paperTickers`. Selected for profit-priority
(high absolute return, accepts higher drawdown):

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

*RIOT and TSLA: strategy underperforms buy-and-hold but included for absolute return magnitude.

---

## Champion Chromosomes

Trained locally with `pop=300, gens=100` (early-stopped ~gen 13–39 per ticker).
Stored in `reports/champions/{SYMBOL}.json` — 30 files total covering all tickers.

`cmd/paper` loads these automatically. Falls back to `genome.Default` if missing.

---

## All 30 Tickers (config.yaml)

**Original 24:** RIOT, MARA, MSTR, CLSK, XOP, COIN, WGMI, GDXJ, ARKG, URA,
IBIT, FBTC, BITB, TAN, SOXX, XME, ARKB, XBI, XLK, SMH, VGT, QQQ, IWM, TSLA

**6 Leveraged ETFs added:** SOXL (3x semis), TQQQ (3x Nasdaq), NVDL (2x NVDA),
BITX (2x BTC futures), TSLL (2x TSLA), LABU (3x biotech)

BTC is disabled (Schwab doesn't provide spot BTC bars).

---

## Backtest Results Summary (pop=300/gen=100 champions, full history)

Top performers by avg annual alpha vs DCA:
1. TSLL +128% | 2. BITX +103% | 3. WGMI +98% | 4. CLSK +71% | 5. COIN +64%
6. SOXL +50% | 7. LABU +43% | 8. MARA +27% | 9. TAN +15% | 10. IWM +10%

Best risk-adjusted (Sharpe): WGMI 1.57, VGT 1.56, SMH 1.30, TSLL 1.30, SOXX 1.27

Lowest drawdown: VGT 15.7%, IWM 17.7%, QQQ 21.1%

Avoid (negative alpha): XLK, XME, IBIT, FBTC, ARKB, TSLA, RIOT, NVDL

---

## GitHub Setup

- Repo: https://github.com/pennyee12/Quant
- Dashboard: https://pennyee12.github.io/Quant/
- Actions run at: 4:05 PM ET weekdays (21:05 UTC / 20:05 UTC depending on DST)
- No secrets required — Yahoo Finance needs no API key

GitHub Pages config: Settings → Pages → Branch: main → Folder: /docs

---

## Known Limitations / Next Steps

- **Short selling**: strategy weights are [0,1] (long-only). Extending to [-1,1] for
  shorting was discussed — would require backtest, paper, and chromosome changes.
- **Deeper training history**: Schwab provides up to 20 years; current cache starts
  2016-01-01. Retraining with full history would improve champion robustness.
- **Real trading**: replace paper fill simulation in `cmd/paper` with Schwab order
  placement via `internal/schwab/client.go` when ready.
- **Fill price note**: paper trade fills at next day's open (correct). First run ever
  has no pending order so just computes signal; first real fill happens the following day.
- **Holiday handling**: GitHub Actions fires on all weekdays including market holidays.
  Yahoo Finance returns no new bar on holidays so the strategy is a no-op (same bar
  fetched, no new signal computed). Could add a holiday calendar check.
- **Git identity**: committer name shows as "Peng Yi <yip@Pengs-Mac-mini.local>".
  Fix with: `git config --global user.name "Penny Ee"` and
  `git config --global user.email "pennyee@gmail.com"`

---

## Running Locally

```bash
# Backtest all tickers with trained champions
go run ./cmd/quant compare -trained

# Backtest YTD only
go run ./cmd/quant compare -trained -start 2026-01-01

# Per-year breakdown to CSV
go run ./cmd/quant compare -trained -yearly

# Train all 30 tickers
go run ./cmd/quant train -pop 300 -gens 100

# Train only leveraged ETFs
go run ./cmd/quant train -tickers SOXL,TQQQ,NVDL,BITX,TSLL,LABU -pop 300 -gens 100

# Run one paper trading step locally (writes paper_state.json and docs/data.json)
go run ./cmd/paper -state paper_state.json -out docs/data.json

# Run all tests
go test ./...
```
