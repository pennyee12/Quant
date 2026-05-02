# Quant — Handover Document

Last updated: 2026-05-01. Written for Codex / Claude to continue development.

---

## Claude Takeover Note — 2026-05-02

User wants Claude Code to continue from here. The newest direction is:

- Build a **local-first Alpaca paper trading app** on the user's Mac.
- Keep the Alpaca API key local in `.env`; do **not** put it in GitHub.
- User may later move the app to a VPS/cloud, but wants local execution first
  for better security.
- User will try to keep the Mac on; reboot only before market open or after
  market close.

Current Alpaca status:

- Alpaca paper account connection is verified.
- Account smoke test succeeded:
  - account: `PA319IDR95I7`
  - status: `ACTIVE`
  - cash: `$100,000`
  - buying power: `$200,000`
  - trading blocked: `false`
- Alpaca paper keys are in local `.env`, which is ignored by git.
- The user pasted the paper secret into chat earlier, so recommend regenerating
  the Alpaca paper key after the workflow is fully tested.

Committed Alpaca code already pushed:

- `internal/alpaca/client.go`
  - loads `.env`
  - gets account
  - gets positions
  - fetches Alpaca daily bars
  - can submit market notional buys and market quantity sells
- `cmd/alpaca-paper/main.go`
  - top-10 Alpaca-backed strategy runner
  - dry-run by default
  - uses Alpaca data with `feed=iex`
  - loads champion genes from `reports/champions/{SYMBOL}.json`
  - places paper orders only with explicit `-execute`
- `ALPACA_SETUP.md`
- `.env.example`

Verified local commands:

```bash
go test ./...

go run ./cmd/alpaca-paper -env .env -tickers TSLA,RIOT

go run ./cmd/alpaca-paper -env .env
```

Recent dry-run output for all top 10, using Alpaca IEX feed, showed:

```text
buy:  RIOT, MARA, TSLA, CLSK, SOXX, LABU
hold: SOXL, TQQQ, MSTR, TSLL
```

Important: this was dry-run only. No Alpaca orders were submitted.

Current uncommitted local changes at takeover:

- `.gitignore` modified to ignore:
  - `alpaca_state.json`
  - `logs/`
- `internal/alpaca/state.go` added but not yet wired into `cmd/alpaca-paper`.
  It defines:
  - `RunState`
  - `Decision`
  - `OrderEvent`
  - `LoadRunState`
  - `Save`

Recommended next Claude task:

1. Finish wiring local Alpaca run state into `cmd/alpaca-paper`.
2. Add flags:
   - `-state alpaca_state.json`
   - `-force` to bypass duplicate-date guard
   - maybe `-log-dir logs`
3. Record every decision and every submitted order with Alpaca `X-Request-ID`.
4. Add duplicate-date guard for local Alpaca runs, similar to `cmd/paper`.
5. Add safety gates before `-execute`:
   - paper endpoint only unless a future explicit live mode is added
   - max order notional
   - max total exposure
   - account/trading-block checks
   - no shorting unless intentionally designed
6. Keep `-execute` paper-only for now.
7. Run:

```bash
go test ./...
go run ./cmd/alpaca-paper -env .env
```

Do not commit `.env`, Alpaca keys, local logs, or local runtime state.

There is also an untracked PDF in the workspace:

```text
The 11 Trading Rules Of A Market Wizard - Marty Schwartz -.pdf
```

Treat it as user material. Do not commit it unless explicitly asked.

---

## Latest Operational Update — GitHub Paper Trading

GitHub paper trading is now running and verified.

- Workflow: `.github/workflows/paper-trade.yml`
- Dashboard: `https://pennyee12.github.io/Quant/`
- Current paper state is committed in `paper_state.json`
- Dashboard data is committed in `docs/data.json`
- Latest dashboard code is in `docs/index.html`

Important behavior:

- Normal scheduled paper trading runs after market close.
- A signal is computed from the completed daily close.
- The resulting order is saved as `PendingOrderUSD`.
- Pending orders normally fill on the next valid run using the next trading day's **open** price.
- The program does not predict the next open; it waits for Yahoo's completed daily bar, then reads the actual `open`.

Manual bootstrap on 2026-05-01:

- The first GitHub run produced pending buy orders.
- User wanted the paper portfolio to start holding shares immediately.
- Added `-fill-pending-at-close` to `cmd/paper`.
- Ran it once manually to fill existing pending orders at 2026-05-01 close.
- This is a paper-trading bootstrap mode only; the scheduled workflow still uses normal next-open fills.

