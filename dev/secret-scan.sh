#!/bin/sh
# Secret scan of the staged diff. Prints file, line and length only (never the line); exit 1 on a hit.
# The all-"a" test key (gigg_aaaaaaaa_aaa...) used by dev tests is allowed.
pat='BEGIN [A-Z ]*PRIVATE KEY|gigg_[a-z0-9]{8}_[A-Za-z0-9_-]{43}|tk_[0-9a-f]{20,}|(api[_-]?key|token|secret|passw(or)?d)[A-Za-z_]*["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"'][^"'"'"' ]{16,}'
# awk only numbers the added lines (file<TAB>line<TAB>text); grep -E does the matching.
hits=$(git diff --cached -U0 | awk '
  /^\+\+\+ b\// { file = substr($0, 7); next }
  /^@@/ { split($3, a, ","); line = substr(a[1], 2) - 1; next }
  /^\+/ { line++; print file "\t" line "\t" substr($0, 2) }' \
  | grep -E "$pat" | grep -vE 'gigg_a{8}_a{43}' | awk -F'\t' '{ printf "possible secret: %s:%s (length %d)\n", $1, $2, length($3) }')
[ -z "$hits" ] && exit 0
echo "$hits"
exit 1
