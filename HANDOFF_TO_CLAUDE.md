# Quant Local Trading App Handoff

Last updated: 2026-05-01

> This file is a quick-start for Claude/Codex. The authoritative full state is in `HANDOVER.md`.

---

## Read First

Read `HANDOVER.md` for the full current state. This file is a quick summary only.

---

## What Is Running Right Now

Two parallel paper trading systems:

| System | Command | Schedule | Orders |
|--------|---------|----------|--------|
| GitHub Actions | `cmd/paper` | 4:05 PM ET weekdays (cron) | Simulated (Yahoo Finance) |
| Local Linux | `cmd/alpaca-paper` | 4:15 PM ET weekdays (systemd timer) | Real Alpaca paper API |

---

## Local Alpaca Status

- Alpaca paper account `PA319IDR95I7` connected and verified
- 6 buy orders submitted 2026-05-01, queued to fill at Monday 2026-05-04 open:
  RIOT $4,924 / MARA $1,365 / TSLA $5,098 / CLSK $5,501 / SOXX $5,346 / LABU $5,808
- SOXL, TQQQ, MSTR, TSLL — strategy said hold
- systemd timer `quant-alpaca-paper.timer` fires Mon–Fri 4:15 PM ET automatically (see `deploy/digitalocean/`)

## Manual Commands

```bash
cd /home/yip/Project/Quant

# Dry run (no orders placed)
go run ./cmd/alpaca-paper -env .env

# Execute paper orders
go run ./cmd/alpaca-paper -env .env -execute

# Force re-run if today already recorded
go run ./cmd/alpaca-paper -env .env -execute -force
```

---

## Key Design Decisions Made

- Each ticker has its own isolated $10,000 initial capital — never shared between tickers
- Profits compound within each ticker's own account
- Per-ticker cash, shares, equity, ROI, max drawdown, and full transaction history stored in `alpaca_state.json` (gitignored, local only)
- Shares reconciled from actual Alpaca account each run (Alpaca is source of truth for shares)
- Cash tracked locally (Alpaca does not split cash per-ticker)
- No safety order caps — strategy sizing is trusted as-is, only capped by available cash
- No shorting — sell qty capped at shares owned
- Paper endpoint enforced in code — will fatal if live URL is set with -execute

---

## Iron Rules (Never Break)

- `strategy.Step()` must remain pure — no I/O, network, DB, timers, randomness
- Signal on close → fill at next open (no lookahead)
- Schwab credentials and Alpaca keys must never be hardcoded or committed
- `alpaca_state.json`, `.env`, `logs/` are gitignored — keep them local

---

## Security Note

Alpaca paper key was pasted into chat on 2026-05-01. Regenerate it at alpaca.markets after the local workflow is validated.

---

## Next Possible Tasks

- Monitor Monday 2026-05-04 fill results in Alpaca dashboard and `logs/alpaca-paper-close.log`
- Regenerate Alpaca paper API keys
- Explore new trading strategies (Schwartz EMA-10 was evaluated and rejected — GA champions win 27/30 tickers)
- Extend GA training with deeper Schwab history (currently starts 2016-01-01)
- Add short-selling capability (requires backtest, paper, chromosome changes)
