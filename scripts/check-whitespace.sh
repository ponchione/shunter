#!/usr/bin/env bash
set -euo pipefail

# Check only whitespace introduced after a known base. Historical fixtures,
# generated artifacts, and retained evidence may contain intentional whitespace
# and must not make every future change fail independently of its diff.
base="${1:-}"
target="${2:-}"

if [[ -z "$base" || "$base" =~ ^0+$ ]]; then
	if git rev-parse --verify --quiet 'HEAD^' >/dev/null; then
		base='HEAD^'
	else
		base="$(git hash-object -t tree /dev/null)"
	fi
fi

if ! git rev-parse --verify --quiet "${base}^{tree}" >/dev/null; then
	echo "whitespace base does not resolve to a tree: $base" >&2
	exit 2
fi

if [[ -n "$target" ]]; then
	if ! git rev-parse --verify --quiet "${target}^{tree}" >/dev/null; then
		echo "whitespace target does not resolve to a tree: $target" >&2
		exit 2
	fi
	exec git diff --check "$base" "$target" --
fi

exec git diff --check "$base" --
