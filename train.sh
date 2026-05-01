#!/bin/zsh
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:$PATH"
cd "/Users/yip/Projects/Quant"
go run ./cmd/quant train -pop 300 -gens 100 2>&1 | tee reports/training_300_100.log
echo
echo "=== Training complete ==="
echo "Run: go run ./cmd/quant compare -trained"
