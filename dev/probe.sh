#!/bin/sh
# HTTP probes against the dev instance, from a client container on the gigg-dev network.
# Needs curl (apk add) and the dev CA root at /ca/root.crt.
set -u
apk add -q --no-cache curl >/dev/null 2>&1
pass=0 fail=0
check() { if eval "$2"; then echo "PASS $1"; pass=$((pass+1)); else echo "FAIL $1"; fail=$((fail+1)); fi; }
C="curl -s -m 5 --cacert /ca/root.crt"
check "main: HTTPS healthz with the dev CA, HTTP/2" '[ "$($C -o /dev/null -w "%{http_code} %{http_version}" https://gigg-dev:8443/healthz)" = "200 2" ]'
check "main: listener name in healthz" '$C https://gigg-dev:8443/healthz | grep -q "\"listener\":\"main\""'
check "main: certificate not trusted without the CA" '! curl -s -m 5 -o /dev/null https://gigg-dev:8443/healthz'
check "main: UI index with CSP + HSTS" 'h=$($C -D - -o /dev/null https://gigg-dev:8443/); echo "$h" | grep -qi "content-security-policy: default-src .none." && echo "$h" | grep -qi strict-transport-security'
check "main: plain HTTP refused on the TLS port" '[ "$(curl -s -m 5 -o /dev/null -w "%{http_code}" http://gigg-dev:8443/healthz)" != 200 ]'
check "main: foreign Host header refused (421)" '[ "$($C -o /dev/null -w "%{http_code}" -H "Host: evil.example" https://gigg-dev:8443/healthz)" = 421 ]'
check "internal: HTTP healthz" '[ "$(curl -s -m 5 -o /dev/null -w "%{http_code}" http://gigg:8080/healthz)" = 200 ] || [ "$(curl -s -m 5 -o /dev/null -w "%{http_code}" -H "Host: gigg" http://gigg-dev:8080/healthz)" = 200 ]'
check "internal: listener name" 'curl -s -m 5 -H "Host: gigg" http://gigg-dev:8080/healthz | grep -q "\"listener\":\"internal\""'
check "internal: no UI" '[ "$(curl -s -m 5 -o /dev/null -w "%{http_code}" -H "Host: gigg" http://gigg-dev:8080/)" = 404 ]'
check "internal: no HSTS" '! curl -s -m 5 -D - -o /dev/null -H "Host: gigg" http://gigg-dev:8080/healthz | grep -qi strict-transport'
check "internal: foreign Host refused" '[ "$(curl -s -m 5 -o /dev/null -w "%{http_code}" -H "Host: evil.example" http://gigg-dev:8080/healthz)" = 421 ]'
echo "--- $pass passed, $fail failed"
