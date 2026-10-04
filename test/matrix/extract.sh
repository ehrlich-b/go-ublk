#!/bin/bash
# Unpack each fetched kernel into $KERNELS/<id>/ (vmlinuz, modules.cpio,
# kinfo.json). Runs where dpkg-deb, rpm2cpio+cpio, bsdtar, depmod and zstd
# exist: natively on a CI runner, or inside the toolbox image on the rig.
#
#   extract.sh [ID|GLOB ...]     default: every fetched kernel not yet extracted
set -uo pipefail
. "$(dirname "$0")/lib.sh"

# Patterns may arrive as one quoted, space-separated word (from make); split
# without letting the shell glob them against the cwd.
read -ra pats <<<"$*"
[ ${#pats[@]} -eq 0 ] && pats=("*")
mkdir -p "$KERNELS" "$CACHE/tmp"
rc=0
for dir in "$DOWNLOADS"/*/; do
	id=$(basename "$dir")
	match=
	for p in "${pats[@]}"; do [[ $id == $p ]] && match=1; done
	[ -n "$match" ] || continue
	[ "$(json_get "$dir/fetch.json" status 2>/dev/null)" = ok ] || continue
	[ -f "$KERNELS/$id/kinfo.json" ] && continue

	stage=$CACHE/tmp/$id
	mkdir -p "$stage"
	chmod -R u+w "$stage"
	find "$stage" -mindepth 1 -delete
	ok=1
	for f in "$dir"/*; do
		case $f in
		*.deb) dpkg-deb -x "$f" "$stage" || ok= ;;
		*.rpm) (cd "$stage" && rpm2cpio "$f" | cpio -idm --quiet --no-absolute-filenames) || ok= ;;
		*.sig) ;;
		*.pkg.tar.*) bsdtar -xf "$f" -C "$stage" || ok= ;;
		esac
	done
	mkdir -p "$KERNELS/$id"
	rm -f "$KERNELS/$id/extract-error.json"
	if [ -n "$ok" ] && python3 "$MATRIX_DIR/kmods.py" "$stage" "$KERNELS/$id" "$id" 2>"$stage.err"; then
		cp "$dir/fetch.json" "$KERNELS/$id/fetch.json"
		log "extracted $id"
	else
		why=$([ -n "$ok" ] && tail -1 "$stage.err" || echo "a package did not unpack")
		log "extract FAILED for $id: $why"
		python3 -c 'import json,sys; json.dump({"id": sys.argv[1], "error": sys.argv[2]}, open(sys.argv[3], "w"))' \
			"$id" "packages fetched but unusable: $why" "$KERNELS/$id/extract-error.json"
		rc=1
	fi
	rm -f "$stage.err"
	chmod -R u+w "$stage"
	find "$stage" -mindepth 1 -delete
	rmdir "$stage"
done
exit $rc
