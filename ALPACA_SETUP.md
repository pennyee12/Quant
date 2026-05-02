# Alpaca Paper Trading API Setup

This project can use Alpaca later for paper/live order execution. For now, keep
Alpaca in paper mode only.

## What To Create In Alpaca

In the Alpaca dashboard, use the **Paper Trading** account and generate paper API
keys.

You need two values:

- `API Key`
- `Secret Key`

Paper and live credentials are different. Do not use live keys while testing.

## Endpoints

Paper trading base URL:

```text
https://paper-api.alpaca.markets
```

Live trading base URL:

```text
https://api.alpaca.markets
```

Use paper until the system has been reviewed for real trading safety.

## Required Headers

Alpaca Trading API uses key headers, not Schwab-style OAuth refresh tokens:

```text
APCA-API-KEY-ID: your_key_id
APCA-API-SECRET-KEY: your_secret_key
```

If you call `/v2/account` without those headers, Alpaca can return `403` or an
auth error. The response header `X-Request-ID` is useful when contacting Alpaca
support.

## Local Setup

Copy the example env file:

```bash
cp .env.example .env
```

Edit `.env` and fill in your **paper** keys:

```bash
ALPACA_API_KEY_ID=...
ALPACA_API_SECRET_KEY=...
ALPACA_TRADING_BASE_URL=https://paper-api.alpaca.markets
```

`.env` is ignored by git and must never be committed.

## Smoke Test With Curl

After loading your env vars:

```bash
source .env

curl -i \
  -H "APCA-API-KEY-ID: $ALPACA_API_KEY_ID" \
  -H "APCA-API-SECRET-KEY: $ALPACA_API_SECRET_KEY" \
  "$ALPACA_TRADING_BASE_URL/v2/account"
```

Expected result:

- HTTP `200`
- JSON account details
- response header `X-Request-ID: ...`

If you get `401` or `403`:

- confirm you used paper keys with `https://paper-api.alpaca.markets`
- confirm there are no extra spaces/newlines in the key values
- regenerate the paper keys if needed

## Run The Top-10 Strategy Against Alpaca

The repo has an Alpaca-backed top-10 runner:

```bash
go run ./cmd/alpaca-paper -env .env
```

Default mode is **dry-run**. It:

- reads your Alpaca paper account
- reads existing Alpaca paper positions
- fetches daily bars from Alpaca Market Data
- loads trained champion genes from `reports/champions/{SYMBOL}.json`
- prints the buy/sell/hold action the strategy would take
- does **not** submit orders

For the free/basic Alpaca market-data plan, use the IEX feed:

```bash
go run ./cmd/alpaca-paper -env .env -feed iex
```

To test a small subset:

```bash
go run ./cmd/alpaca-paper -env .env -tickers TSLA,RIOT
```

To submit Alpaca paper orders, you must explicitly add `-execute`:

```bash
go run ./cmd/alpaca-paper -env .env -execute
```

Do not use `-execute` until you are comfortable with the dry-run output.

Current behavior:

- It uses a virtual allocation of `$10,000` per ticker by default.
- It uses actual Alpaca paper position shares if they already exist.
- Buy orders are submitted as market notional orders.
- Sell orders are submitted as market quantity orders.
- Orders submitted after market close are expected to queue/execute according
  to Alpaca paper-trading rules.
- Each Alpaca response may include `X-Request-ID`; keep it for support/debugging.

Useful flags:

```text
-allocation 10000       virtual strategy allocation per ticker
-tickers TSLA,RIOT      only run selected tickers
-feed iex               data feed; use iex for free/basic plan
-execute                actually submit paper orders
```

## GitHub Actions Setup

When GitHub Actions needs to talk to Alpaca, put keys in GitHub repository
secrets:

- `ALPACA_API_KEY_ID`
- `ALPACA_API_SECRET_KEY`
- `ALPACA_TRADING_BASE_URL`

Use:

```text
ALPACA_TRADING_BASE_URL=https://paper-api.alpaca.markets
```

Do not put Alpaca secrets in `config.yaml`, `docs/data.json`, or
`paper_state.json`.

Future cloud/GitHub command, once secrets are configured:

```bash
go run ./cmd/alpaca-paper -execute
```

For the first cloud version, keep it in dry-run mode until logs look correct.

## Safety Rule

Before any real trading:

- review order sizing
- add a max daily loss kill switch
- add a max order value limit
- add market-hours checks
- log `X-Request-ID` for every submitted order
- keep live keys completely separate from paper keys