Current filled paper buys from 2026-05-01:

| Symbol | Approx Filled USD | Fill Price Source |
|--------|-------------------|-------------------|
| RIOT | $4,924 | 2026-05-01 close |
| MARA | $1,365 | 2026-05-01 close |
| TSLA | $5,098 | 2026-05-01 close |
| CLSK | $5,501 | 2026-05-01 close |
| TSLL | $3,689 | 2026-05-01 close |
| SOXX | $5,346 | 2026-05-01 close |
| LABU | $5,808 | 2026-05-01 close |

No buy was made for SOXL, TQQQ, or MSTR because the strategy did not issue buy orders for them.

Dashboard improvements:

- Position cards now show shares owned, position value, cash, pending order, ROI, max drawdown, trades, and last price.
- Summary table now shows shares, cash, equity, ROI, pending order, max drawdown, trades, and days.
- Recent activity now shows buy/sell amount, fill price, approximate fill shares, and fill time/mode.
- `paper.DailyRecord` now has `fill_price`, `fill_time`, and `fill_mode`.

Duplicate-run fix:

- The workflow has two cron entries to cover EST and EDT.
- Both fired on 2026-05-01, causing an accidental duplicate same-day paper run.
- The duplicate state was corrected back to the clean one-trade state.
- `cmd/paper` now skips duplicate scheduled runs if the same date already has records for all paper tickers.
- Manual `-fill-pending-at-close` runs bypass this duplicate guard intentionally.

Small immediate losses:

- Some just-filled positions show a few dollars negative immediately.
- This is expected: the simulator applies slippage, fees/cost model, and ETF expense drag where applicable.
- It is not necessarily post-fill market movement.

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
  alpaca/             — Alpaca paper trading/data client using env credentials
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

cmd/alpaca-paper/main.go — Alpaca-backed top-10 runner; dry-run by default,
                           submits paper orders only with -execute

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

Alpaca files:

- `.env.example` — safe template only
- `.env` — local secret file, ignored by git
- `ALPACA_SETUP.md` — setup and run instructions

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

Exception: `cmd/paper -fill-pending-at-close` exists only for manual paper-trading
bootstrap/testing. It fills already-pending orders at the current daily close, then
continues to compute the next pending order. Do not enable this in the scheduled
GitHub workflow.

**After all tickers:**
- Saves `paper_state.json`
- Writes `docs/data.json` for the dashboard
- GitHub Actions commits both files back to the repo

Duplicate scheduled run protection:

- Because the workflow includes both EST and EDT cron entries, `cmd/paper` checks
  whether the current date already has records for all paper tickers.
- If yes, and not in manual close-fill mode, it skips the duplicate run and only
  refreshes dashboard JSON.

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
- Manual trigger is no longer required for normal operation.
- Next expected scheduled run after the initial 2026-05-01 setup: Monday,
  2026-05-04 around 4:05 PM ET.

GitHub Pages config: Settings → Pages → Branch: main → Folder: /docs

---

## Alpaca Paper Trading Setup

Alpaca paper API credentials are configured locally in `.env` and must not be
committed.

Current verified paper account smoke test:

- account: `PA319IDR95I7`
- status: `ACTIVE`
- cash: `$100,000`
- buying power: `$200,000`
- trading blocked: `false`

Run a dry-run top-10 strategy check:

```bash
go run ./cmd/alpaca-paper -env .env
```

Run a smaller dry-run:

```bash
go run ./cmd/alpaca-paper -env .env -tickers TSLA,RIOT
```

Submit paper orders to Alpaca only when explicitly requested:

```bash
go run ./cmd/alpaca-paper -env .env -execute
```

Design notes:

- Default mode is dry-run and places no orders.
- Uses Alpaca Market Data daily bars with `feed=iex` by default.
- Loads trained champion genes from `reports/champions/{SYMBOL}.json`.
- Uses `$10,000` virtual allocation per ticker by default.
- Reads actual Alpaca paper positions and adjusts action sizing from those
  holdings.
- Buy orders use market notional orders.
- Sell orders use market quantity orders.
- Intended future cloud path: put Alpaca keys in GitHub/VPS secrets and run
  `cmd/alpaca-paper` after market close. Start cloud in dry-run before using
  `-execute`.

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
