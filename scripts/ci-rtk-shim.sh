#!/usr/bin/env bash
set -euo pipefail

# Repository gate scripts use RTK locally for compact agent output. GitHub
# Actions does not need output compaction, so this shim executes the underlying
# command unchanged while preserving the exact gate scripts. RTK's read
# subcommand corresponds to cat; every other command used by these gates is an
# executable with the same name.
if [[ "${1:-}" == "read" ]]; then
	shift
	exec cat "$@"
fi
exec "$@"
