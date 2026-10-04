#!/bin/bash
# Build the guest payload from the current checkout: static (CGO_ENABLED=0)
# linux binaries, every package's unit tests compiled with `go test -c`, the
# integration test binary, the repo's VM scripts and payload/run, packed as a
# cpio rooted at /payload ($BUILD/payload.cpio).
set -euo pipefail
. "$(dirname "$0")/lib.sh"

export CGO_ENABLED=0 GOOS=linux GOARCH=${GOARCH:-amd64}
out=$BUILD/payload
mkdir -p "$out"
find "$out" -mindepth 1 -delete
mkdir -p "$out/bin" "$out/tests" "$out/integration" "$out/scripts"
cd "$REPO_ROOT"

# GO_UBLK_COMMIT names the tested commit when the tree is a copy without .git.
if [ -n "${GO_UBLK_COMMIT:-}" ]; then
	commit=$GO_UBLK_COMMIT
else
	commit=$(git rev-parse HEAD 2>/dev/null || echo unknown)
	git diff --quiet HEAD -- . ':!test/matrix' 2>/dev/null || commit="$commit-dirty"
fi
log "payload for $commit ($GOOS/$GOARCH)"

go build -o "$out/bin/ublk-mem" ./examples/ublk-mem
go build -o "$out/bin/ublk-loop" ./examples/ublk-loop
go build -o "$out/bin/verify" ./test/verify
go build -o "$out/bin/ublk-probe" ./test/matrix/cmd/ublk-probe
go build -o "$out/bin/ublk-suite" ./test/suite
go build -o "$out/bin/ublk-ctrl" ./test/ctrl
for pkg in $(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...); do
	name=${pkg#github.com/ehrlich-b/go-ublk}
	name=${name#/}
	name=${name:-root}
	go test -c -o "$out/tests/${name//\//_}.test" "$pkg"
done
go test -c -tags=integration -o "$out/integration/integration.test" ./test/integration
install -m 0755 "$MATRIX_DIR/payload/run" "$out/run"
install -m 0755 scripts/vm-verify.sh scripts/vm-loop-e2e.sh "$out/scripts/"
echo "$commit" >"$out/COMMIT"

python3 "$MATRIX_DIR/mkcpio.py" "$BUILD/payload.cpio.tmp" "$out" --prefix payload
mv "$BUILD/payload.cpio.tmp" "$BUILD/payload.cpio"
log "wrote $BUILD/payload.cpio ($(du -h "$BUILD/payload.cpio" | cut -f1), $(ls "$out/tests" | wc -l) unit test binaries)"
