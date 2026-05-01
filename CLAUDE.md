# Quant Local Trading App

This project is a local-first adaptation of the QuantSaaS design documents in
`交易系统文档/`.

## Current Scope

- No cloud SaaS service.
- No real orders.
- Schwab is used for market data and account smoke tests.
- Backtests and paper trading run locally.
- Each ticker gets an independent virtual portfolio.
- Initial capital defaults to 10000 USD per ticker.
- Strategy decisions run on daily completed bars.

## Iron Rules

- Backtest and paper/live paths must share the same pure `Step()` strategy function.
- Strategy code must not perform network, database, file, timer, or OS I/O.
- Schwab credentials and tokens must never be hardcoded.
- Price logic should use ratios, log returns, z-scores, or other dimensionless values.
- Avoid look-ahead bias: a signal formed at day close can only fill on a later bar.

## Validation

Run:

```bash
go test ./...
go run ./cmd/quant compare
```

