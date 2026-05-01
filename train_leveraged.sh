#!/bin/zsh
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:$PATH"
cd "/Users/yip/Projects/Quant"
go run ./cmd/quant train -tickers SOXL,TQQQ,NVDL,BITX,TSLL,LABU -pop 300 -gens 100 2>&1 | tee reports/training_leveraged.log
echo
echo "=== Training complete ==="
