#!/bin/sh
# The store and api Go tests against PostgreSQL, run inside the Go build container next to a throwaway
# PostgreSQL container (see dev/README.md). GIGG_TEST_DATABASE_URL names the database; each test gets
# its own schema in it. dev/check.sh runs the same tests on SQLite.
set -eu
cd "$(dirname "$0")/.."
url=${GIGG_TEST_DATABASE_URL:?set GIGG_TEST_DATABASE_URL, see dev/README.md}
# wait for the server to listen (a fresh container takes a few seconds)
hp=$(echo "$url" | sed -E 's#^[a-z]+://([^@/]*@)?([^/?]*).*#\2#')
host=${hp%:*}
port=5432
[ "$host" = "$hp" ] || port=${hp##*:}
i=0
until nc -z -w 1 "$host" "$port" 2>/dev/null; do
  i=$((i + 1))
  [ "$i" -lt 60 ] || { echo "PostgreSQL at $host:$port never answered"; exit 1; }
  sleep 1
done
echo "== go test on PostgreSQL (store, api)"
go test -count=1 ./internal/store/ ./internal/api/
echo "== check-pg OK"
