#!/bin/sh
# End-to-end API check from a client container on the gigg-dev network, against an instance
# bootstrapped with `gigg admin` (ada's password in /s/pw, scout's key in /s/scout.token).
# Secrets are read from files and never printed.
set -u
command -v jq >/dev/null || apk add -q --no-cache curl jq >/dev/null 2>&1
pass=0 fail=0
check() { if eval "$2"; then echo "PASS $1"; pass=$((pass+1)); else echo "FAIL $1"; fail=$((fail+1)); fi; }
MAIN=https://gigg-dev:8443/api/v1 INT=http://gigg-dev:8080/api/v1
CA="--cacert /ca/root.crt"
K="Authorization: Bearer $(cat /s/scout.token)"
J="Content-Type: application/json"
jar=$(mktemp)
# ada logs in over HTTPS (cookie jar), creates a task through the browser API
code=$(jq -nc --arg p "$(cat /s/pw)" '{username:"ada",password:$p}' | curl -s $CA -o /tmp/login -w '%{http_code}' -c $jar -H "$J" -d @- $MAIN/auth/login)
check "ada logs in on main" '[ "$code" = 200 ] && jq -e .admin /tmp/login >/dev/null'
P=$(curl -s $CA -b $jar $MAIN/workspaces/scout/projects | jq -r '.items[0].id')
code=$(curl -s $CA -b $jar -o /tmp/t -w '%{http_code}' -H "$J" -H "Origin: https://gigg-dev:8443" -d '{"title":"Write the README","priority":3}' $MAIN/projects/$P/tasks)
check "ada creates a task (same-origin JSON)" '[ "$code" = 201 ] && [ "$(jq -r .key /tmp/t)" = GIG-1 ]'
code=$(curl -s $CA -b $jar -o /dev/null -w '%{http_code}' -H "$J" -H "Origin: https://evil.example" -d '{"title":"x"}' $MAIN/projects/$P/tasks)
check "cross-origin write refused" '[ "$code" = 403 ]'
check "scout's key refused on main" '[ "$(curl -s $CA -o /dev/null -w "%{http_code}" -H "$K" $MAIN/auth/me)" = 401 ]'
check "scout's key works on internal" '[ "$(curl -s -H "Host: gigg" -o /dev/null -w "%{http_code}" -H "$K" $INT/auth/me)" = 200 ]'
H="Host: gigg"
code=$(curl -s -H "$H" -H "$K" -H "$J" -o /tmp/c -w '%{http_code}' -d '{"created_by":["ada","scout"],"start_due":true}' $INT/workspaces/current/tasks/claim)
check "runner claims the task" '[ "$code" = 200 ] && [ "$(jq -r .status_category /tmp/c)" = doing ]'
sid=$(cat /proc/sys/kernel/random/uuid)
code=$(curl -s -H "$H" -H "$K" -H "$J" -o /tmp/r -w '%{http_code}' -d "{\"session_id\":\"$sid\",\"mode\":\"new\"}" $INT/tasks/GIG-1/runs)
RID=$(jq -r .id /tmp/r)
check "run recorded" '[ "$code" = 201 ] && [ "$RID" -gt 0 ]'
code=$(curl -s -H "$H" -H "$K" -H "$J" -o /tmp/f -w '%{http_code}' -d '{"status":"needs_human","report_md":"Siren or **chime**?","comment_kind":"question","move_to_category":"waiting","cost_usd":0.44}' $INT/runs/$RID/finish)
check "run finished, task waiting" '[ "$code" = 200 ] && [ "$(jq -r .task.status_name /tmp/f)" = "Needs review" ]'
check "ada's inbox has the question" 'curl -s $CA -b $jar $MAIN/inbox | jq -e ".items[0].question.body_md == \"Siren or **chime**?\"" >/dev/null'
code=$(curl -s $CA -b $jar -o /tmp/res -w '%{http_code}' -H "$J" -H "Origin: https://gigg-dev:8443" -d '{"comment_md":"chime please"}' $MAIN/tasks/GIG-1/resume)
check "ada resumes; task back to To-Do" '[ "$code" = 200 ] && [ "$(jq -r .status_category /tmp/res)" = todo ]'
check "summary for the hook" 'curl -s -H "$H" -H "$K" $INT/workspaces/current/summary | grep -q "To-Do (1): GIG-1 Write the README"'
# MCP over loopback with the same key
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"0"}}}'
code=$(curl -s -H "$H" -H "$K" -H "$J" -H "Accept: application/json, text/event-stream" -o /tmp/m -w '%{http_code}' -d "$init" http://gigg-dev:8080/mcp)
check "MCP initialize over loopback" '[ "$code" = 200 ] && jq -e ".result.serverInfo.name == \"gigg\"" /tmp/m >/dev/null'
list='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
check "MCP lists tools, no delete_task" 'curl -s -H "$H" -H "$K" -H "$J" -H "Accept: application/json, text/event-stream" -d "$list" http://gigg-dev:8080/mcp | jq -e "[.result.tools[].name] | (index(\"create_task\") != null) and (index(\"delete_task\") == null)" >/dev/null'
check "MCP without a key: 401" '[ "$(curl -s -H "$H" -H "$J" -o /dev/null -w "%{http_code}" -d "$init" http://gigg-dev:8080/mcp)" = 401 ]'
check "OpenAPI served" 'curl -s -H "$H" $INT/openapi.json | jq -e ".paths[\"/tasks/{id}/move\"]" >/dev/null'
echo "--- $pass passed, $fail failed"
