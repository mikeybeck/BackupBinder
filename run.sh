#!/usr/bin/env bash
# Build bin/backupbinder and start it.
# With no arguments, the local UI is served. Extra arguments are passed through.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$root"

mkdir -p bin
go build -o bin/backupbinder ./cmd/backupbinder

if [[ $# -eq 0 ]]; then
  set -- serve
fi

exec ./bin/backupbinder "$@"
