# Quant Local Trading App Handoff

Date: 2026-05-01

> Current note for Claude/Codex: this file was originally written earlier in the
> project and some lower sections are historical. The current authoritative
> repo summary is `HANDOVER.md`. The section below records the latest GitHub
> paper-trading state as of 2026-05-01 evening.

## Claude Takeover Update — 2026-05-02

Read `HANDOVER.md` first. It now has the authoritative latest state.

Newest user direction:

- Continue building a **local Alpaca paper trading app**.
- User prefers local Mac execution for security.
- Do not put Alpaca API keys in GitHub.
- `.env` exists locally and is ignored by git.

Alpaca paper account connection has been verified:

```text
account: PA319IDR95I7
status: ACTIVE
cash: $100,000
buying power: $200,000
trading blocked: false
```

Already committed and pushed:

- `internal/alpaca/client.go`
- `cmd/alpaca-paper/main.go`
- `ALPACA_SETUP.md`
- `.env.example`

`cmd/alpaca-paper` is dry-run by default. It only places Alpaca paper orders with
explicit `-execute`.

Verified commands:

```bash
go test ./...
go run ./cmd/alpaca-paper -env .env -tickers TSLA,RIOT
go run ./cmd/alpaca-paper -env .env
```

Latest dry-run decision summary:

```text
buy:  RIOT, MARA, TSLA, CLSK, SOXX, LABU
hold: SOXL, TQQQ, MSTR, TSLL
```

Current uncommitted WIP:

- `.gitignore` now ignores `alpaca_state.json` and `logs/`
- `internal/alpaca/state.go` exists but is not wired into `cmd/alpaca-paper`

Recommended next task:

- wire local `alpaca_state.json` into the Alpaca runner
- record decisions/orders/request IDs
- add duplicate-date guard
- add safety limits before allowing `-execute`
- keep everything paper-only

Security note: user pasted the paper secret into chat earlier. Recommend
regenerating the Alpaca paper key after the local app flow is validated.

## Latest Status — GitHub Paper Trading Is Running

The local research app has evolved into a GitHub-hosted paper-trading workflow:

- GitHub repo: `pennyee12/Quant`
- GitHub Pages dashboard: `https://pennyee12.github.io/Quant/`
- Workflow: `.github/workflows/paper-trade.yml`
- Paper state: `paper_state.json`
- Dashboard data: `docs/data.json`
- Dashboard UI: `docs/index.html`

The workflow was manually triggered and fixed until it ran successfully. It no
longer needs manual triggering for ordinary operation. The next expected
automatic run is Monday, 2026-05-04 around 4:05 PM ET.

Important paper-trading behavior:

- Scheduled runs happen after market close.
- The strategy uses the completed daily close to decide buy/sell/hold.
- New orders are stored as `PendingOrderUSD`.
- Pending orders normally fill on the next valid run using the next trading
  day's actual daily `open` from Yahoo Finance.
- The app does not know Monday's open price in advance; it waits until Yahoo
  has Monday's completed daily bar.

Manual first-day bootstrap:

- The first successful GitHub run created pending buys but did not fill them.
- User wanted the paper portfolio to start holding shares on the same day.
- Added `-fill-pending-at-close` to `cmd/paper`.
- Ran it once manually on 2026-05-01 to fill only already-pending strategy
  buy orders at the 2026-05-01 close.
- This did not force-buy tickers with no buy signal.
- Do not put `-fill-pending-at-close` into the scheduled workflow.

Filled on 2026-05-01 by manual close-fill:

```text
RIOT  about $4,924 at 2026-05-01 close
MARA  about $1,365 at 2026-05-01 close
TSLA  about $5,098 at 2026-05-01 close
CLSK  about $5,501 at 2026-05-01 close
TSLL  about $3,689 at 2026-05-01 close
SOXX  about $5,346 at 2026-05-01 close
LABU  about $5,808 at 2026-05-01 close
```

Not bought on 2026-05-01 because the strategy did not issue buy orders:

```text
SOXL, TQQQ, MSTR
```

Meaning of dashboard `Pending`:

- `Pending +$X`: strategy wants to buy about `$X` at the next open.
- `Pending -$X`: strategy wants to sell about `$X` at the next open.
- `—`: no waiting order.

Dashboard updates already made:

- Shows shares owned, position value, cash, pending order, ROI, drawdown, trade
  count, and last price.
- Recent activity shows bought/sold amount, fill price, approximate shares, and
  fill date/mode.
- `paper.DailyRecord` now includes `fill_price`, `fill_time`, and `fill_mode`.

Duplicate-run bug and fix:

- The workflow has both EST and EDT cron entries.
- Both fired on 2026-05-01, briefly creating duplicate same-day trades.
- State was restored to the clean one-trade version.
- `cmd/paper` now skips a duplicate scheduled run if today's records already
  exist for all paper tickers.
- Manual `-fill-pending-at-close` intentionally bypasses this guard.

Small immediate losses after buying:

