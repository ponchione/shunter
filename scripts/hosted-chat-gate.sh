#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

rtk go test ./examples/hosted-chat/...
rtk go build ./examples/hosted-chat/cmd/hosted-chat
rtk go run ./examples/hosted-chat/cmd/export-contract --out examples/hosted-chat/shunter.contract.json
rtk go run ./cmd/shunter contract codegen \
  --contract examples/hosted-chat/shunter.contract.json \
  --language typescript \
  --out examples/hosted-chat/frontend/src/generated/hosted_chat.ts

cd examples/hosted-chat/frontend
rtk npm install
rtk npm run typecheck
