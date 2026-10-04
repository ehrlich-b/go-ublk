#!/bin/bash
# Full-distro mode: boot a distro's own cloud image (its bootloader, kernel,
# initrd, systemd, udev) with the payload on a read-only ISO, let cloud-init
# run it, and record the result like a kernel-matrix run (boot "full-vm").
#
#   run-full.sh ID [RUNDIR]      ID from images.tsv; RUNDIR default $RUNS/full-<date>
#
# No ssh and no guest networking setup: results come back over the second
# serial port, exactly as in initramfs mode. Needs qemu-system-x86_64,
# qemu-img, cloud-localds and genisoimage (all in the toolbox image).
# Knobs: ACCEL, QEMU, MEM (default 3072), SMP, TIMEOUT (default 3600 under
# tcg), SCALE, PROFILE, TESTS.
set -uo pipefail
. "$(dirname "$0")/../lib.sh"

id=$1
rundir=${2:-$RUNS/full-$(date +%Y%m%d-%H%M%S)}
row=$(awk -F'\t' -v id="$id" '$1 == id' "$MATRIX_DIR/full-distro/images.tsv")
[ -n "$row" ] || die "no $id in full-distro/images.tsv"
IFS=$'\t' read -r _ family distro url prep <<<"$row"
ACCEL=${ACCEL:-tcg}
QEMU=${QEMU:-qemu-system-x86_64}
MEM=${MEM:-3072}
SMP=${SMP:-2}
if [ "$ACCEL" = kvm ]; then
	accel=(-accel kvm -cpu host); TIMEOUT=${TIMEOUT:-1500}; SCALE=${SCALE:-1}
else
	accel=(-accel tcg,thread=multi,tb-size=256 -cpu max); TIMEOUT=${TIMEOUT:-3600}; SCALE=${SCALE:-0.25}
fi
[ -x "$BUILD/payload/run" ] || die "no payload (make matrix-payload)"

# Resolve a * in the file name against the directory listing.
if [[ $url == *'*'* ]]; then
	dir=${url%/*}/
	re=$(basename "$url" | sed 's/\./\\./g; s/\*/[^"\/]*/')
	name=$(curl -fsSL "$dir" | grep -oE "$re" | sort -uV | tail -1)
	[ -n "$name" ] || die "$id: nothing matches $url"
	url=$dir$name
fi
img=$CACHE/images/$(basename "$url")
mkdir -p "$CACHE/images"
if [ ! -s "$img" ]; then
	log "$id: downloading $url"
	curl -fL --retry 3 -o "$img.part" "$url" && mv "$img.part" "$img" || die "$id: download failed"
fi

out=$rundir/$id
mkdir -p "$out/iso"
rm -f "$out/console.log" "$out/results.log" "$out/run.json" "$out/disk.qcow2"
fmt=$(qemu-img info --output=json "$img" | python3 -c 'import json,sys; print(json.load(sys.stdin)["format"])')
qemu-img create -q -f qcow2 -F "$fmt" -b "$img" "$out/disk.qcow2" 20G || die "$id: overlay failed"

{
	echo "#cloud-config"
	echo "runcmd:"
	[ -n "${prep:-}" ] && printf '  - [bash, -c, %s]\n' "$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$prep > /dev/console 2>&1")"
	echo '  - [bash, -c, "mkdir -p /mnt/goublk && mount -o ro LABEL=GOUBLKPAYLD /mnt/goublk && exec bash /mnt/goublk/guest-run.sh"]'
} >"$out/user-data"
printf 'instance-id: %s\nlocal-hostname: matrix\n' "$id" >"$out/meta-data"
cloud-localds "$out/seed.iso" "$out/user-data" "$out/meta-data" || die "$id: cloud-localds failed"

find "$out/iso" -mindepth 1 -delete
cp -a "$BUILD/payload" "$out/iso/payload"
install -m 0755 "$MATRIX_DIR/full-distro/guest-run.sh" "$out/iso/guest-run.sh"
printf 'MATRIX_SCALE=%s\nMATRIX_PROFILE=%s\nMATRIX_TESTS="%s"\n' "$SCALE" "${PROFILE:-full}" "${TESTS:-}" >"$out/iso/config.env"
genisoimage -quiet -R -V GOUBLKPAYLD -o "$out/payload.iso" "$out/iso" || die "$id: genisoimage failed"
find "$out/iso" -mindepth 1 -delete

log "$id: booting $distro ($ACCEL, ${SMP} cpu, ${MEM}M, timeout ${TIMEOUT}s)"
t0=$(date +%s)
timeout -k 15 "$TIMEOUT" $QEMU "${accel[@]}" -m "$MEM" -smp "$SMP" \
	-nodefaults -no-user-config -display none -no-reboot \
	-drive "file=$out/disk.qcow2,if=virtio" \
	-drive "file=$out/seed.iso,if=virtio,format=raw,readonly=on" \
	-drive "file=$out/payload.iso,if=virtio,format=raw,readonly=on" \
	-nic user,model=virtio-net-pci \
	-serial "file:$out/console.log" -serial "file:$out/results.log" \
	>"$out/qemu.log" 2>&1
rc=$?
wall=$(($(date +%s) - t0))
rm -f "$out/disk.qcow2" "$out/payload.iso" "$out/seed.iso"

printf '{"id": "%s", "family": "%s", "distro": "%s", "version": "", "upstream": "", "source": "%s", "status": "ok"}\n' \
	"$id" "$family" "$distro" "$url" >"$out/fetch.json"
python3 "$MATRIX_DIR/report.py" parse "$out" --id "$id" --rc "$rc" --wall "$wall" --accel "$ACCEL" \
	--boot full-vm --timeout "$TIMEOUT" --fetch "$out/fetch.json" --commit-file "$BUILD/payload/COMMIT"
log "$id: $(json_get "$out/run.json" status) after ${wall}s (qemu rc=$rc)"
