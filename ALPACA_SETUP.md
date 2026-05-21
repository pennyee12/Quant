# Alpaca Paper Trading Setup

This document covers local setup and operation of `cmd/alpaca-paper`.

---

## Alpaca Account

Paper account `PA319IDR95I7` is connected and verified:
- Status: ACTIVE
- Starting cash: $100,000 ($10,000 per ticker × 10 tickers)
- Paper endpoint: `https://paper-api.alpaca.markets`

**Security note:** Regenerate the paper API keys after the local workflow is validated —
the keys were pasted into chat on 2026-05-01.

---

## Credentials

Alpaca uses header-based auth (not OAuth). Create a local `.env` from the template:

```bash
cp .env.example .env
```

Edit `.env` with your **paper** keys:

```bash
ALPACA_API_KEY_ID=...
ALPACA_API_SECRET_KEY=...
ALPACA_TRADING_BASE_URL=https://paper-api.alpaca.markets
```

`.env` is gitignored and must never be committed.

---

## Running

```bash
# Dry run — safe to run anytime, no orders placed
go run ./cmd/alpaca-paper -env .env

# Subset of tickers only
go run ./cmd/alpaca-paper -env .env -tickers TSLA,RIOT

# Execute paper orders to Alpaca
go run ./cmd/alpaca-paper -env .env -execute

# Force re-run if today's date is already recorded in state
go run ./cmd/alpaca-paper -env .env -execute -force
```

---

## All Flags

```text
-env .env              local env file for Alpaca credentials (default: .env)
-execute               submit paper orders; default is dry-run
-force                 bypass duplicate-date guard
-allow-non-trading-day run even when today is not a regular NYSE trading day
-save-dry-run          persist dry-run decisions; default dry runs are read-only
-state alpaca_state.json  local run-state file (default: alpaca_state.json)
-tickers TSLA,RIOT     run subset of tickers; default: top 10
-feed iex              Alpaca data feed: iex (free) or sip (paid)
-allocation 10000      initial capital per ticker — used only on first run for each ticker
-warmup 200            warmup bars for indicator seeding
-config config.yaml    config file
```

---

## How It Works

1. Loads Alpaca credentials from `.env`
2. Checks account is not blocked
3. Loads per-ticker state from `alpaca_state.json` (creates fresh if missing)
4. Trading-day guard: checks Alpaca's market calendar and exits when today is not a trading day
5. Duplicate-date guard: exits if today already ran (bypass with `-force`)
6. For each ticker:
   - Reconciles actual shares from Alpaca (source of truth)
   - Updates equity/ROI/drawdown from latest price
   - Fetches ~200 days of bars from Alpaca IEX feed
   - Loads champion chromosome from `reports/champions/{SYMBOL}.json`
   - Runs `strategy.Step()` to get signal and order size
   - In dry-run: prints intended action without saving state unless `-save-dry-run` is set
   - In execute mode: submits market order to Alpaca, records transaction
7. In execute mode, checks open Alpaca orders first and skips symbols that already have an open order
8. Prints per-ticker summary and account total
9. Saves state to `alpaca_state.json`

**Order timing:** Orders submitted at 4:15 PM ET are held by Alpaca and fill at
the next morning's market open automatically. No morning run needed.

---

## Per-Ticker State (alpaca_state.json)

Each ticker tracks independently — cash never crosses between tickers:

```json
{
  "symbol": "RIOT",
  "initial_capital": 10000,
  "cash": 5075.81,
  "shares": 266.17,
  "avg_entry_price": 18.50,
  "last_price": 18.50,
  "equity": 10000.00,
  "peak_equity": 10000.00,
  "max_drawdown": 0.0,
  "roi": 0.0,
  "trade_count": 1,
  "transactions": [...]
}
```

`alpaca_state.json` is gitignored and stays local.

---

## Automatic Schedule (systemd)

The app runs automatically via systemd on Linux. Unit files are in `deploy/digitalocean/`:

- **Service**: `quant-alpaca-paper.service`
- **Timer**: `quant-alpaca-paper.timer` — Mon–Fri at 4:15 PM ET
- The app uses the `America/New_York` timezone internally, so daylight saving is handled correctly

Check logs after each run:
```bash
journalctl -u quant-alpaca-paper.service -n 100 --no-pager
```

Check timer status:
```bash
systemctl list-timers quant-alpaca-paper.timer
```

To reload after editing unit files:
```bash
sudo systemctl daemon-reload
sudo systemctl restart quant-alpaca-paper.timer
```

See `deploy/digitalocean/README.md` for full installation instructions.

---

## Safety Rules

- Paper endpoint is enforced in code — will fatal if URL doesn't contain "paper" when `-execute` is set
- Execute mode checks existing open Alpaca orders and skips affected symbols to avoid duplicate orders
- Sell qty is capped at shares owned — no shorting
- Cash per ticker never goes negative — buy notional capped at available cash
- All decisions and submitted orders are logged to `alpaca_state.json` with Alpaca request IDs

---

## DigitalOcean Deployment

Cloud deployment files live in:

```text
deploy/digitalocean/
```

Follow `deploy/digitalocean/README.md` to build the Linux binary, copy the minimal package,
install the systemd service/timer, and run automatically at 4:15 PM Eastern on the droplet.

---

## Smoke Test

```bash
# Verify account connection
go run ./cmd/alpaca-paper -env .env -tickers TSLA

# Run all tests
go test ./...
```
