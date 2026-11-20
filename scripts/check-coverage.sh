#!/usr/bin/env bash
set -euo pipefail

COVER_FILE="${1:-coverage.out}"

if [ ! -f "$COVER_FILE" ]; then
  echo "FAIL: coverage profile '$COVER_FILE' not found." >&2
  exit 1
fi

awk '
BEGIN {
  prefix = "github.com/lucastheo/photos/internal"
  total_stmts = 0
  total_cov = 0
}
NR == 1 && /^mode:/ { next }
NF >= 3 {
  split($1, parts, ":")
  file = parts[1]
  pkg = file
  sub(/\/[^\/]+$/, "", pkg)
  if (pkg != prefix && index(pkg, prefix "/") != 1) {
    next
  }
  stmts = $2 + 0
  cnt = $3 + 0
  if (!($1 in block_stmts)) {
    block_stmts[$1] = stmts
  }
  block_cnt[$1] += cnt
}
END {
  for (key in block_stmts) {
    split(key, parts, ":")
    pkg = parts[1]
    sub(/\/[^\/]+$/, "", pkg)
    if (pkg != prefix && index(pkg, prefix "/") != 1) {
      continue
    }
    stmts = block_stmts[key]
    pkg_total[pkg] += stmts
    total_stmts += stmts
    if (block_cnt[key] > 0) {
      pkg_cov[pkg] += stmts
      total_cov += stmts
    }
  }
  if (total_stmts == 0) {
    print "FAIL: no statements found under " prefix " in coverage profile." > "/dev/stderr"
    exit 1
  }

  n = 0
  for (p in pkg_total) {
    n++
    pkgs[n] = p
  }
  for (i = 2; i <= n; i++) {
    key = pkgs[i]
    j = i - 1
    while (j >= 1 && pkgs[j] > key) {
      pkgs[j + 1] = pkgs[j]
      j--
    }
    pkgs[j + 1] = key
  }

  failed = 0
  print "Go package coverage (required > 90.0%):"
  for (i = 1; i <= n; i++) {
    p = pkgs[i]
    t = pkg_total[p]
    c = pkg_cov[p] + 0
    if (t == 0) {
      printf "  %-55s   0.00%% (0/0) [FAIL]\n", p
      failed = 1
      continue
    }
    pct = (c * 100.0) / t
    status = "OK"
    if (c * 10 <= t * 9) {
      status = "FAIL"
      failed = 1
    }
    printf "  %-55s %6.2f%% (%d/%d) [%s]\n", p, pct, c, t, status
  }

  overall_pct = (total_cov * 100.0) / total_stmts
  overall_status = "OK"
  if (total_cov * 10 <= total_stmts * 9) {
    overall_status = "FAIL"
    failed = 1
  }
  printf "Total internal/... coverage: %.2f%% (%d/%d) [%s]\n", overall_pct, total_cov, total_stmts, overall_status

  if (failed) {
    print "FAIL: coverage must be strictly > 90.0% overall and for every package under internal/." > "/dev/stderr"
    exit 1
  }
}
' "$COVER_FILE"