- Expected and explained to user.
- The paper simulator applies slippage, fee/cost model, and ETF expense drag.
- A position can start a few dollars negative even if filled at the displayed
  close.

Validation:

```bash
go test ./...
```

passes after these changes.

## Purpose

This workspace started from the Chinese design docs under `交易系统文档/`.
The original SaaS/cloud architecture has been adapted into a **local-first Schwab
research and paper-trading app**:

- no cloud SaaS for now
- no real orders for now
- Schwab is used for market/account data
- backtests run locally
- each ticker is tested as its own independent virtual portfolio
- initial capital is 10000 USD per ticker
- strategy decisions use completed daily bars
- signal is formed on close and simulated fill happens on the next open

## Source Docs

Read these before making strategy or GA changes:

- `交易系统文档/系统架构设计.md`
- `交易系统文档/进化文档.md`
- `交易系统文档/Plan[含phase和提示词].md`
- `交易系统文档/GA 进化与回测模块文档.md`

Important interpretation:

- The docs require a pure shared `Step()` path for backtest and future paper/live.
- The evolution engine should optimize parameters, not memorize one lucky period.
- Fitness should compare to Ghost DCA and penalize drawdown/friction.
- The new GA document names 18 combat/environment genes explicitly, but does **not**
  provide an exact 24-gene table. A 24-gene local chromosome has now been drafted
  by extending the named genes with six practical daily-bar risk/signal genes.

## Current Project Shape

```text
cmd/quant/main.go                 CLI entrypoint
config.yaml                       Schwab/config/tickers/strategy defaults
internal/schwab/client.go         Schwab token refresh, price history, account smoke test
internal/quant/types.go           Bar, portfolio, strategy input/output, parameter structs
internal/quant/math.go            EMA, log-return volatility, drawdown, Sharpe
internal/strategy/step.go         Pure strategy function
internal/backtest/backtest.go     Next-open fill backtester with fees/slippage
internal/backtest/ghost_dca.go    Ghost DCA baseline
internal/genome/chromosome.go     24-gene chromosome draft, bounds, sample/mutate/crossover
reports/yearly_compare.csv        Full-range per-year report generated from latest run
data/bars/*.json                  Cached Schwab daily candles
```

## CLI Commands

Account smoke test:

```bash
go run ./cmd/quant account
```

Full-range comparison:

```bash
go run ./cmd/quant compare
```

Force Schwab refresh:

```bash
go run ./cmd/quant compare -refresh
```

YTD comparison:

```bash
go run ./cmd/quant compare -start 2026-01-01 -end 2026-05-01
```

Full-range comparison plus per-year CSV:

```bash
go run ./cmd/quant compare -yearly
```

Validation:

```bash
go test ./...
```

Current validation status: `go test ./...` passes.

## Schwab Setup

The Go client reads the existing token file:

```text
/Users/yip/Library/CloudStorage/Dropbox/Stock trading/data/token.json
```

It can refresh the token using credentials parsed from:

```text
/Users/yip/Projects/Stock Analysis 2/scripts/run_schwab_keepalive.sh
```

Do not hardcode credentials anywhere else.

Schwab endpoints currently used:

- `GET /marketdata/v1/pricehistory`
- `GET /trader/v1/accounts/accountNumbers`
- `GET /trader/v1/accounts/{hashValue}?fields=positions`
- `POST /v1/oauth/token` for refresh

## Tickers

User provided 24 items. BTC is disabled because Schwab equities API does not
provide spot BTC daily bars. The app tests 23 Schwab-tradable tickers:

```text
RIOT, MSTR, MARA, CLSK, XOP, COIN, WGMI, GDXJ, ARKG, URA,
IBIT, FBTC, BITB, TAN, SOXX, XME, ARKB, XBI, XLK, SMH,
VGT, QQQ, IWM
```

## Backtest Results Already Run

### Full-Range Improved Fixed Baseline

Command:

```bash
go run ./cmd/quant compare
```

Earlier notable full-range results by AlphaDCA:

```text
ARKG  ROI about +133% to +135%, AlphaDCA about +95% to +99%
MARA  ROI around -8% to -9%, AlphaDCA about +46%
XBI   ROI around +118% to +121%, AlphaDCA about +37% to +38%
CLSK  ROI around -33%, AlphaDCA about +31%
COIN  ROI around -27%, AlphaDCA about +23%
```

Interpretation:

- Fixed baseline sometimes beats DCA over full range.
- But per-year consistency is weak.
- Positive AlphaDCA does not always mean the strategy made money; sometimes it
  only means it lost less than passive exposure.

### YTD 2026 Comparison

Command:

```bash
go run ./cmd/quant compare -start 2026-01-01 -end 2026-05-01
```

Top YTD by AlphaDCA:

```text
COIN  ROI +12.47%, AlphaDCA +31.04%
IBIT  ROI +2.21%,  AlphaDCA +16.29%
ARKB  ROI +2.00%,  AlphaDCA +16.16%
FBTC  ROI +2.02%,  AlphaDCA +16.15%
BITB  ROI +1.97%,  AlphaDCA +16.07%
MSTR  ROI +19.39%, AlphaDCA +12.36%
```

