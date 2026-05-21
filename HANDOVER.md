# Quant — Handover Document

Last updated: 2026-05-01. Written for Codex / Claude to continue development.

---

## Current State — 2026-05-01

The project has two live paper trading systems running in parallel:

1. **GitHub Actions paper trader** (`cmd/paper`) — simulated fills using Yahoo Finance, runs daily at 4:05 PM ET via cron, committed to GitHub.
2. **Local Alpaca paper trader** (`cmd/alpaca-paper`) — submits real orders to Alpaca paper account, runs daily at 4:15 PM ET via systemd timer on Linux.

Both systems use the same `strategy.Step()` function and the same champion chromosomes from `reports/champions/`.

---

## Local Alpaca Paper Trading — Fully Operational

### Alpaca Account
- Account: `PA319IDR95I7`
- Status: ACTIVE
- Starting cash: $100,000 ($10,000 per ticker × 10 tickers)
- Paper keys: local `.env` only, never committed

### First Orders Submitted — 2026-05-01
Six buy orders submitted to Alpaca, queued to fill at Monday 2026-05-04 open:

| Ticker | Notional | Alpaca Order ID |
|--------|----------|----------------|
| RIOT   | $4,924   | a3780f74-0c60-40c8-9cee-71e6cd682d7d |
| MARA   | $1,365   | e7e62c37-9741-4b78-8978-3fb7cc4b3311 |
| TSLA   | $5,098   | 60457c94-d611-4671-8312-bf41bb6abb3d |
| CLSK   | $5,501   | ff70c38c-00e0-49ab-9461-0c7903b6210e |
| SOXX   | $5,346   | bf117466-0a23-4ce7-a61e-2652ea183316 |
| LABU   | $5,808   | 45db0896-c613-4bd3-810b-6a5a258cf5f8 |

SOXL, TQQQ, MSTR, TSLL — strategy said hold, no orders.

### Ongoing Schedule
- **systemd timer**: `quant-alpaca-paper.timer` (see `deploy/digitalocean/`)
- **Fires**: Mon–Fri at 4:15 PM ET
- **Action**: computes close signal → submits orders → Alpaca fills next morning open
- **Log**: `journalctl -u quant-alpaca-paper.service`

### Manual Run Commands
```bash
cd /home/yip/Project/Quant

# Dry run (safe, no orders placed)
go run ./cmd/alpaca-paper -env .env

# Execute paper orders
go run ./cmd/alpaca-paper -env .env -execute

# Force re-run if duplicate-date guard blocked today
go run ./cmd/alpaca-paper -env .env -execute -force
```

---

## Local Alpaca State Tracking

All per-ticker state is persisted locally in `alpaca_state.json` (gitignored).

Each ticker tracks independently:
- `cash` — starts at $10,000, decremented on buys, credited on sells, never shared
- `shares` — reconciled from actual Alpaca account each run
- `equity` — cash + shares × price (compounds as profits grow)
- `roi` — vs initial $10,000
- `max_drawdown` — worst peak-to-trough for that ticker
- `trade_count`
- `transactions` — full history: every buy/sell with date, notional, qty, estimated price, Alpaca order ID, cash balance after

Account-level summary (total across all tickers) printed after each run.

---

## cmd/alpaca-paper Flags

```text
-env .env              local env file for Alpaca credentials
-execute               submit paper orders (default: dry-run)
-force                 bypass duplicate-date guard
-state alpaca_state.json  path to local state file
-tickers TSLA,RIOT     run subset of tickers
-feed iex              Alpaca data feed (iex=free, sip=paid)
-allocation 10000      initial capital per ticker (first run only)
-warmup 200            warmup bars for indicator seeding
```

Safety enforced in code:
- Paper endpoint enforced: will fatal if TradingBaseURL does not contain "paper"
- Sell qty capped at shares owned — no shorting
- Cash per ticker never goes negative

---

## GitHub Paper Trading — Also Running

- Workflow: `.github/workflows/paper-trade.yml`
- Dashboard: `https://pennyee12.github.io/Quant/`
- Cron: 4:05 PM ET weekdays
- State: `paper_state.json` (committed to repo)
- Uses Yahoo Finance (no auth needed)

Current filled GitHub paper positions (filled 2026-05-01 at close):

| Symbol | Shares | Cash Remaining | Pending Mon |
|--------|--------|---------------|-------------|
| RIOT   | 266.04 | $5,075        | +$4,923     |
| MARA   | 119.03 | $8,635        | +$1,365     |
| TSLA   | 13.04  | $4,902        | +$4,902     |
| CLSK   | 451.80 | $4,498        | —           |
| TSLL   | 282.09 | $6,311        | —           |
| SOXX   | 11.47  | $4,653        | +$4,422     |
| LABU   | 33.44  | $4,192        | +$3,333     |
| SOXL   | —      | $10,000       | —           |
| TQQQ   | —      | $10,000       | —           |
| MSTR   | —      | $10,000       | —           |

---

## Architecture

