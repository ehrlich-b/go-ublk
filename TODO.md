# TODO.md — production backlog

Reconciled 2026-10-09 against **v0.2.0 (`e4e39b0`)**. The
[feature inventory](docs/FEATURES.md) records implementation, actual tests,
per-feature matrix passes and support tiers. It supersedes the old “prototype”
feature list and the 6.8 minimum: the copy baseline has recorded passes from 6.4.
The saved matrix is at earlier revisions, not a fresh release-tip test run.

Implemented modes include multi-queue, copy/user copy, manual/automatic zero
copy, shared-memory zero copy, batching, OS-thread ownership, best-effort CPU
placement, raw async Handler, FUA, recovery, zoned and integrity. Advanced modes
remain experimental within the inventory's stated coverage. NBD and SQPOLL are
absent. No current comparative performance claim is established by this backlog.

## Remaining work, ordered by risk

1. [ ] **Shutdown / #15 and teardown failure ownership.** Reproduce unsupervised
   shutdown with SIGHUP handling already present, capture serial output and blocked
   thread stacks, and distinguish group exit from a real core dump. Test busy/failed
   unmount, STOP timeout, held Handler and late CQEs: preserve serving resources
   until safe release; assert no writeback loss, D-state, device/fd/mmap leaks.
2. [ ] **Zero-copy and shared-region lifetime.** Inject registration, file-I/O and
   unregister errors and delayed/reordered CQEs in manual, auto, forced fallback
   and batch-auto paths. Exercise SHMEM unregister/remap under held requests and
   read-only regions; prove no early reuse/unpin and count registrations/resources.
3. [ ] **Recovery failure states/combinations.** Test failure at every acquisition
   between START/END_USER_RECOVERY, simultaneous recoverers, partial queue start,
   timeout and quiesce failure. Cover every policy plus ZC/zoned; explicitly reject
   or restore shared-region mappings. Verify acknowledged data and resource counts.
4. [ ] **Unprivileged isolation and actual udev.** Run two owners and user namespaces
   through the supplied rules/helper on a full distro; reject cross-owner access,
   forged/stale paths, ID reuse and forbidden modes. Test delayed/failed node handoff.
5. [ ] **Durability and application integrity.** Existing FLUSH/FUA dispatch and
   SIGKILL/guest-reset tests need an independent ordering/crash oracle on durable
   storage, including ZC and integrity metadata, with the backing host's cache
   removed from the failure domain. Validate lost/torn/aliased detectors; keep
   synced/unsynced witnesses. Application-layer checksums/verify-on-read remain
   the backend/application's responsibility, separate from kernel PI support.
6. [ ] **Untested semantics and adversarial progress.** Add NOUNMAP semantics tests
   (Backend has no flag parameter; ZC fallback punches holes), zone append/reset-all,
   alternate integrity formats, larger descriptor strides and DMA/segment behavior.
   Add batch buffer exhaustion, partial SQ submissions, invalid/duplicate/late CQEs,
   syscall/allocation failures, long tag reuse and device-supervisor fault tests.
   Expand existing fuzz targets with deterministic schedule replay and resource
   oracles; their current scope is documented in FEATURES.md.
7. [ ] **Fresh candidate and CI coverage.** Coordinator runs Linux tests on real VMs:
   x86_64 and arm64, feature-boundary kernels, selected full-distro shutdown/udev
   runs, race/checkptr and GC/memory-pressure stress. Persist exact revision, kernel,
   negotiated/exercised path and skip reason. Existing KVM Actions workflow is
   manual-only and explicitly unverified there; unit/race/vet/fuzz CI already exists.
8. [ ] **Minor compatibility leftovers / #26.** Apply `explainControlError` to the
   ListDevices path reached by fixed-ID checks; make io_uring tests skip unavailable
   SQE128/CQE32 or disabled io_uring. Preserve a precise unsupported result.
9. [ ] **Performance, after affected correctness gates.** Profile existing copy,
   user-copy, manual/auto ZC, batch, Inline/async and backend paths under equal CPU
   budgets; verify effective affinity and expose placement failures. Measure
   throughput, CPU/I/O, QD1 and tail latency, allocations/GC effects and noise.
   Benchmark FLUSH/FUA coalescing only with a proven ordering oracle. SQPOLL/IOPOLL
   are future experiments if profiles justify them; NBD is a separate product
   expansion, not a prerequisite for optimizing the transport.

