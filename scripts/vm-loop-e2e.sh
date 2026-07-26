#!/bin/bash
# e2e for the two examples: ublk-loop (file backend) and ublk-mem --zip.
#
# Beyond the shadow oracle, this checks what a RAM backend cannot: that bytes
# land at the right offset IN THE FILE and survive teardown, that discard
# actually returns space to the filesystem, and that the write-cache attribute
# matches what the backend really does.
#
# Run from the directory holding ./ublk-loop, ./ublk-mem and ./verify.
set -u
IMG=/var/tmp/loop-test.img
pass=0; fail=0

ok()   { echo "  PASS: $1"; pass=$((pass+1)); }
bad()  { echo "  FAIL: $1"; fail=$((fail+1)); }
chk()  { if [ "$2" = "$3" ]; then ok "$1 ($2)"; else bad "$1: got '$2' want '$3'"; fi; }

resolve_pid() {
    local sp=$1 name=$2 c
    for _ in $(seq 1 25); do
        c=$(pgrep -P "$sp" -x "$name" 2>/dev/null | head -1 || true)
        [ -n "$c" ] && { echo "$c"; return; }
        sleep 0.2
    done
    echo "$sp"
}

start() {  # start <binary> <logfile> <args...>
    local bin=$1 log=$2; shift 2
    sudo "./$bin" "$@" > "$log" 2>&1 &
    SP=$!
    DP=$(resolve_pid $SP "$bin")
    DEV=""
    for _ in $(seq 1 40); do
        DEV=$(grep -oE '/dev/ublkb[0-9]+' "$log" | head -1)
        [ -n "$DEV" ] && [ -b "$DEV" ] && break
        sleep 0.5
    done
    [ -n "$DEV" ] || { echo "  device never appeared; log:"; cat "$log"; return 1; }
}

stop() {
    sudo kill -INT "$DP" 2>/dev/null
    wait "$SP" 2>/dev/null
    sleep 0.5
}

sudo modprobe ublk_drv 2>/dev/null
sudo ./ublk-mem --del=all >/dev/null 2>&1

echo "=== 1. ublk-loop: integrity via shadow oracle (Q=4, O_DIRECT) ==="
sudo rm -f $IMG
start ublk-loop loop.log --file=$IMG --size=256M --queues=4 --depth=64 || exit 1
echo "  device=$DEV file=$IMG"
if sudo ./verify -device "$DEV" -workers 4 -duration 12s -direct > verify1.log 2>&1; then
    ok "shadow oracle clean"
else
    bad "shadow oracle mismatch"; tail -20 verify1.log
fi

echo "=== 2. bytes land at the right offset in the backing file ==="
# Three distinctive 4K patterns at offsets a RAM backend cannot distinguish.
for spec in "0:AAAA" "67112960:BBBB" "268431360:CCCC"; do
    off=${spec%%:*}; tag=${spec##*:}
    yes "$tag" | head -c 4096 > /tmp/pat_$tag
    sudo dd if=/tmp/pat_$tag of="$DEV" bs=4096 seek=$((off/4096)) count=1 \
        oflag=direct conv=notrunc status=none 2>/dev/null
done
stop   # teardown must fsync the file

for spec in "0:AAAA" "67112960:BBBB" "268431360:CCCC"; do
    off=${spec%%:*}; tag=${spec##*:}
    sudo dd if=$IMG of=/tmp/got_$tag bs=4096 skip=$((off/4096)) count=1 status=none 2>/dev/null
    if sudo cmp -s /tmp/pat_$tag /tmp/got_$tag; then
        ok "offset $off present in file after teardown"
    else
        bad "offset $off wrong in file"
    fi
done

echo "=== 3. discard returns space to the filesystem ==="
start ublk-loop loop.log --file=$IMG --size=256M --queues=4 --depth=64 || exit 1
sudo dd if=/dev/urandom of="$DEV" bs=1M count=64 oflag=direct status=none 2>/dev/null
sudo blockdev --flushbufs "$DEV"
before=$(sudo du -m $IMG | awk '{print $1}')
maxd=$(cat /sys/block/$(basename $DEV)/queue/discard_max_bytes)
sudo blkdiscard -o 0 -l $((64*1024*1024)) "$DEV" && dres=ok || dres=fail
sleep 1
after=$(sudo du -m $IMG | awk '{print $1}')
echo "  discard_max_bytes=$maxd  du before=${before}M after=${after}M  blkdiscard=$dres"
if [ "$maxd" -gt 0 ] && [ "$dres" = ok ] && [ "$after" -lt "$before" ]; then
    ok "punch-hole reclaimed $((before-after))MB"
else
    bad "discard did not reclaim space"
fi
echo "=== 4. write cache advertised as volatile (buffered mode) ==="
chk "write_cache" "$(cat /sys/block/$(basename $DEV)/queue/write_cache)" "write back"
stop

echo "=== 5. -sync mode advertises write-through ==="
start ublk-loop loop.log --file=$IMG --queues=2 --depth=32 --sync || exit 1
chk "write_cache" "$(cat /sys/block/$(basename $DEV)/queue/write_cache)" "write through"
sudo dd if=/tmp/pat_AAAA of="$DEV" bs=4096 count=1 oflag=direct status=none 2>/dev/null && \
    ok "write succeeds in O_DSYNC mode" || bad "write failed in O_DSYNC mode"
stop

echo "=== 6. -read-only mode ==="
start ublk-loop loop.log --file=$IMG --read-only --queues=1 --depth=8 || exit 1
chk "blockdev --getro" "$(sudo blockdev --getro $DEV)" "1"
if sudo dd if=/tmp/pat_AAAA of="$DEV" bs=4096 count=1 conv=notrunc status=none 2>/dev/null; then
    bad "write to read-only device SUCCEEDED"
else
    ok "write to read-only device rejected"
fi
stop

echo "=== 7. ublk-mem --zip: integrity + compression ==="
start ublk-mem zip.log --size=256M --zip --queues=4 --depth=64 || exit 1
if sudo ./verify -device "$DEV" -workers 4 -duration 12s -direct > verify2.log 2>&1; then
    ok "shadow oracle clean on compressed backend"
else
    bad "shadow oracle mismatch on compressed backend"; tail -20 verify2.log
fi
# Highly compressible data should show a large ratio.
sudo dd if=/dev/zero of="$DEV" bs=1M count=64 oflag=direct status=none
yes "compressible pattern for the zip backend" | head -c 67108864 > /tmp/compressible.bin
sudo dd if=/tmp/compressible.bin of="$DEV" bs=1M count=64 seek=64 oflag=direct status=none
stop
grep -o 'compressed backend usage.*' zip.log | tail -1
if grep -q 'compressed backend usage' zip.log; then ok "ratio reported"; else bad "no ratio reported"; fi

echo "=== 8. no leaked devices ==="
left=$(ls /dev/ublkb* 2>/dev/null | wc -l)
chk "leaked block devices" "$left" "0"
dstate=$(ps -eo stat,comm | awk '$1 ~ /^D/ {print $2}' | tr '\n' ' ')
chk "D-state processes" "${dstate:-none}" "none"

echo
echo "=============================="
echo "  PASS=$pass FAIL=$fail"
echo "=============================="
[ "$fail" -eq 0 ]
