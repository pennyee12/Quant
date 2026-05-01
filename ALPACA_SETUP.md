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

## Safety Rule

Before any real trading:

- review order sizing
- add a max daily loss kill switch
- add a max order value limit
- add market-hours checks
- log `X-Request-ID` for every submitted order
- keep live keys completely separate from paper keys

