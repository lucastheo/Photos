#!/usr/bin/env bash
set -euo pipefail

MAX_LINES=1000
violations=0
checked=0

while IFS= read -r -d '' file; do
  checked=$((checked + 1))
  lines=$(wc -l < "$file" | tr -d '[:space:]')
  if [ "$lines" -gt "$MAX_LINES" ]; then
    echo "FAIL: $file has $lines lines (max $MAX_LINES)" >&2
    violations=$((violations + 1))
  fi
done < <(
  find . \
    -type d \( -path './.git' -o -path '*/node_modules' -o -path '*/dist' -o -path '*/coverage' -o -path './bin' -o -path './data' \) -prune \
    -o -type f \( -name '*.go' -o -name '*.ts' -o -name '*.css' \) -print0
)

if [ "$violations" -gt 0 ]; then
  echo "lint-max-file failed: $violations file(s) exceed $MAX_LINES lines." >&2
  exit 1
fi

echo "OK: $checked file(s) checked, all <= $MAX_LINES lines."
