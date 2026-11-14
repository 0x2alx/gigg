#!/bin/sh
# gofmt a copy of the (read-only) source and write only the changed files to $1/<path>.
set -e
out=$1
rm -rf /tmp/f && cp -r /src /tmp/f && cd /tmp/f
for f in $(gofmt -l cmd internal web); do
  gofmt -w "$f"; mkdir -p "$out/$(dirname "$f")"; cp "$f" "$out/$f"; echo "formatted $f"
done
