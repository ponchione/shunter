#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$repo_root/scripts/check-whitespace.sh"
test_repo="$(mktemp -d)"
trap 'rm -rf "$test_repo"' EXIT

git -C "$test_repo" init -q
git -C "$test_repo" config user.name 'Shunter CI'
git -C "$test_repo" config user.email 'ci@shunter.invalid'

printf 'historical whitespace  \n' > "$test_repo/fixture.txt"
git -C "$test_repo" add fixture.txt
git -C "$test_repo" commit -q -m 'historical baseline'
baseline="$(git -C "$test_repo" rev-parse HEAD)"

printf 'clean\n' > "$test_repo/clean.txt"
git -C "$test_repo" add clean.txt
git -C "$test_repo" commit -q -m 'clean change'
clean="$(git -C "$test_repo" rev-parse HEAD)"

(
	cd "$test_repo"
	bash "$checker" "$baseline" "$clean"
	bash "$checker" '' HEAD
)

if output="$(cd "$test_repo" && bash "$checker" does-not-exist HEAD 2>&1)"; then
	echo 'whitespace checker accepted an invalid base revision' >&2
	exit 1
fi
if [[ "$output" != *'whitespace base does not resolve to a tree'* ]]; then
	echo "whitespace checker returned an unexpected invalid-base error: $output" >&2
	exit 1
fi

printf 'new whitespace  \n' > "$test_repo/bad.txt"
git -C "$test_repo" add bad.txt
git -C "$test_repo" commit -q -m 'bad change'
bad="$(git -C "$test_repo" rev-parse HEAD)"

if output="$(cd "$test_repo" && bash "$checker" "$clean" "$bad" 2>&1)"; then
	echo 'whitespace checker accepted a committed trailing-space regression' >&2
	exit 1
fi
if [[ "$output" != *'trailing whitespace.'* ]]; then
	echo "whitespace checker returned an unexpected committed-diff error: $output" >&2
	exit 1
fi

printf 'working tree whitespace  \n' >> "$test_repo/bad.txt"
if output="$(cd "$test_repo" && bash "$checker" "$bad" 2>&1)"; then
	echo 'whitespace checker accepted a working-tree trailing-space regression' >&2
	exit 1
fi
if [[ "$output" != *'trailing whitespace.'* ]]; then
	echo "whitespace checker returned an unexpected working-tree error: $output" >&2
	exit 1
fi