Period: 2026-01-01 to 2026-05-01, 82 daily bars.

### Per-Year Report

Command:

```bash
go run ./cmd/quant compare -yearly
```

Output:

```text
reports/yearly_compare.csv
```

Top yearly summary from the latest run:

```text
XBI   11 years, AvgROI +6.44%, AvgAlphaDCA -1.96%, ROI>0 8/11, AlphaDCA>0 5/11
XOP   11 years, AvgROI +2.70%, AvgAlphaDCA -4.48%, ROI>0 8/11, AlphaDCA>0 6/11
IWM   11 years, AvgROI +4.13%, AvgAlphaDCA -5.01%, ROI>0 9/11, AlphaDCA>0 3/11
ARKG  11 years, AvgROI +5.96%, AvgAlphaDCA -8.03%, ROI>0 7/11, AlphaDCA>0 4/11
URA   11 years, AvgROI +4.62%, AvgAlphaDCA -10.23%, ROI>0 6/11, AlphaDCA>0 3/11
```

Interpretation:

- The fixed baseline is not robust enough.
- Training is justified, but fitness must reward consistency and penalize friction.

## Current Strategy

`internal/strategy/step.go` is the only strategy entrypoint.

It is pure:

- no Schwab/API calls
- no file I/O
- no DB
- no timers
- no randomness

It currently consumes:

- close prices
- portfolio snapshot
- `quant.StrategyParams`

The strategy uses:

- EMA distance
- momentum
- acceleration
- macro trend
- breakout/range position
- volatility ratio
- sigmoid target weight
- deadband/dust filtering
- max position cap
- max trade size
- rebalance cooldown
- fee/slippage handled outside in backtest

## 24-Gene Chromosome Draft

File:

```text
internal/genome/chromosome.go
```

The 24 genes are:

```text
1.  MaxDCAMonths
2.  BetaThreshold
3.  MoonPhasePressure
4.  DeadlineForcePct
5.  GCThresholdMonths
6.  GCMaxRatio
7.  TMacro
8.  TMicro
9.  TDeadline
10. EMAAnchor
11. KP
12. KV
13. KA
14. MinTradeThreshold
15. MicroReserveRate
16. SigmoidScale
17. Gamma
18. Beta
19. TrendWeight
20. BreakoutWeight
21. VolatilityWeight
22. MaxPositionWeight
23. StopLossDrawdown
24. ProfitTrimThreshold
```

Important caveat:

- Genes 1-18 are directly inspired by `GA 进化与回测模块文档.md`.
- Genes 19-24 were added creatively for the local daily-bar Schwab version.
- They are connected to actual strategy behavior, not placeholders.

The chromosome currently has:

- bounds
- default seed
- `Sample`
- `Mutate`
- `Crossover`
- `Clamp`
- `ToParams`
- JSON `Save` / `Load`

## Work Interrupted Before Training

The user asked to pause before the GA trainer was completed.

No GA training has been run yet.

What exists:

- 24-gene chromosome structure
- mapping from chromosome to strategy params
- strategy code partially expanded to use the 24 genes
- all code currently builds/tests

What does not exist yet:

- `train` CLI command
- GA population loop
- multi-window fitness evaluator
- champion JSON output per ticker
- trained comparison report
- smoke training for `COIN`, `IBIT`, `XBI`

## Recommended Next Step

Implement a `train` command:

```bash
go run ./cmd/quant train -tickers COIN,IBIT,XBI -pop 30 -gens 8
```

Suggested first smoke settings:

```text
tickers: COIN, IBIT, XBI
population: 30
generations: 8
elite ratio: 0.05 or min 2 elites
mutation probability: 0.15
mutation scale: 1.0
mutation ramp factor: 1.25
fitness windows: full / 5y / 2y / 6m or available subsets
weights: 0.40 / 0.30 / 0.20 / 0.10
```

Fitness should be close to the docs:

```text
SliceScore = AlphaDCA - 1.5 * max(0, StrategyMaxDD - GhostDCAMaxDD)
```

Add extra local penalties:

```text
trade friction is already in backtest via fee/slippage
penalize very high trade count
fatal reject if MaxDrawdown >= 88%
penalize poor yearly consistency after training/reporting
```

Outputs to create:

```text
reports/champions/COIN.json
reports/champions/IBIT.json
reports/champions/XBI.json
reports/training_summary.csv
reports/trained_compare.csv
```

After smoke training passes, scale to all 23 Schwab tickers.

## Notes for Claude

- Do not reintroduce the cloud SaaS architecture yet; user explicitly wants local control first.
- Do not place real orders.
- Keep Schwab credentials isolated.
- Do not claim the docs contain an exact 24-gene list; they do not.
- The current 24-gene set is a best-effort local design based on the new GA doc.
- Use cached `data/bars/*.json` unless user asks to refresh.
- Preserve the pure `Step()` invariant.