The five highest-risk paths and concrete closing tests are in
[FEATURES.md → Gaps](docs/FEATURES.md#gaps). Supervision and fencing under a
permanently wedged backend remain part of the shutdown/recovery gates.

## Implemented work — do not reopen as new features

- [x] Multi-queue default: `DefaultParams.NumQueues=0` resolves to CPU count and
  ADD_DEV's answer is reconciled (`backend.go:454`, `:1229`, `:433`); fixed mmap
  stride (`internal/queue/queue.go:128`, original fix `9eb3b17`).
- [x] Normal and failed-start teardown, bounded control waits, STOP-before-release,
  memory retention while held, queue supervision, graceful context stop and
  terminal Stop (#3/#7/#8/#17–#23; `backend.go:697`, `:873`, `:902`, `:966`).
- [x] FLUSH, volatile-cache default, independent DISCARD/WRITE_ZEROES advertisement
  and safe large-range results/limits (`internal/queue/adapter.go:55`,
  `internal/ctrl/device.go:132`, `internal/queue/request.go:134`; #9/#11/#13/#16/#24).
- [x] Per-I/O FUA interface/dispatch and ZC `RWF_DSYNC`
  (`request.go:81`, `internal/queue/adapter.go:43`, `internal/queue/engine.go:1062`).
- [x] Async backend API via Handler/Request, goroutine and Inline dispatch, eventfd
  completion wakeups (`request.go:22`, `internal/queue/engine.go:689`, `:1121`).
- [x] Manual zero copy, AUTO_BUF_REG and NEED_REG_BUF fallback; sparse fixed-buffer
  table; batch PREP/multishot FETCH/partial COMMIT
  (`internal/queue/engine.go:223`, `:264`, `:463`, `:983`, `:1013`).
- [x] User copy, NEED_GET_DATA and per-queue tag-range threads; queue OS-thread
  ownership and best-effort affinity (`internal/queue/engine.go:192`, `:641`,
  `:802`; `internal/queue/queue.go:178`). Effective placement still needs a test.
- [x] USER_RECOVERY queue/reissue/fail modes, Detach/Recover and batch/integrity
  restoration (`backend.go:254`, `recover.go:223`, `:282`, `:312`; #25).
- [x] Typed control commands, feature/parameter negotiation, shared memory, zoned,
  integrity, resize, safe stop, no partition scan and extended descriptors
  (`internal/ctrl/commands.go`, `recover.go`, `backend.go:1268`). Bindings and
  implementations have the distinct test scopes listed in the inventory.
- [x] RAM/compressed and sparse-file backend examples on the public API
  (`examples/ublk-mem/{mem,zip}.go`, `examples/ublk-loop/loop.go`); NBD absent.
- [x] Shipped systemd service/mount and SIGHUP handling (`examples/systemd/`,
  `examples/ublk-loop/main.go:131`, `examples/ublk-mem/main.go:202`);
  `e1f67d7` fixes mount ordering before user.slice. #15 diagnosis remains open.
- [x] Crash/reset oracle with independent synced/unsynced regions and injected
  lost/torn/aliased failures (`test/crash/main.go:100`, `:231`, `:446`,
  `scripts/vm-crash.sh`); host power-loss coverage remains open.
- [x] Unit/model tests and fuzz foundation: C UAPI fixtures, decoder fuzz, real
  queue-engine/fake-kernel fuzz (`internal/uapi/kernel_fixture_test.go:319`,
  `internal/{uapi,ctrl,queue}/fuzz_test.go`); bounded CI fuzz, race/vet, real-kernel
  suite/matrix and storm/soak harnesses (`.github/workflows/`, `test/suite/`,
  `test/matrix/`, `scripts/vm-{shutdown-storm,soak}.sh`). More coverage remains open.
- [x] Remove obsolete logger/shared-global barrier work (#4/#5); mmap buffers,
  request preallocation and io_uring submission batching already exist
  (`internal/queue/queue.go:151`, `engine.go:163`, `:541`, `internal/uring/ring.go:359`).
- [x] Metrics/Observer, percentile histograms and structured errors
  (`metrics.go`, `metrics_percentile_test.go`, `errors.go`).

---

## Critical Bugs history

Status reconciled against v0.2.0 on 2026-10-09. Earlier diagnoses and results below
are historical observations; implementation details describe the fixing version,
which may since have been replaced by the queue/io_uring engine. Current paths,
test scope and recorded kernel coverage are in [FEATURES.md](docs/FEATURES.md).
#15 remains an investigation; #26 remains open. #4/#5 are obsolete, not work items.

1. **[FIXED — commit 9eb3b17] Multi-queue completion loss → unkillable I/O hang.**
   Root cause was NOT dropped io_uring completions: `mmapQueues` computed the per-queue
   descriptor mmap offset as `queueID * round_up(queue_depth*24, PAGE)`, but the kernel
   (`ublk_ch_mmap`) derives the queue as `phys_off / round_up(UBLK_MAX_QUEUE_DEPTH*24, PAGE)`
   — a FIXED stride keyed on `UBLK_MAX_QUEUE_DEPTH` (4096), independent of the actual depth.
   For any realistic depth that offset floored to q_id 0, so every queue ≥1 aliased queue 0's
   descriptor buffer → wrong descriptors → state-machine violations that killed the queue's
   ioLoop → its requests wedged in D-state. Rate scaled as (N−1)/N (2q ~50%, 4q ~70%),
   exactly the fraction of I/O landing on a non-zero queue. Fixed by using the kernel's fixed
   stride. Verified: Q=1/2/4/8, O_DIRECT, concurrent, on arm64 — 0 hangs across many runs.

2. **[FIXED — commit 9eb3b17] Intermittent data corruption, multi-queue.**
   Same root cause as #1 (descriptor aliasing): queues ≥1 read queue 0's descriptors and
   did I/O to the wrong offsets. Fixed by the same change. Verified byte-exact O_DIRECT
   read-after-write across all queues (taskset-pinned) — 0 mismatches across many runs.

3. **[FIXED — commit b3846fe] DEL_DEV teardown hang.**
   Four stacked bugs: `runner.Close()` never joined the ioLoop goroutine; `Device.Close()`
   tore down queue rings *before* STOP_DEV; the original `/dev/ublkcN` fd (dup'd to each queue)
   was never closed; the io_uring fixed-file registration wasn't unregistered before close.
   DEL_DEV blocked forever, masked by the example's 1s watchdog + `os.Exit(0)`.

4. **[OBSOLETE — 2026-10-04] Verbose logging stalls I/O under load.** The old
   runner logged requests through a blocking logger mutex. The v0.2.0 engine has
   no per-request logging: `internal/queue/engine.go:665` logs fatal errors and
   `:770` logs handler panics only. `backend.go:494` documents this scope. Delete
   the old optimization task; error-path logging can still block a caller's logger.

5. **[OBSOLETE — 2026-10-04] Memory fences use one shared global.** The old
   `barrierDummy` RMW is gone. `internal/uring/ring.go:359` publishes ring indices
   with atomics; `internal/uring/ring_index_test.go:73` tests wrap/publication.
   Preserve the historical diagnosis, delete the shared-barrier optimization task.

6. **[FIXED — commit 5f23336] Requested queue count not reconciled with kernel.**
   The kernel clamps `nr_hw_queues` at ADD_DEV (notably to the online CPU count). We created a
   runner per *requested* queue, so asking for more queues than CPUs made the extra queues'
   descriptor mmap fail with EINVAL and wedged startup. Now we read back the kernel's actual
   `nr_hw_queues` after ADD_DEV and create exactly that many runners (`--queues=8` on a 4-CPU
   box → 4 queues, starts cleanly).

7. **[FIXED — commits 55303ca, 5f23336] Failed multi-queue startup wedged the process.**
   The creation-failure cleanup closed runners while their ioLoops were parked in an UNBOUNDED
   `io_uring_enter` (nothing woke them, since the device was never STARTed so STOP_DEV is a
   no-op), then called DEL_DEV while the original `/dev/ublkcN` fd was still open (DEL blocks on
   the refcount) → permanent hang requiring a reboot. Fixed two ways: (a) the data-plane wait is
   now BOUNDED (`IORING_ENTER_EXT_ARG`, 100ms) so ioLoops observe context cancellation and exit
   without kernel help; (b) the error path releases every char-device fd (dups + original) before
   DEL_DEV. A mid-startup failure now tears down in ~0.1s with no zombie device, no reboot.

8. **[FIXED — commit 8ae839c] Control-plane completion wait + LIVE-teardown ordering.**
   Two distinct defects shared one symptom (STOP-under-load hang / START flakiness):
   - **Control-plane wait.** `submitAndWait` did one `submitAndWaitRing(1,1)` then a fixed 5×10µs
     poll. `UBLK_F_URING_CMD_COMP_IN_TASK` defers the completion to task_work and Go's async
     preemption (SIGURG) interrupts the wait with EINTR, so the single wait + short poll could
     miss a completion it WOULD receive → START_DEV intermittently failed ("no completions
     available after retries"), and a stuck STOP blocked forever. Fixed with a bounded blocking
     RE-WAIT loop (reliable submit, then re-enter `io_uring_enter` across EINTR/ETIME until the
     CQE appears, 10s overall cap) plus a stale-completion drain — mirrors the data-plane
     `WaitForCompletion`. This alone made START reliable and converted the STOP hang from an
     unkillable D-state into a clean bounded timeout.
   - **Teardown ordering (the real STOP hang).** The kernel's `del_gendisk` drains in-flight I/O
     before STOP_DEV returns, and only the running ioLoops can complete that I/O
     (`COMMIT_AND_FETCH`). But `Close()`/`Stop()` (and the example's SIGINT handler) cancelled the
     ioLoops FIRST, stranding the in-flight requests so the drain never finished → STOP_DEV
     blocked. Reordered the LIVE path to **STOP_DEV → cancel ioLoops → join → DEL_DEV**.
     `teardownPartial` (creation-failure, device not yet LIVE) correctly keeps cancel-first (#7).
   Verified on arm64: START reliable; 50+ teardown-under-load cycles (Q=1/4/8, O_DIRECT fio,
   SIGINT mid-load) all exit in ~0.13s with 0 leaks / 0 D-state; 128MB O_DIRECT read-after-write
   byte-exact; idle stop 0.11s; unit tests green.

9. **[FIXED — 2026-07-26] Any discard over 1MB panicked the whole daemon.**
   `handleIORequest` acquired a data buffer sized from the request length for
   *every* op. A DISCARD's length is a range to deallocate, not bytes to move, so
   `blkdiscard` over a few MB — or an `fstrim` on a mounted filesystem — called
   `GetBuffer(32MB)`, which resliced a 1MB pooled buffer past its capacity:
   `panic: slice bounds out of range [:33554432] with capacity 1048576`. The
   process died, every device it served went to EIO, and the kernel logged
   `I/O error, dev ublkb0, sector 0 op 0x3:(DISCARD)`. Reachable by anything with
   write access to the device, and it existed from the moment discard was first
   advertised (commit ff53e7a) — the earlier discard test only used a small range.
   Fixed two ways: only READ/WRITE acquire a buffer, and `GetBuffer` now allocates
   exactly rather than reslicing a pooled buffer past its capacity. Found by the
   `ublk-loop` e2e, which discards 64MB.

10. **[FIXED — 2026-07-26] `LogicalBlockSize` other than 512 silently corrupted data.**
    The ublk UAPI counts sectors in 512-byte units everywhere
    (`ublk_param_basic.dev_sectors` and `max_sectors`,
    `ublksrv_io_desc.start_sector`/`nr_sectors`), but the control plane derived
    `DevSectors` from `LogicalBlockSize` and the data plane multiplied
    `StartSector` by it, while `submitCommitAndFetch` hardcoded `NrSectors << 9`
    — the internal disagreement that gave it away. A 4096-byte build of
    `ublk-loop` reported a 256MB device as 32MB and did I/O at 8x the intended
    offset (`FULL-READBACK MISMATCH at dev-offset 25096192: got 0x00 want 0x95`);
    a zero value divided by zero. Fixed by counting sectors in `uapi.SectorSize`
    everywhere and deleting the runner's `blockSize` field entirely so the
    conflation cannot come back. `validateParams` now enforces the kernel's real
    rule (power of two, 512..PAGE_SIZE) plus MaxIOSize and backend-size
    alignment. Verified on arm64: a 4Kn device reports 268435456 bytes and the
    shadow oracle is CLEAN at Q=4/depth=64/O_DIRECT, and the 512-byte sweep is
    still 24/24.

11. **[FIXED — 2026-07-26] Four of five optional backend interfaces were never called.**
    `WriteZeroesBackend`, `SyncBackend`, `StatBackend` and `ResizeBackend` were
    public, documented, and asserted by tests, but nothing in the I/O loop
    consulted them, so a user could implement one and never have it fire.
    Resolved by wiring the one with a kernel operation behind it and dropping the
    three without: `UBLK_IO_OP_WRITE_ZEROES` now dispatches to
    `WriteZeroesBackend`, and the write-zeroes limit is advertised only when the
    backend implements it (discard and write-zeroes share one param block but are
    independent capabilities). `SyncBackend`/`StatBackend`/`ResizeBackend` are
    removed: ublk has no range-sync op, statistics are the caller's own business
    since they hold the backend, and resize is a feature rather than a wiring
    task. Verified: `write_zeroes_max_bytes` is advertised, `blkdiscard -z`
    succeeds, the range reads back all zeros, and the backing file returns to
    sparse (the punch-hole path).

12. **[FIXED — 2026-07-26] The examples could not be compiled by a library user.**
    `examples/ublk-mem` imported `internal/ctrl` (to reap leaked devices) and
    `internal/logging` (for `-v`), neither reachable from outside the module — so
    the flagship example was not a valid demonstration of the public API.
    Reaping is now public (`ublk.ListDevices`, `ublk.DeleteDevice`), and
    `Options.Debug` plus an adapter that routes the internal logger's output
    through `Options.Logger` replaces the internal-logging import — so a caller's
    own logger now receives the control-plane and queue diagnostics instead of
    only the handful of messages `backend.go` emits directly. Neither example
    imports `internal/` any more.

13. **[DECIDED — 2026-07-26] Durability default flipped to advertising a volatile write cache.**
    `DefaultParams` now sets `VolatileCache: true`. The library cannot know
    whether a backend's completed write is durable, and the two mistakes are not
    symmetric: claiming a cache that does not exist costs a no-op flush
    round-trip, while hiding one that does exist loses data on power failure with
    no error anywhere. A backend that makes every write durable before returning
    should set it false, which also stops the kernel sending flushes it does not
    need — `ublk-mem` does exactly that (RAM has no cache below it) and
    `ublk-loop` sets it from its `-sync` flag. Per-I/O FUA is now implemented: `backend.go:1334` gates advertisement,
    `internal/queue/adapter.go:43` calls `FUABackend.WriteAtFUA`, and zero-copy
    writes use `RWF_DSYNC` (`internal/queue/engine.go:1062`). Handler users must
    honor `FlagFUA`; plain backends retain block-layer flush emulation.
    `features/fua` tests dispatch; crash durability is still a separate gate.

14. **[FIXED — 2026-07-26] `vm-simple-e2e.sh` killed unrelated processes, including its own caller.**
    `cleanup_force()` ran `ps aux | grep -E "(ublk-mem|timeout)"` and SIGKILLed
    every match, so any process whose command line merely mentioned those strings
    died with it — including the ssh session or shell running the script, which
    then looked exactly like the script hanging after "device stopped
    successfully". (An earlier note in this file claiming that hang was a real
    pre-existing defect was wrong; the script exits 0.) Now matched by exact
    process name via `pgrep -x`, D-state entries skipped, and the `killall -9
    ublk-mem timeout dd` on the I/O-hang path narrowed the same way.
    `vm-fuzz.sh`'s unanchored `pkill` patterns were tightened too.

15. **[OPEN INVESTIGATION — ordering and signal mitigations shipped] Shutdown under load
    can lose writeback or wedge a reboot.** Historical unsupervised runs wedged
    3/14 reboots. The source does not settle whether a remaining library/kernel
    teardown defect contributes; “Blocked by coredump” is not proof of SIGBUS or
    an actual dump. Mount/service ordering is required; the root cause of the
    unsupervised wedge still needs captured stacks and a current reproducer.
    `examples/systemd/` ships the service/mount (`e1f67d7` adds `Before=user.slice`),
    and both examples now handle SIGHUP (`examples/ublk-loop/main.go:131`,
    `examples/ublk-mem/main.go:202`). The results below predate or qualify those
    mitigations; do not treat them as a current v0.2.0 failure rate.

    Reported 2026-08-22 by `make vm-shutdown-storm`. Running `ublk-loop` as a bare background process with an
    ext4 filesystem mounted on its device and fio writing into it, then issuing a normal
    `systemctl reboot`:

    | Deployment | Storm reboots | Wedged | I/O errors on ublkb0 per shutdown |
    |---|---|---|---|
    | Bare background process (ssh session scope) | 14 | **3** | **10, every cycle** |
    | systemd unit, mount ordered `After=`/`Requires=` it | 9 | **0** | **0, every cycle** |

    **Historical ordering diagnosis.** A bare process lives in the login session's
    scope, which systemd tears down at the *start* of shutdown — so the daemon dies while the filesystem above it
    still owes writeback. The unmount then fails, ext4 aborts its journal, and roughly one
    time in five the machine never finishes rebooting at all: `systemd-shutdown` reaches
    `reboot.target`, then its final "Syncing filesystems and block devices" times out and
    it waits forever on tasks that cannot be reaped. Every occurrence had the identical
    signature — `INFO: task ublk-loop:<tid> blocked for more than 122 seconds. Blocked by
    coredump.` plus `iou-wrk-<tid>`, the fio workers, and two `(sd-sync)` helpers. The host
    needs a forced power cycle. That is strictly worse than the old resilience goal of "a wedged
    daemon must not require a host reboot": here it *prevents* one.

    **Observed mitigation.** Ordering corrected writeback in these runs. Making
    the daemon a systemd service and giving the mount unit `Requires=`/`After=` that service inverts the stop order, so the
    filesystem unmounts *through* a still-live daemon. The I/O-error result is
    deterministic (10 vs 0 on every single cycle) and settles the mechanism; the wedge
    result is suggestive rather than conclusive on its own (9 clean cycles against a 21%
    per-cycle rate is p≈0.12), but it is the same mechanism and it points the same way.

    **Original hypothesis, not established:** a fatal signal (possibly SIGBUS on a
    torn-down mmap) caused a core dump piped to apport during shutdown, blocking
    the thread group. SIGTERM/SIGKILL do not dump core; the hung-task label alone
    does not establish that any dumping signal occurred. The later diagnosis
    below offers a different explanation. Three attempts to capture the daemon's own output
    across the wedge all failed and the reasons are recorded in
    `scripts/vm-shutdown-storm.sh`: a plain file gets rolled back by ext4 replay, and a
    `tail -F` mirror is killed at the start of shutdown. The daemon now writes straight to
    the console; the next wedge should be captured.

    **Shipping status:** the service and mount examples are present, with
    `After=`/`Requires=` the daemon and `Before=user.slice`; dependent services
    must use `RequiresMountsFor=`. This closes the missing-unit task, not the
    investigation of the unsupervised wedge or failed-unmount handling.

    **Re-diagnosis to test (2026-10-03):** "Blocked by coredump" may not mean a core dump.
    `synchronize_group_exit()` sets `PF_POSTCOREDUMP` on every exiting thread, so hung_task
    prints it for any thread stuck in `do_exit`. A plausible chain: logind's session scope sends
    SIGTERM then SIGHUP; at that revision neither example handled SIGHUP, so Go's
    default could kill the process while STOP_DEV is running in an `iou-wrk`
    worker (`del_gendisk` -> `sync_filesystem`), and the exiting threads wait on that worker, which waits on I/O only the dead daemon could complete.
    SIGHUP handling is now in both examples. Test the hypothesis by re-running
    the unsupervised storm on the candidate and capturing `/proc/<tid>/stack`
    of any stuck threads; do not reimplement the signal handler.

    **Ordering gap found and fixed (2026-10-04):** the correctly ordered units were still not
    enough. Rebooting a 7.0.0-38 VM under buffered fio with the shipped units lost writeback in 3
    of 5 reboots: fio ran in an ssh login session, the unmount failed with "target is busy"
    because that session had not been stopped yet, systemd then stopped the daemon anyway
    (the mount's stop job had finished, by failing), and STOP_DEV removed the disk under a
    mounted ext4 with ~290 MB dirty (I/O errors, JBD2 aborted). Adding `Before=user.slice` to
    the mount unit stops every session and user service before the unmount: 0 of 5 after, the
    journal showing user.slice removed, then unmount, then the daemon stopped. Services that use
    the filesystem must likewise be ordered with `RequiresMountsFor=`.

16. **[FIXED — 2026-10-03] A discard or write-zeroes of 2GiB or more failed with EIO.**
    `submitCommitAndFetch` reported `int32(NrSectors) << 9` for every op. From 4194304 sectors
    that wraps negative, and the kernel fails any negative result even though the backend had
    already done the work. `DefaultMaxDiscardSectors` is `0xffffffff`, so the block layer sends
    ~4GiB requests: `blkdiscard`, `blkdiscard -z`, and `mkfs.ext4`'s whole-device discard on any
    device over 2GiB all hit it. Range ops now complete with 0, which is all the kernel reads for
    them. Reproduced on arm64 7.0.0-30 (2G/3G/4G discards failed), fixed and re-verified there and
    on x86_64 7.0.0-38; `TestRunnerRangeOperationResultIsNonNegative` fails against the old code.

17. **[FIXED — 2026-10-04] Every io_uring the library creates leaked until process exit.**
    `minimalRing.Close` closed the fd but never unmapped the SQ, CQ and SQE regions, and the
    mappings kept the ring alive. Measured on 7.0.0-38: 50 `ListDevices` calls left 150 io_uring
    mappings; each device create/close cycle left 4 rings. `internal/uring` is now a general
    io_uring core (`IoUring`) under the ublk `Ring`; its `Close` is idempotent, unregisters the
    buffer rings, buffers and files it registered, unmaps every mapping and closes the fd.
    `TestRingCloseReleasesMappingsAndFds` creates and closes 5000 rings: `/proc/self/maps` 26 -> 26
    lines and fds unchanged now, 29 -> 15032 lines on the old code (kernel 6.6, WSL rig). Since
    unmapping makes a racing caller fault where it used to touch orphaned memory, `Ring.Close`
    defers teardown until any goroutine still inside a `Ring` method returns (a parked
    `WaitForCompletion` does within 100ms), and later calls fail with `ErrRingClosed`.

18. **[FIXED — 2026-10-04] `Device.Close` ignored a failed STOP_DEV and tore the queues down
    anyway.** `Stop` (and `Close`, through it) now returns the STOP_DEV error with nothing torn
    down, so the device keeps serving; the wait is bounded by `Options.StopTimeout` (default one
    minute) instead of a fixed 10s. `SafeStop` uses TRY_STOP_DEV and returns `ErrDeviceBusy`
    while the block device is open; `features/safe-stop` checks the device still serves after a
    refused stop.

19. **[FIXED — 2026-10-04] `Runner.Close` freed the ring and buffers after a join that timed
    out.** The Runner is gone. A `queue.Queue` frees its descriptor and data mappings only once
    every engine has exited and no handler still holds a request; otherwise `Close` returns
    `ErrStillInUse` and leaks them, and the device keeps `/dev/ublkcN` open and refuses DEL_DEV
    rather than free memory a handler or the kernel may still touch.

20. **[FIXED — 2026-10-04] A queue's ioLoop could die silently while the device stayed LIVE.**
    An engine that hits an unexpected completion now stops dispatching, still commits what its
    handlers hold, closes its ring (so the kernel aborts or requeues its outstanding commands)
    and reports the error; a per-queue supervisor turns that into `Device.Err()`, closes
    `Device.Done()`, and `State()` reports `failed`. `TestEngineUnexpectedCompletionIsFatal`
    covers the engine side.

21. **[FIXED — 2026-10-04, both sides] Buffers handed to asynchronous control commands were not
    pinned for the kernel's use.** SET_PARAMS' buffer was only reachable via a uintptr (fixed in
    the 2026-10-02 hardening merge); ADD_DEV returned before its KeepAlive on the 10s timeout, so a
    late kernel write landed in freed memory; GET_DEV_INFO's and GET_PARAMS' buffers were
    stack-allocated (`go build -gcflags=-m`: "does not escape") and moved with the goroutine stack.
    Control side: every control buffer now lives in an mmap'd scratch page owned by the command's
    ring slot; it never moves and is never unmapped while a command may be in flight — a transport
    error or timeout retires the slot and leaks the page. `TestControlBufferSurvivesStackMove`
    forces a stack copy between building the command and the "kernel" write; it fails against the
    old code (reply reads back as zeros) and passes now. io_uring side: `Ring.SubmitCtrlCmd` copies
    the payload into ring-owned off-heap memory, and a late CQE is told apart by an internal
    user_data tag. A command whose context ends returns `*ctrl.InFlightError`;
    `Ring.SubmitCtrlCmdContext` cancels it with IORING_OP_ASYNC_CANCEL, so an abandoned command that
    sleeps interruptibly in the kernel (END_USER_RECOVERY or START_DEV waiting for FETCHes, DEL_DEV
    waiting for the last reference, QUIESCE_DEV) gets EINTR (its CQE is reaped within a 1s grace and `ErrCtrlCanceled` returned) and releases its device instead of
    lingering until process exit (measured on 7.0.0-38). ctrl's rings therefore run with
    `CtrlTimeout: -1` and rely on the caller's context for bounds.

22. **[FIXED — 2026-10-04] Cancelling the serving context wedged the device, and on Ubuntu
    7.0.0-38 the whole ublk control plane.** The old `CreateAndServe` context cancelled the queue
    loops directly. If that happened while the kernel's asynchronous partition scan had a read
    outstanding (right after START_DEV), the read never completed: `ublk_partition_scan_work`
    sat in `folio_wait_bit_common` holding `disk->open_mutex`, STOP_DEV's `del_gendisk` blocked
    on that mutex in an io-wq worker, and DEL_DEV then blocked on the mutex STOP_DEV held — every
    later ADD_DEV timed out. Reproduced deterministically by the kernel matrix on 7.0.0-38
    (hung-task and sysrq-w stacks matched); mainline 7.0.14 and Fedora 7.2.8 passed by timing.
    Cancelling the context now performs a graceful `Stop` (STOP_DEV through the running queues).
    `lifecycle/ctx-cancel-under-load` passes 15/15 on 7.0.0-38.

23. **[FIXED — 2026-10-04, by refusing it] Start after Stop crashed or wedged kernels.** The old
    docs promised `Start` could resume a stopped device. The kernel matrix showed START_DEV on a
    stopped device fails with EBUSY (Fedora 6.19/7.2.8, 7.0.14), wedges the control plane
    (6.10-6.12), or oopses — Arch 7.2.8-arch1-2: NULL dereference in `ublk_queue_rq+0x4d` from
    `ublk_partition_scan_work`. Missing per-I/O state is a hypothesis; the trace proves
    a NULL write, not its exact lifetime cause. `Start` on a stopped device now
    returns `ErrStopped` without touching the kernel; close it and create a new
    device. This fixes the library's unsafe promise; kernel restart behavior on
    other revisions remains unproven.

24. **[FIXED — 2026-10-04] On kernels before 6.11, a zeroout of 4 GiB or more succeeded but
    zeroed only the length mod 4 GiB.** Found by the kernel matrix: `blkdiscard -z -o 1M -l 5G`
    exited 0, the disk stats showed one 1 GiB write-zeroes, and data planted at 1 GiB, 3 GiB and
    5 GiB was still there — on mainline 6.4, 6.6, 6.9, 6.10, openSUSE Leap 15.6 and Ubuntu's 6.8 (the
    default noble kernel). go-ublk advertised `max_write_zeroes_sectors = 0xffffffff`; pre-6.11
    `__blkdev_issue_write_zeroes` builds the whole request in one bio when it fits the limit and
    stores `nr_sects << 9` in the 32-bit `bi_size`, so 5 GiB became 1 GiB and the call returned
    success. The advertised discard and write-zeroes limits are now capped at `UINT32_MAX >> 9`
    rounded down to the logical block; the suite's range tests also check data at the end of the
    range. Correct on 6.11.11, 7.0.14 and 7.0.0-38 before and after the fix.

25. **[FIXED — 2026-10-04] Two recovery defects, found by new suite tests.** (a) `Recover` took only
    the geometry and user-copy flag from the kernel, so a recovered integrity device served
    requests without their metadata (`guard tag error ... rcvd 0000` on the first read); it now
    takes zero copy, batch I/O, zoned parameters and the integrity format from the kernel too.
    (b) `Detach` on a batch device sent QUIESCE_DEV, which on kernels without 8a14be55bdc6
    (7.3-rc3, stable 7.2.7) leaves force_abort set: a writer got EIO across the handoff on
    7.0.0-38. Batch devices now hand off without QUIESCE; the kernel reissues what was
    outstanding. `recovery/{batch-kill-and-recover,batch-detach-handoff,integrity-kill-and-recover}`
    failed before and pass 3/3 after on 7.0.0-38.

26. **[OPEN — minor, v0.2.1] Two leftovers from the v0.2.0 kernel matrix.** (a) On RHEL 10 with
    io_uring disabled, every error carries the `kernel.io_uring_disabled` hint except one path:
    `lifecycle/fixed-id` reports a bare "open control device: … operation not permitted". Wrap that
    path with `explainControlError` too. (b) The io_uring unit tests fail on 5.15 kernels (SQE128 and
    CQE32 arrived in 5.19) and on RHEL 9 (io_uring disabled); go-ublk cannot run there, but the tests
    should skip rather than fail.

---

---

## Validation commands

On this Mac, offline cross-compile gates:

```sh
taskpolicy -b nice -n 15 env GOOS=linux GOARCH=amd64 GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go build ./...
taskpolicy -b nice -n 15 env GOOS=linux GOARCH=amd64 GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go vet ./...
```

Linux runtime tests are coordinator-owned. The existing entry points include
`make test-unit`, `make test-race`, `make test-uapi-fuzz`, `make suite`,
`make vm-verify`, `make vm-loop-e2e`, `make vm-crash`, `make vm-powerfail`,
`make vm-shutdown-storm` and `make vm-soak`; setup is in
[VM_TESTING.md](docs/VM_TESTING.md) and [test/matrix/README.md](test/matrix/README.md).
Building a harness is distinct from passing it on a named candidate/kernel.

## Historical observations

Earlier TODO revisions recorded arm64 copy integrity sweeps, x86_64 AWS
teardown-under-load cycles, file-backend SIGKILL/guest-reset consistency and RAM
O_DIRECT benchmarks. Those were specific kernel/workload observations; they do
not establish current advanced-mode coverage, host power-cut durability or a
competitive performance ranking. Current reproducible coverage comes from the
saved per-test matrix mapped in FEATURES.md. The old buffered benchmark table,
buffer-pool speedups and shared-barrier/Sfence advice are superseded by the
v0.2.0 mmap/atomic engine and are removed from the active backlog.
