# Proposed personal guest acceptance

This is a reviewable proposal, not execution authority. No guest was booted,
module loaded, device created or host configuration changed by this checkpoint.
The reviewed UAPI checkpoint `14b7dae` remains clean in its original worktree;
this slice's tested source commit is `5ad6224`.

## Existing guest and startup effects

`ublk-noble` is an existing stopped personal Lima 2.2.0 instance owned by local
uid 501 (`ehrlich`, Bryan Ehrlich), under `/Users/ehrlich/.lima/ublk-noble` on
the current Darwin arm64 Mac. It uses Apple's Virtualization.framework (`vz`),
not WSL, the Windows PC, a Slide account, or a cloud machine. Its configured
resources are 4 vCPUs, 4 GiB memory and an existing 40 GiB virtual system disk.
The disk file, existing personal SSH identity and public key exist. Only their
metadata was inspected; no image was mounted or identity contents read.
Historical console logs show `7.0.0-30-generic`, but the current installed kernel,
driver, autoload configuration and guest processes are unverified while stopped.

As currently configured, starting it would expose `/Users/ehrlich` read-only.
That fails this proposal's requirement to expose no host data filesystem, so
booting it unchanged is excluded. No raw host disk or additional disk is configured.
Starting Lima also creates instance runtime files/SSH configuration, starts its
hostagent, activates guest networking, opens SSH forwarding and, with the current
host resolver enabled, opens local TCP/UDP DNS listeners. Normal VM boot writes
its existing virtual system disk; RAM-only test data does not make the entire OS
diskless. No bridge, `socket_vmnet`, custom host provisioning, host privilege grant
or host firewall/system-DNS modification is requested or apparent in the inspected
instance configuration. Boot is therefore not an inert read-only host operation.

The [Lima default network documentation](https://lima-vm.io/docs/config/network/user/)
describes its default user-mode networking and host resolver. The
[pinned hostagent source](https://github.com/lima-vm/lima/blob/v2.2.0/pkg/hostagent/hostagent.go#L369)
shows loopback DNS listeners and instance startup actions.
[Plain mode](https://lima-vm.io/docs/config/plain/) disables host filesystem mounts,
dynamic port forwarding, containerd bootstrap and guest-agent conveniences while
preserving SSH. It still permits guest networking and base guest setup.
This is a local test guest, not a network-isolated initramfs cell.

Read-only host capacity checks found 24 GiB RAM and 10 logical CPUs; a subsequent
`memory_pressure -Q` reported 30% free. Compressor occupancy was substantial.
These are observations, not reservations. Before boot, recheck host pressure and
other work; stop admission if the host cannot comfortably accommodate the guest.

## Exact bounded proposal for approval

Request one bundle: temporarily adjust only this stopped instance to 2 vCPUs/
2 GiB, plain mode with no host mounts and no proxy-environment propagation;
boot once; inspect guest capability; run the guarded RAM-backed test if capability
already exists; collect logs; stop this instance and restore its previous YAML.
No paid resource, new credentials, package/kernel installation, additional disk,
filesystem formatting/mounting, or host kernel/security setting is part of the bundle.
The WSL CPU 8,10 partition is on a separate machine and does not cap this Mac guest.

The following are proposed commands only. Save the exact original instance YAML
and its hash first. This checkpoint observed hash
`86a4ab42fb106acc780366894d0894e41952a28c27df9732104882d1a27e5931`;
if it changes, re-inspect rather than overwrite someone else's work.

```sh
limactl edit --tty=false --cpus=2 --memory=2 --mount-none \
  --set='.plain = true | .propagateProxyEnv = false' ublk-noble
# Inspect normalized settings: zero mounts/static forwards/custom networks;
# no provisioning; existing identity present; no package upgrade/install.
limactl start --tty=false --timeout=90s ublk-noble
```

Guest preflight uses the existing personal transport and `sudo -n`; it never
requests a password or creates a credential. Record `uname -r`, architecture,
running configuration/module identity, control-node metadata, active ublk servers,
and existing ublk character/block nodes. Require an otherwise idle ublk guest with
no existing ublk devices and working io_uring. Stop on unexpected startup provisioning,
downloads, missing tools, unavailable privilege or insufficient memory. Do not fix
these requirements by changing guest security settings or installing packages.

If `/dev/ublk-control` already exists and opens with existing guest privilege,
no manual driver action is needed. If it is absent, stop unless the approval
explicitly also includes this single optional guest-only action:

```sh
# Only after recording an already-installed matching module's path/hash/vermagic:
sudo -n modprobe ublk_drv
```

This is never a host module operation. A missing/mismatched module, disabled
driver, kernel failure or unsuccessful load ends acceptance; no installation,
autoload-file edit, permission change, unload or reboot loop follows. Guest boot
itself may load already-configured guest modules; that configuration cannot be
fully verified from the stopped instance's host metadata.

Compile only the integration binary for Linux arm64 from the exact source commit
and record its hash. Transfer that one binary over existing SSH into a unique
directory under the guest's existing `/dev/shm` (tmpfs); no host mount is used.
Run it with the guest's existing root capability:

```sh
sudo -n env GO_UBLK_DISPOSABLE_TEST=1 GOMAXPROCS=2 \
  ./integration-5ad6224-linux-arm64.test \
  -test.run='^TestDisposableLargeIOPublicRunnerPaths$' \
  -test.v -test.count=1 -test.parallel=1 -test.timeout=2m
```

The test uses one 32 MiB in-memory backend per sequential subtest, one queue,
depth 8, default 1 MiB maximum I/O and encoded commands. It exercises both
`CreateAndServe` and `Create` then `Start`, opens only their returned block paths,
checks exact data across the 64 KiB boundary up to 1 MiB, and closes those owned
devices. It formats/mounts no filesystem and uses no file-backed backend.
Do not run the old broad integration target: it can report PASS after creation
failure and has skipped I/O tests.

The entire guest session has a 10-minute wall budget: 90-second startup limit,
2-minute test limit, and bounded preflight/collection/cleanup. Record test status,
kernel diagnostics, node identities and before/after ublk metadata. Require no
remaining ublk nodes/devices after the otherwise-empty guest's test. On failure,
retain evidence and attempt graceful cleanup only for an ID proven to be owned
by this run; never bulk-delete devices, kill unrelated processes, or unload the
module. Then stop only this guest:

```sh
limactl stop --tty=false ublk-noble
```

Use force-stop only if separately authorized when graceful stop fails; otherwise
report the still-running instance and stop further tests. Confirm the guest is
Stopped, release its local admission and restore only the exact instance YAML
saved for this run if nobody else changed it. No image deletion, disk resize,
global daemon restart, credential/grant change or publishing follows.

## What this test would and would not establish

Even a successful run establishes only two public large-I/O paths on the one
recorded arm64 kernel. It does not exercise the changed `GET_PARAMS` parser.
Full UAPI acceptance still needs a separate guarded protocol fixture for
ADD_DEV -> SET_PARAMS -> GET_PARAMS -> START_DEV, BASIC-only/BASIC|DISCARD,
retained lengths and character/block devt. That fixture is not included here.
No 6.6/6.8 device support, x86_64 device support or benchmark claim follows.

The earlier WSL TCG matrix remains a separate approval proposal requiring verified
task-local QEMU/kernel/initramfs artifacts and disposable guest launches. Its
running WSL kernel has `CONFIG_BLK_DEV_UBLK` disabled, so native WSL device
acceptance is blocked regardless of the new userspace test passes.
