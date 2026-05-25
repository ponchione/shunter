#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

build_bin="$(mktemp)"
describe_json="$(mktemp)"
health_json="$(mktemp)"
validate_json="$(mktemp)"
assert_json="$(mktemp)"
call_json="$(mktemp)"
procedure_json="$(mktemp)"
query_json="$(mktemp)"
sql_query_json="$(mktemp)"
running_describe_json="$(mktemp)"
running_health_json="$(mktemp)"
preflight_json="$(mktemp)"
migrate_json="$(mktemp)"
run_data="$(mktemp -d)"
backup_data="$(mktemp -d)"
restored_data="$(mktemp -d)"
server_log="$(mktemp)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -f "$build_bin" "$describe_json" "$health_json" "$validate_json" "$assert_json" "$call_json" "$procedure_json" "$query_json" "$sql_query_json" "$running_describe_json" "$running_health_json" "$preflight_json" "$migrate_json" "$server_log"
  rm -rf "$run_data" "$backup_data" "$restored_data"
}
trap cleanup EXIT

free_port() {
  rtk python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()'
}

start_server() {
  local data_dir="$1"
  local port="$2"
  : > "$server_log"
  SHUNTER_DATA_DIR="$data_dir" SHUNTER_LISTEN_ADDR="127.0.0.1:${port}" \
    "$build_bin" > "$server_log" 2>&1 &
  server_pid="$!"
}

wait_for_query_ready() {
  local url="$1"
  local ready=0
  for _ in {1..40}; do
    if ! kill -0 "$server_pid" >/dev/null 2>&1; then
      rtk read "$server_log" >&2 || true
      exit 1
    fi
    if rtk go run ./cmd/shunter query \
      --url "$url" \
      --contract examples/hosted-chat/shunter.contract.json \
      --allow-dev-anonymous \
      --format json \
      recent_messages > "$query_json" 2>/dev/null; then
      ready=1
      break
    fi
    sleep 0.25
  done
  if [[ "$ready" != "1" ]]; then
    rtk read "$server_log" >&2 || true
    exit 1
  fi
}

stop_server_cleanly() {
  if ! kill "$server_pid" >/dev/null 2>&1; then
    rtk read "$server_log" >&2 || true
    exit 1
  fi
  if ! wait "$server_pid"; then
    rtk read "$server_log" >&2 || true
    exit 1
  fi
  server_pid=""
}

rtk go test ./examples/hosted-chat/...
rtk go build -o "$build_bin" ./examples/hosted-chat/cmd/hosted-chat
rtk go run ./examples/hosted-chat/cmd/export-contract --out examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json --format json > "$describe_json"
rtk go run ./cmd/shunter contract assert \
  --contract examples/hosted-chat/shunter.contract.json \
  --module hosted_chat \
  --module-version v0.1.0 \
  --contract-version 1 \
  --schema-version 1 \
  --tables 4 \
  --reducers 1 \
  --procedures 1 \
  --queries 1 \
  --views 1 \
  --format json > "$assert_json"
rtk grep '"status": "passed"' "$assert_json"
rtk grep '"name": "tables"' "$assert_json"
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json --section reducers --format json > "$describe_json"
rtk grep '"section": "reducers"' "$describe_json"
rtk grep '"name": "send_message"' "$describe_json"
rtk go run ./cmd/shunter contract validate --contract examples/hosted-chat/shunter.contract.json --format json > "$validate_json"
rtk grep '"status": "valid"' "$validate_json"
rtk grep '"scope": "contract"' "$validate_json"
rtk go run ./cmd/shunter health --contract examples/hosted-chat/shunter.contract.json --format json > "$health_json"
rtk grep '"status": "ok"' "$health_json"
rtk grep '"scope": "contract"' "$health_json"
rtk grep '"running_server_checked": false' "$health_json"
listen_port="$(free_port)"
server_url="http://127.0.0.1:${listen_port}"
rm -rf "$backup_data" "$restored_data"
rtk go run ./examples/hosted-chat/cmd/maintain preflight \
  --data-dir "$run_data" \
  --format json > "$preflight_json"