```
cmd/
  quant/main.go         — CLI: compare, train, account commands (local use only)
  paper/main.go         — Daily paper trading step (runs in GitHub Actions)
  alpaca-paper/main.go  — Local Alpaca paper trader; dry-run by default, -execute to place orders

internal/
  alpaca/
    client.go           — Alpaca REST client: account, positions, bars, orders, cancel
    state.go            — RunState, TickerState, Transaction, Decision, OrderEvent; Load/Save
  backtest/             — backtest.go (Run), ghost_dca.go (SimulateGhostDCA)
  config/config.go      — YAML config loader
  ga/
    engine.go           — GA loop: tournament selection, crossover, mutation ramp
    fitness.go          — Multi-window fitness: 6m/2y/5y/all, alpha vs ghost DCA
  genome/
    chromosome.go       — 24-gene chromosome, Default, Sample, Mutate, Crossover, ToParams
  paper/
    state.go            — Position, DailyRecord, State; Load/Save JSON
  quant/
    types.go            — Bar, StrategyParams, StrategyInput, StrategyOutput
    math.go             — MaxDrawdown, SharpeDaily
  schwab/client.go      — Schwab OAuth2 client (local only)
  strategy/step.go      — Pure Step() function: signal → target weight → order USD
  yahoo/fetch.go        — FetchDaily(): fetches OHLCV bars from Yahoo Finance

docs/
  index.html            — GitHub Pages dashboard
  data.json             — Written by cmd/paper each run

.github/workflows/
  paper-trade.yml       — Cron: 4:05 PM ET weekdays

reports/
  champions/            — Per-ticker trained chromosomes: {SYMBOL}.json (30 tickers)
  training_300_100.log  — pop=300/gen=100 training log (current champions)

deploy/digitalocean/
  quant-alpaca-paper.service  — systemd service unit
  quant-alpaca-paper.timer    — Mon–Fri 4:15 PM ET systemd timer
```

---

## The Strategy (Iron Rules — Do Not Break)

1. `strategy.Step()` is pure: no I/O, no globals, no side effects.
2. Signal computed on **bar close**; order fills at **next bar's open** (no lookahead).
3. `StrategyParams` maps 1-to-1 from `genome.Chromosome.ToParams()`.
4. Backtest and paper paths use the same `Step()` function.

---

## Top 10 Paper Trading Tickers

| # | Symbol | Type | Avg Annual Alpha |
|---|--------|------|-----------------|
| 1 | SOXL | 3x Semiconductor ETF | +50% |
| 2 | RIOT | Bitcoin miner | -90%* |
| 3 | MARA | Bitcoin miner | +27% |
| 4 | TQQQ | 3x Nasdaq ETF | +6% |
| 5 | MSTR | Bitcoin treasury | +7% |
| 6 | TSLA | EV/tech | -19%* |
| 7 | CLSK | Bitcoin miner | +71% |
| 8 | TSLL | 2x Tesla ETF | +128% |
| 9 | SOXX | Semiconductor ETF | +8% |
| 10 | LABU | 3x Biotech ETF | +43% |

---

## Champion Chromosomes

Trained with `pop=300, gens=100`. Stored in `reports/champions/{SYMBOL}.json` — 30 files.
Both `cmd/paper` and `cmd/alpaca-paper` load these automatically, falling back to `genome.Default` if missing.

---

## Security Notes

- Alpaca paper key was pasted into chat on 2026-05-01. **Regenerate the paper API key** after the local workflow is validated.
- `.env` is gitignored — never commit it.
- `alpaca_state.json` is gitignored — local only.
- `logs/` is gitignored — local only.

---

## All 30 Tickers (config.yaml)

**Original 24:** RIOT, MARA, MSTR, CLSK, XOP, COIN, WGMI, GDXJ, ARKG, URA,
IBIT, FBTC, BITB, TAN, SOXX, XME, ARKB, XBI, XLK, SMH, VGT, QQQ, IWM, TSLA

**6 Leveraged ETFs:** SOXL, TQQQ, NVDL, BITX, TSLL, LABU

---

## CLI Commands

```bash
# Backtest all tickers with trained champions
go run ./cmd/quant compare -trained

# Per-year breakdown to CSV
go run ./cmd/quant compare -trained -yearly

# Train all 30 tickers
go run ./cmd/quant train -pop 300 -gens 100

# Run one paper trading step locally (GitHub-style simulated)
go run ./cmd/paper -state paper_state.json -out docs/data.json

# Alpaca dry run
go run ./cmd/alpaca-paper -env .env

# Alpaca execute paper orders
go run ./cmd/alpaca-paper -env .env -execute

# Run all tests
go test ./...
```

---

## Known Limitations / Next Steps

- **Short selling**: strategy weights are [0,1] (long-only). Shorting would require backtest, paper, and chromosome changes.
- **Holiday handling**: the systemd timer fires on all weekdays including market holidays. The app checks Alpaca's market calendar and exits cleanly on non-trading days.
- **Alpaca key rotation**: regenerate paper keys after workflow is validated.
- **Real trading**: replace paper Alpaca endpoint with live endpoint when ready; add kill switches and daily loss limits first.
