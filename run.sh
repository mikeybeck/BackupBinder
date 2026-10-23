#!/usr/bin/env bash
# Build bin/local-backup and start it.
# With no arguments, the local UI is served. Extra arguments are passed through.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$root"

mkdir -p bin
go build -o bin/local-backup ./cmd/local-backup

if [[ $# -eq 0 ]]; then
  set -- serve
fi

exec ./bin/local-backup "$@"
