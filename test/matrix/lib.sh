# Shared settings for the kernel-matrix scripts. Source it, do not run it.
#
# Everything the harness downloads or builds lives under MATRIX_HOME; nothing
# is written into the repo except test/matrix/results by `report.py`.

MATRIX_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$MATRIX_DIR/../.." && pwd)
MATRIX_HOME=${MATRIX_HOME:-$HOME/goublk-matrix}
CACHE=$MATRIX_HOME/cache
DOWNLOADS=$CACHE/downloads   # raw packages per kernel id, plus fetch.json
KERNELS=$CACHE/kernels       # vmlinuz + modules.cpio + kinfo.json per kernel id
BUILD=$MATRIX_HOME/build     # userland.cpio, payload/ and payload.cpio
RUNS=$MATRIX_HOME/runs       # one dir per matrix run
DOCKER=${DOCKER:-docker}

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

# json_get FILE KEY — print a top-level string/bool/number field of a JSON file.
json_get() { python3 -c 'import json,sys; v=json.load(open(sys.argv[1])).get(sys.argv[2], ""); print(str(v).lower() if isinstance(v, bool) else v)' "$1" "$2"; }
