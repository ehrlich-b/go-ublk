# Full-distro mode

Kernel-matrix mode boots each kernel straight into an initramfs, which is
fast but has no systemd, no udev and no distro userland. Full-distro mode
boots a distro's own cloud image instead: its bootloader, kernel, initrd,
systemd and udev. It is the base for the systemd/udev integration tests
(units ordering mounts after the daemon, udev-created nodes, shutdown
ordering).

How it works (`run-full.sh ID`):

- The cloud image (`images.tsv`) is downloaded once to
  `$MATRIX_HOME/cache/images/`. Each run boots a throwaway qcow2 overlay.
- A NoCloud seed (`cloud-localds`) has a single `runcmd`: mount the
  read-only payload ISO (label `GOUBLKPAYLD`) and run `guest-run.sh` from it.
- `guest-run.sh` copies the payload to `/var/tmp`, emits the same `meta`
  lines as the initramfs `/init`, runs `/payload/run`, dumps `dmesg` and
  powers off. Results come back on the second serial port, so there is no
  ssh, no port forward and no key to manage.
- `report.py parse --boot full-vm` writes the same `run.json` as a matrix run,
  so `report.py aggregate` can merge both kinds.

The image's own kernel is what gets tested. To test another kernel in a full
distro, add a cloud-init `packages:` entry and a reboot, as the original
`~/goublk-vm` recipe does for HWE 7.0. Under TCG that costs about 20 minutes
per guest, which is why the kernel sweep uses initramfs mode.

```bash
# on the rig, in the toolbox image
M=$HOME/goublk-matrix
docker run --rm -v $M:$M -e MATRIX_HOME=$M -w $M/src goublk-matrix \
  bash test/matrix/full-distro/run-full.sh full-ubuntu-24.04
```

Images without xfsprogs (Debian genericcloud, for example) skip the xfs
tests, and the suite reports that as a skip.
