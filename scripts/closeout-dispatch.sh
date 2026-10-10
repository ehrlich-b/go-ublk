#!/usr/bin/env bash
# No transport, VM management, installation, or network operations.
set -euo pipefail
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
[[ $PWD == "$ROOT" ]] || { echo "closeout: run from the clone root" >&2; exit 2; }
export PYTHONDONTWRITEBYTECODE=1
exec python3 "$ROOT/scripts/closeout_dispatch.py" "$@"
