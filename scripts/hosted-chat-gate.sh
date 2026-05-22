#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

rtk go test ./examples/hosted-chat/...
rtk go build ./examples/hosted-chat/cmd/hosted-chat
rtk go run ./examples/hosted-chat/cmd/export-contract --out examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json
describe_json="$(mktemp)"
health_json="$(mktemp)"
validate_json="$(mktemp)"
assert_json="$(mktemp)"
trap 'rm -f "$describe_json" "$health_json" "$validate_json" "$assert_json"' EXIT
rtk go run ./cmd/shunter describe --contract examples/hosted-chat/shunter.contract.json --format json > "$describe_json"
rtk go run ./cmd/shunter contract assert \
  --contract examples/hosted-chat/shunter.contract.json \
  --module hosted_chat \
  --module-version v0.1.0 \
  --contract-version 1 \
  --schema-version 1 \
  --tables 3 \
  --reducers 1 \
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
rtk go run ./cmd/shunter contract codegen \
  --contract examples/hosted-chat/shunter.contract.json \
  --language typescript \
  --out examples/hosted-chat/frontend/src/generated/hosted_chat.ts

cd examples/hosted-chat/frontend
rtk npm install
rtk npm run typecheck
