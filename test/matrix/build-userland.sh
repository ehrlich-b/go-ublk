#!/bin/bash
# Build the guest userland: a minimal Alpine root with the tools the payload
# scripts call, the matrix /init and a pass-through sudo, packed as an
# uncompressed newc cpio at $BUILD/userland.cpio.
#
# The root is built and packed inside an alpine container, so it needs only
# docker (rootless is fine) and works the same on a CI runner.
set -euo pipefail
. "$(dirname "$0")/lib.sh"

ALPINE_IMAGE=${ALPINE_IMAGE:-alpine:3.22}
PKGS="alpine-baselayout busybox bash coreutils util-linux procps-ng e2fsprogs xfsprogs fio kmod
      grep findutils diffutils gawk sed"
mkdir -p "$BUILD"

log "building userland from $ALPINE_IMAGE"
# shellcheck disable=SC2086
$DOCKER run --rm -v "$MATRIX_DIR:/matrix:ro" -v "$BUILD:/out" "$ALPINE_IMAGE" sh -euc "
	apk add --no-cache -q python3 >/dev/null
	apk --root /rootfs --initdb --keys-dir /etc/apk/keys \
		--repositories-file /etc/apk/repositories --no-cache -q add $(echo $PKGS)
	install -m 0755 /matrix/guest/init /rootfs/init
	mkdir -p /rootfs/usr/local/bin /rootfs/mnt/matrix /rootfs/payload
	printf '#!/bin/sh\nexec \"\$@\"\n' >/rootfs/usr/local/bin/sudo
	chmod 0755 /rootfs/usr/local/bin/sudo
	find /rootfs/var/cache/apk /rootfs/usr/share/man /rootfs/usr/share/doc -mindepth 1 -delete 2>/dev/null || true
	# busybox links its applets into bin, sbin, usr/bin and usr/sbin; drop any
	# link that would shadow the real tool installed under another of them
	# (busybox's /usr/bin/blkdiscard has no -z, util-linux's /sbin one does).
	cd /rootfs
	for d in bin sbin usr/bin usr/sbin; do
		for f in \$d/*; do
			[ -L \"\$f\" ] && [ \"\$(readlink \"\$f\")\" = /bin/busybox ] || continue
			n=\${f##*/}
			for o in bin sbin usr/bin usr/sbin; do
				[ \"\$o\" = \"\$d\" ] && continue
				if [ -e \"\$o/\$n\" ] && [ \"\$(readlink \"\$o/\$n\")\" != /bin/busybox ]; then rm \"\$f\"; break; fi
			done
		done
	done
	cd /
	python3 /matrix/mkcpio.py /out/userland.cpio.tmp /rootfs \
		--node /dev/console:c:5:1:0600 --node /dev/null:c:1:3:0666 --node /dev/ttyS0:c:4:64:0600
	mv /out/userland.cpio.tmp /out/userland.cpio
	du -sh /rootfs | sed 's/^/rootfs /'
"
log "wrote $BUILD/userland.cpio ($(du -h "$BUILD/userland.cpio" | cut -f1))"
