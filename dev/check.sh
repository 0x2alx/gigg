#!/bin/sh
# The Go checks, run inside the Go build container (see dev/README.md). Fails on the first problem.
set -eu
cd "$(dirname "$0")/.."
echo "== gofmt"
bad=$(gofmt -l cmd internal web)
[ -z "$bad" ] || { echo "not gofmt-ed:"; echo "$bad"; exit 1; }
echo "== go vet"
go vet ./...
echo "== the server never runs commands or calls out"
# No package under internal/ may import os/exec (or other process/remote-call packages), and none
# may create an HTTP client or dial out. cmd/gigg's healthcheck (a loopback self-check) is exempt.
banned='os/exec|net/smtp|net/rpc|plugin|syscall/js'
deps=$(go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}} {{join .Imports " "}}' ./internal/... ./web 2>/dev/null \
  | grep -E "^github.com/0x2alx/gigg/(internal|web)" || true)
if echo "$deps" | tr ' ' '\n' | grep -qxE "$banned"; then
  echo "forbidden import in internal/:"; echo "$deps" | grep -E "(^| )($banned)( |$)"; exit 1
fi
hits=$(find internal web -name '*.go' ! -name '*_test.go' -exec grep -nE \
  'http\.(Get|Post|PostForm|Head)\(|http\.Client\{|&http\.Client|http\.DefaultClient|net\.Dial|tls\.Dial|http\.NewRequest(WithContext)?\(' {} + ) \
  || [ $? -eq 1 ]   # grep: 1 = no match is fine, 2 = error fails the check
[ -z "$hits" ] || { echo "outbound client code in internal/:"; echo "$hits"; exit 1; }
echo "== go test"
go test -count=1 ./...
echo "== check OK"