rtk grep '"status": "fresh"' "$preflight_json"
rtk grep '"compatible": true' "$preflight_json"
start_server "$run_data" "$listen_port"
wait_for_query_ready "$server_url"
rtk go run ./cmd/shunter health \
  --url "$server_url" \
  --format json > "$running_health_json"
rtk grep '"status": "ok"' "$running_health_json"
rtk grep '"scope": "running_app"' "$running_health_json"
rtk grep '"running_server_checked": true' "$running_health_json"
rtk go run ./cmd/shunter describe \
  --url "$server_url" \
  --format json > "$running_describe_json"
rtk grep '"status": "ok"' "$running_describe_json"
rtk grep '"scope": "running_app"' "$running_describe_json"
rtk grep '"Name": "hosted_chat"' "$running_describe_json"
rtk go run ./cmd/shunter call \
  --url "$server_url" \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  --format json \
  send_message '{"author":"Ada","body":"hello from hosted-chat gate"}' > "$call_json"
rtk grep '"status": "ok"' "$call_json"
rtk grep '"command": "call"' "$call_json"
rtk grep '"surface": "send_message"' "$call_json"
rtk go run ./cmd/shunter procedure \
  --url "$server_url" \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  --format json \
  send_system_message '{"body":"hello from hosted-chat procedure"}' > "$procedure_json"
rtk grep '"status": "ok"' "$procedure_json"
rtk grep '"command": "procedure"' "$procedure_json"
rtk grep '"surface": "send_system_message"' "$procedure_json"
rtk go run ./cmd/shunter query \
  --url "$server_url" \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  --format json \
  recent_messages > "$query_json"
rtk grep '"status": "ok"' "$query_json"
rtk grep '"command": "query"' "$query_json"
rtk grep '"surface": "recent_messages"' "$query_json"
rtk grep '"body": "hello from hosted-chat gate"' "$query_json"
rtk grep '"body": "hello from hosted-chat procedure"' "$query_json"
rtk go run ./cmd/shunter query \
  --url "$server_url" \
  --contract examples/hosted-chat/shunter.contract.json \
  --allow-dev-anonymous \
  --format json \
  --sql "SELECT * FROM messages ORDER BY id DESC LIMIT 10" > "$sql_query_json"
rtk grep '"status": "ok"' "$sql_query_json"
rtk grep '"command": "query"' "$sql_query_json"
rtk grep '"surface": "SELECT \* FROM messages ORDER BY id DESC LIMIT 10"' "$sql_query_json"
rtk grep '"body": "hello from hosted-chat gate"' "$sql_query_json"
rtk grep '"body": "hello from hosted-chat procedure"' "$sql_query_json"

stop_server_cleanly

rtk go run ./examples/hosted-chat/cmd/maintain preflight \
  --data-dir "$run_data" \
  --format json > "$preflight_json"
rtk grep '"status": "compatible"' "$preflight_json"
rtk grep '"compatible": true' "$preflight_json"
rtk go run ./examples/hosted-chat/cmd/maintain migrate \
  --data-dir "$run_data" \
  --format json > "$migrate_json"
rtk grep '"DataDir": "' "$migrate_json"
rtk grep '"DurableTxID":' "$migrate_json"
rtk go run ./cmd/shunter backup --data-dir "$run_data" --out "$backup_data"
rtk go run ./cmd/shunter restore --backup "$backup_data" --data-dir "$restored_data"

listen_port="$(free_port)"
server_url="http://127.0.0.1:${listen_port}"
start_server "$restored_data" "$listen_port"
wait_for_query_ready "$server_url"
rtk grep '"status": "ok"' "$query_json"
rtk grep '"body": "hello from hosted-chat gate"' "$query_json"
rtk grep '"body": "hello from hosted-chat procedure"' "$query_json"
stop_server_cleanly
rtk go run ./cmd/shunter contract codegen \
  --contract examples/hosted-chat/shunter.contract.json \
  --language typescript \
  --out examples/hosted-chat/frontend/src/generated/hosted_chat.ts

cd examples/hosted-chat/frontend
rtk npm install
rtk npm run typecheck
