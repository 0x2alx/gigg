#!/bin/sh
# go get + go mod tidy on a copy of the (read-only) source; writes go.mod/go.sum to $1.
# Usage (in the Go container): sh dev/tidy.sh /out [module@version ...]
set -e
out=$1; shift
rm -rf /tmp/t && cp -r /src /tmp/t && cd /tmp/t
[ $# -eq 0 ] || go get "$@"
go mod tidy
cp go.mod go.sum "$out"/
go build ./... && go vet ./... && echo TIDY-BUILD-OK
