# Feature and support inventory

Source baseline: **v0.2.0, `e4e39b0`**, checked 2026-10-09. Linux only,
Go 1.25, no cgo. The copy baseline runs from Linux 6.4 with `ublk_drv`
and enabled io_uring; availability is negotiated, including distribution
backports. Root/CAP_SYS_ADMIN is required unless using unprivileged devices.

Support tiers describe the scope in each row:

- **Guaranteed (G):** baseline transport/API behavior backed by unit/model tests
  and recorded kernel passes, with valid parameters and a backend that completes
  requests and honors its interfaces.
- **Tested-experimental (E):** implemented and exercised, with limited mode,
  failure, lifetime or deployment coverage. Use the listed kernel results.
- **Untested (U):** no behavioral test for the stated path. This includes absent,
  rejected and symbol-only capabilities, explicitly identified below; layout or
  conversion tests alone do not establish working support.

Counts are inventory rows, grouping related behavior: **12 G, 26 E, 11 U**.
Tests listed here exist in source; this inventory did not rerun Linux tests.
Combinations inherit their least-supported path and need their own coverage.

## Recorded kernel coverage

The [saved matrix](../test/matrix/results/matrix.json) has **67 rows: 47 pass,
4 fail, 14 no-ublk, 2 fetch-failed**. Its [per-run revisions](../test/matrix/results/runs-detail.json)
are 52 rows at `4d9712f`, 13 at `0aa26d9`, and two without a revision.
Completed rows are x86_64/QEMU TCG. These are earlier-revision observations,
with raw logs kept outside the repository ([results notes](../test/matrix/results/README.md)).
They establish neither a fresh v0.2.0 sweep nor advanced-mode arm64 coverage.

Sets below identify exact matrix row IDs; the JSON supplies full kernel builds.
The tables cite individual passing tests, never counting skips as passes.

| Set | Passing kernel rows |
|---|---|
| **B (47)** | Mainline: `mainline-6.4`, `mainline-6.5.11`, `mainline-6.6.158`, `mainline-6.7.10`, `mainline-6.8.12`, `mainline-6.9.12`, `mainline-6.10.14`, `mainline-6.11.11`, `mainline-6.12.112`, `mainline-6.13.12`, `mainline-6.14.11`, `mainline-6.15.11`, `mainline-6.16.12`, `mainline-6.17.12`, `mainline-6.18.55`, `mainline-6.19.14`, `mainline-7.0.14`, `mainline-7.1.13`, `mainline-7.2.6`, `mainline-7.3-rc3`. Distribution rows: `arch-lts`, `arch`, `debian-12-backports`, `debian-13`, `fedora-42`, `fedora-43`, `fedora-44`, `oracle-9-uek8`, `almalinux-10+io_uring`, `centos-stream-10+io_uring`, `rocky-10+io_uring`, `opensuse-leap-15.6`, `opensuse-tumbleweed`, `full-ubuntu-24.04-extra`, `ubuntu-22.04-hwe`, `ubuntu-24.04-ga`, `ubuntu-24.04-hwe-6.11`, `ubuntu-24.04-hwe-6.14`, `ubuntu-25.04`, `ubuntu-24.04-hwe-6.17`, `ubuntu-25.10`, `ubuntu-24.04-aws`, `ubuntu-24.04-azure`, `ubuntu-24.04-gcp`, `ubuntu-24.04-hwe`, `ubuntu-24.04-hwe-7.0.0-38`, `ubuntu-26.04`. |
| **R (46)** | B except `mainline-6.4`. |
| **Z (45)** | R except `mainline-6.5.11`. |
| **T (25)** | `mainline-6.16.12`, `mainline-6.17.12`, `mainline-6.18.55`, `mainline-6.19.14`, `mainline-7.0.14`, `mainline-7.1.13`, `mainline-7.2.6`, `mainline-7.3-rc3`, `arch-lts`, `arch`, `fedora-42`, `fedora-43`, `fedora-44`, `almalinux-10+io_uring`, `centos-stream-10+io_uring`, `rocky-10+io_uring`, `opensuse-tumbleweed`, `ubuntu-24.04-hwe-6.17`, `ubuntu-25.10`, `ubuntu-24.04-aws`, `ubuntu-24.04-azure`, `ubuntu-24.04-gcp`, `ubuntu-24.04-hwe`, `ubuntu-24.04-hwe-7.0.0-38`, `ubuntu-26.04`. |
| **C (26)** | T plus `mainline-6.15.11`. |
| **F (30)** | T plus `mainline-6.13.12`, `mainline-6.14.11`, `mainline-6.15.11`, `ubuntu-24.04-hwe-6.14`, `ubuntu-25.04`. |
| **A (37)** | B except `mainline-6.4`, `mainline-6.5.11`, `mainline-6.6.158`, `mainline-6.7.10`, `mainline-6.8.12`, `mainline-6.9.12`, `opensuse-leap-15.6`, `full-ubuntu-24.04-extra`, `ubuntu-22.04-hwe`, `ubuntu-24.04-ga`. |
| **N (14)** | `mainline-7.0.14`, `mainline-7.1.13`, `mainline-7.2.6`, `mainline-7.3-rc3`, `arch`, `fedora-43`, `fedora-44`, `opensuse-tumbleweed`, `ubuntu-24.04-aws`, `ubuntu-24.04-azure`, `ubuntu-24.04-gcp`, `ubuntu-24.04-hwe`, `ubuntu-24.04-hwe-7.0.0-38`, `ubuntu-26.04`. |
| **S (7)** | `mainline-7.1.13`, `mainline-7.2.6`, `mainline-7.3-rc3`, `arch`, `fedora-43`, `fedora-44`, `opensuse-tumbleweed`. |
| **D (1)** | `mainline-7.3-rc3`. |

RHEL 10 default rows fail with disabled io_uring; their `+io_uring` rows pass.
Ubuntu `6.17.0-40` is a failing ADD_DEV kernel negative control. Eleven rows
have a zero-copy UBSAN report classified as a known kernel bug and skipped by
the reporter; retain that qualification when using C/T/N results. Older
[restart artifacts](../test/matrix/results/evidence-restart-after-stop/) record
a kernel oops; today's `lifecycle/restart-after-stop` proves library refusal
(`ErrStopped`), not a working kernel restart.

## Baseline

Implementation and test links include source line numbers. Queue `TestEngine*`
tests use the [fake kernel](../internal/queue/fakekernel_test.go#L23).

| Feature/mode | Tier | Implementation and scope | Unit/model tests; recorded kernel passes |
|---|---|---|---|
| Copy READ/WRITE, synchronous Backend, error propagation | G | [adapter.go:24](../internal/queue/adapter.go#L24), [engine.go:689](../internal/queue/engine.go#L689); mmap per-tag buffers; short writes fail, partial reads limited to copy Handler requests. | [TestEngineRoundTripInline/Goroutine:132](../internal/queue/engine_test.go#L132), [TestEngineShortWriteFails:214](../internal/queue/engine_test.go#L214), [TestEngineErrnoMapping:196](../internal/queue/engine_test.go#L196); B: `io/integrity-*`, `io/boundaries`, `io/error-propagation`. |
| Create/Start/Stop/Close, context stop | G | [backend.go:592](../backend.go#L592), [Start:697](../backend.go#L697), [Stop:966](../backend.go#L966), [Close:1024](../backend.go#L1024); STOP drains through live queues. Stop is terminal. Failed STOP preserves queues; a held handler can prevent cleanup. | [TestDeviceLifecycleAPIPreconditions:440](../backend_test.go#L440), [TestEngineStopDrainsThenExits:296](../internal/queue/engine_test.go#L296); B: `lifecycle/create-start-stop-close`, `ctx-cancel-{idle,under-load}`, `close-under-load`, `restart-after-stop`, `churn-leaks`, `server-killed`, `chaos`. |
| Multi-queue and kernel geometry reconciliation | G | [backend.go:433](../backend.go#L433), [queue.go:128](../internal/queue/queue.go#L128); fixed descriptor stride, kernel-accepted queue count/depth/buffer size. | [TestApplyNegotiatedDeviceInfo:211](../backend_test.go#L211); B: `io/integrity/q2-d32-bs4096`, `q4-d128-bs512`, `verify-sweep`. |
| Goroutine dispatch and Inline | G | [engine.go:755](../internal/queue/engine.go#L755); default concurrency per tag; Inline requires a nonblocking handler for queue progress. | [TestEngineRoundTripInline/Goroutine:132](../internal/queue/engine_test.go#L132); B: `features/integrity-inline` and baseline integrity. |
| Queue OS-thread ownership | G | [engine.go:192](../internal/queue/engine.go#L192); LockOSThread for each engine, thread exits with engine. CPU placement is a separate row below. | Round-trip engine models exercise the owner loop; B: baseline I/O/lifecycle exercises the real kernel's same-task requirement. |
| FLUSH and volatile/write-through cache advertisement | G | [adapter.go:55](../internal/queue/adapter.go#L55), [device.go:109](../internal/ctrl/device.go#L109), [DefaultParams:454](../backend.go#L454); volatile cache defaults true; Backend.Flush supplies stable-storage semantics. | [TestBasicAttrs:40](../internal/ctrl/params_test.go#L40); B: `io/flush`, `io/no-volatile-cache`. Crash durability has a separate gap. |
| DISCARD/TRIM and WRITE_ZEROES | G | [adapter.go:60](../internal/queue/adapter.go#L60), [device.go:132](../internal/ctrl/device.go#L132), [request.go:134](../internal/queue/request.go#L134); advertise only implemented optional interfaces; range result 0, limits capped below 4 GiB. | [TestRangeLimitsFitA32BitRequest:223](../internal/ctrl/params_test.go#L223), [TestEngineRangeOpsCommitZero:183](../internal/queue/engine_test.go#L183); B: `io/{discard,discard-large,write-zeroes,write-zeroes-large,no-discard-advertised}`. |
| Block sizes, capacity, MaxIOSize, physical/I/O hints | G | [validateParams:297](../backend.go#L297), [device.go:45](../internal/ctrl/device.go#L45); logical 512..PAGE_SIZE, sector units always 512 bytes; maximum signed completion size. | [TestValidateParamsBlockSize:136](../backend_test.go#L136), [TestConvertAllFieldsForwarded:86](../backend_convert_test.go#L86); B: `params/geometry`, 4Kn integrity, `integration/large-io`. Matrix page size is 4 KiB. |
| ReadOnly and Rotational attributes | G | [device.go:109](../internal/ctrl/device.go#L109); SET_PARAMS attributes. | [TestBasicAttrs:40](../internal/ctrl/params_test.go#L40); B: `io/readonly`, `params/geometry`, `loop-e2e`. |
| Probe, negotiated flags/params, ioctl encoding | G | [Probe:62](../recover.go#L62), [features.go:204](../internal/ctrl/features.go#L204), [commands.go:85](../internal/ctrl/commands.go#L85), [SetParams:222](../internal/ctrl/commands.go#L222); reject missing/cleared requested bits/types. Legacy COMP_IN_TASK is recognized, no longer requested ([device.go:184](../internal/ctrl/device.go#L184)); EnableIoctlEncode is inert, encoding always used. | [TestNegotiate:87](../internal/ctrl/features_test.go#L87), [TestAddDevReportsFlagsTheKernelCleared:157](../internal/ctrl/features_test.go#L157), [TestSetParamsReportsTypesOlderKernelsDrop:275](../internal/ctrl/commands_test.go#L275); B: `features/probe`. 6.4 reports unknown features. |
| Device IDs, tags, discovery, inspection, zombie deletion | G | [manage.go:23](../manage.go#L23), [DeleteDevice:89](../manage.go#L89), [GetDeviceInfo:144](../recover.go#L144), [FindDevices:162](../recover.go#L162); sysfs enumeration or bounded 0..63 fallback. Delete is for abandoned devices; Close for an owned device. | [manage_test.go](../manage_test.go), [command lifecycle:38](../internal/ctrl/commands_test.go#L38); B: `features/tag-find`, `lifecycle/{fixed-id,list-high-id,server-killed}`. |
| io_uring submission batching and atomic ordering | G | [engine loop:541](../internal/queue/engine.go#L541), [ring.go:359](../internal/uring/ring.go#L359); multiple SQEs per submit, atomic SQ/CQ indices; distinct from ublk BatchIO. | [ring_index_test.go:73](../internal/uring/ring_index_test.go#L73), [TestCQOverflowIsFlushedNotDropped:479](../internal/uring/core_test.go#L479), [TestHotPathDoesNotAllocate:14](../internal/uring/bench_test.go#L14); B: `unit/internal_uring` and baseline I/O. |

## Advanced modes and backends

| Feature/mode | Tier | Implementation and scope | Unit/model tests; recorded kernel passes |
|---|---|---|---|
| Raw asynchronous Handler | E | [request.go:22](../request.go#L22), [engine.go:689](../internal/queue/engine.go#L689); exactly one completion, fields/buffers valid until completion; queue faults surface through [Done/Err:900](../backend.go#L900). | [TestEngineAsyncOutOfOrderStress:142](../internal/queue/engine_test.go#L142), [NoLostWakeup:387](../internal/queue/engine_test.go#L387), [UnexpectedCompletionIsFatal:351](../internal/queue/engine_test.go#L351); B: `features/handler-async`. Device-level failure/timeout injection remains a gap. |
| User copy | E | [engine.go:781](../internal/queue/engine.go#L781), [copyIn/Out:802](../internal/queue/engine.go#L802); char-device pread/pwrite, not zero copy. | [TestEngineRoundTripUserCopyInline/Goroutine:134](../internal/queue/engine_test.go#L134); R: `features/integrity-user-copy` (byte-integrity test, not PI metadata). |
| NEED_GET_DATA | E | [engine.go:641](../internal/queue/engine.go#L641); extra write-buffer round trip; incompatible with user copy, zero copy and batch. | [TestEngineNeedGetData:229](../internal/queue/engine_test.go#L229); R: `features/integrity-need-get-data`. |
| Manual fixed-buffer zero copy | E | [engine setup:223](../internal/queue/engine.go#L223), [register/file I/O:1013](../internal/queue/engine.go#L1013), [unregister before commit:929](../internal/queue/engine.go#L929); ZeroCopyBackend file/base mapping, no Go payload or backend callbacks. | [TestEngineZeroCopyManual:486](../internal/queue/engine_test.go#L486), [ShortReadFails:490](../internal/queue/engine_test.go#L490); `mainline-6.15.11`: `features/{zero-copy,zero-copy-close-under-load}`. |
| AUTO_BUF_REG zero copy and manual fallback | E | [automatic negotiation:620](../backend.go#L620), [engine.go:983](../internal/queue/engine.go#L983), [NEED_REG_BUF:1019](../internal/queue/engine.go#L1019); enabled opportunistically when advertised. | [TestEngineZeroCopyAuto/AutoFallback:484](../internal/queue/engine_test.go#L484); T: `features/{zero-copy,zero-copy-close-under-load}`. Accepted feature identifies auto configuration; VM tests do not force/count fallback. |
| Ublk BatchIO with copy/user copy | E | [engine.go:264](../internal/queue/engine.go#L264), [partial commits:463](../internal/queue/engine.go#L463); PREP, multishot FETCH, tag buffers and COMMIT arrays. One engine per queue; rejects NEED_GET_DATA and ThreadsPerQueue > 1. | [TestEngineBatchCopy/CopyInline/UserCopy/PartialCommits:556](../internal/queue/engine_test.go#L556); N: `features/integrity-batch-io`, `features/integrity-batch-io-user-copy`, `features/batch-io-close-under-load`. |
| BatchIO with zero copy | E | [batchFlags:264](../internal/queue/engine.go#L264), [commit:908](../internal/queue/engine.go#L908); requires AUTO_BUF_REG; supports manual fallback. | Separate batch and ZC model tests above; no combined batch-ZC model test. N: `features/batch-io-zero-copy`. |
| Unprivileged devices | E | [Create udev wait:633](../backend.go#L633), [dev-path probe:39](../internal/ctrl/commands.go#L39), [rules/helper](../examples/ublk-chown/); kernel forbids user copy, zero copy and recovery. Root may have the unprivileged bit cleared. | [TestDevPathLayout:212](../internal/ctrl/commands_test.go#L212), [TestAddDevUnprivilegedRequestAsRoot:209](../internal/ctrl/features_test.go#L209); R: `features/unprivileged` ([test:1408](../test/suite/tests_v1.go#L1408)); root emulates udev, nobody serves, root issues block I/O. |
| RecoveryReissue and RecoveryQueue | E | [modes:254](../backend.go#L254), [Recover:282](../recover.go#L282); respectively reissue or fail outstanding requests; both hold new I/O while server absent. | [fake control lifecycle:38](../internal/ctrl/commands_test.go#L38); R: `recovery/kill-and-recover`, `recovery/queue-mode` ([tests:534](../test/suite/tests_v1.go#L534)). No public recovery failure-state model. |
| RecoveryFailIO | E | [backend.go:270](../backend.go#L270), [Recover:282](../recover.go#L282); absent-server outstanding/new I/O fails until takeover. | Control flag/model tests; F: `recovery/fail-io-mode` ([test:701](../test/suite/tests_v1.go#L701)), idle-server crash. |
| Detach, QUIESCE and handoff | E | [recover.go:223](../recover.go#L223); best-effort QUIESCE on T, then abandon/drain; batch deliberately bypasses QUIESCE. Older recovery kernels hand off without it. | [TestEngineAbandonLeavesNewRequestsToKernel:327](../internal/queue/engine_test.go#L327), [TestQuiesceTimeoutEncoding:318](../internal/ctrl/commands_test.go#L318); R: `recovery/detach-handoff`; T configures QUIESCE, without asserting its success. |
| Recovery with batch or integrity | E | [restore flags/format:312](../recover.go#L312), [batch handoff exception:239](../recover.go#L239). | Shared control/abandon models; N: `recovery/{batch-kill-and-recover,batch-detach-handoff,integrity-kill-and-recover}` ([registration:95](../test/suite/tests_v1.go#L95)). |
| Shared-memory zero copy, REG_BUF/UNREG_BUF | E | [recover.go:401](../recover.go#L401), [Unregister:429](../recover.go#L429), [shmem.go:33](../internal/queue/shmem.go#L33); page-aligned shared mappings must outlive unregister; eligible request Data aliases a region, other requests use normal buffers. | [TestEngineSharedMemory:564](../internal/queue/engine_test.go#L564), [control lifecycle:38](../internal/ctrl/commands_test.go#L38); S: `features/shared-memory` ([test:1291](../test/suite/tests_v1.go#L1291)), writable-region read/write only. No in-flight unregister/read-only-region VM oracle. |
| Zoned reporting, sequential read/write, open/close/finish/reset | E | [backend.go:321](../backend.go#L321), [ReportZones:45](../internal/queue/zoned.go#L45); Handler required; automatically uses user copy. | UAPI/conversion tests; Z: `features/zoned` ([test:1061](../test/suite/tests_v1.go#L1061)); one queue, Inline. Append/reset-all listed below. |
| Per-I/O FUA | E | [advertisement:1334](../backend.go#L1334), [FUABackend dispatch:43](../internal/queue/adapter.go#L43), [ZC RWF_DSYNC:1062](../internal/queue/engine.go#L1062); opt-in with volatile cache and FUABackend, Handler or ZC; plain Backend gets block-layer flush emulation. | [TestConvertFeatureFlags:208](../backend_convert_test.go#L208), ZC models check RWF_DSYNC; B: `features/fua` ([test:229](../test/suite/tests_v1.go#L229)); C/N: zero-copy tests accept O_DSYNC writes. Dispatch/acceptance, not crash durability. |
| Integrity metadata, CRC16 T10-DIF + reference tags | E | [backend.go:1280](../backend.go#L1280), [adapter.go:34](../internal/queue/adapter.go#L34), [metadata buffers:744](../internal/queue/engine.go#L744); IntegrityBackend or Handler, user copy, CONFIG_BLK_DEV_INTEGRITY. | UAPI/conversion tests; N: `features/integrity` ([test:1189](../test/suite/tests_v1.go#L1189)) with corrupted guard-tag rejection; recovery test above. |
| Multiple engines per queue / PER_IO_DAEMON | E | [queue.go:178](../internal/queue/queue.go#L178), [backend.go:750](../backend.go#L750); split tag ranges, cap threads at depth; unavailable with BatchIO. | Field/flag conversion tests and engine model; T: `features/integrity-threads-per-queue` ([registration:38](../test/suite/tests_v1.go#L38)), four threads requested per queue. |
| Online Resize / UPDATE_SIZE | E | [recover.go:185](../recover.go#L185), [commands.go:412](../internal/ctrl/commands.go#L412); running device only, backend must already serve new size; public mutex serializes Stop. | [control lifecycle:38](../internal/ctrl/commands_test.go#L38); T: `features/resize` ([test:279](../test/suite/tests_v1.go#L279)). |
| SafeStop / TRY_STOP_DEV | E | [backend.go:989](../backend.go#L989); open device returns ErrDeviceBusy and keeps serving. | Control opcode/model tests; N: `features/safe-stop` ([test:350](../test/suite/tests_v1.go#L350)). |
| NoPartitionScan | E | [backend.go:1308](../backend.go#L1308); optional NO_AUTO_PART_SCAN bit. | [TestConvertFeatureFlags:208](../backend_convert_test.go#L208); N: `features/no-partition-scan` ([test:324](../test/suite/tests_v1.go#L324)). |
| IO_DESC_SIZE, 32-byte descriptors | E | [backend.go:813](../backend.go#L813), [queue.go:128](../internal/queue/queue.go#L128), [DescriptorExtra:695](../internal/queue/engine.go#L695). | Flag/size validation tests; D: `features/io-desc-size` ([test:1357](../test/suite/tests_v1.go#L1357)), two queues, eight extra bytes. |
| DeleteDeviceAsync / DEL_DEV_ASYNC | E | [manage.go:109](../manage.go#L109); delete finishes when references close. | [control lifecycle:38](../internal/ctrl/commands_test.go#L38); A: `lifecycle/delete-async` ([test:575](../test/suite/tests_lifecycle.go#L575)). Pass on 6.10 contradicts the public comment's 6.11 threshold; probe command support. |
| Complete typed control/UAPI surface | E | [commands.go:85](../internal/ctrl/commands.go#L85), [uapi structs](../internal/uapi/structs.go); ADD/DEL/DEL_ASYNC, START/STOP/TRY_STOP, SET/GET_PARAMS, GET_INFO/INFO2, AFFINITY, FEATURES, START/END_RECOVERY, UPDATE_SIZE, QUIESCE, REG/UNREG_BUF; all seven param blocks represented, DEVT read-only. | [TestCommandsUseExactOpcodes:167](../internal/ctrl/commands_test.go#L167), [TestKernelCFixtures:319](../internal/uapi/kernel_fixture_test.go#L319), [context tests:44](../internal/ctrl/context_test.go#L44), [stack move:43](../internal/ctrl/pin_test.go#L43); B: `unit/internal_ctrl`, `unit/internal_uapi`. [test/ctrl:286](../test/ctrl/main.go#L286) exercises real commands; current matrix has no per-command ctrl results. UAPI parity alone does not prove every mode. |
| RAM and compressed RAM example backends | E | [mem.go:40](../examples/ublk-mem/mem.go#L40), [zip.go:115](../examples/ublk-mem/zip.go#L115); concurrent sharded RAM and flate chunks; volatile storage. | [TestRoundTripWithinAndAcrossShards:78](../examples/ublk-mem/mem_test.go#L78), [TestZipRandomDifferential:448](../examples/ublk-mem/zip_test.go#L448); B: `verify-sweep`, `loop-e2e`, `unit/examples_ublk-mem`. |
| File backend example, buffered/O_DSYNC/zero copy | E | [loop.go:39](../examples/ublk-loop/loop.go#L39), [Flush:122](../examples/ublk-loop/loop.go#L122), [ZeroCopyFile:211](../examples/ublk-loop/loop.go#L211); sparse file, discard/zeroes, read-only; remains an example rather than an importable backend package. | [TestLoopRoundTrip:229](../examples/ublk-loop/loop_test.go#L229), [TestSyncWritesO_DSYNC:609](../examples/ublk-loop/loop_test.go#L609); B: `loop-e2e`, `unit/examples_ublk-loop`; C/N: suite ZC file backend. |
| Supervised systemd deployment | E | [service:20](../examples/systemd/ublk-loop@.service#L20), [mount:13](../examples/systemd/srv-ublk0.mount#L13), [signal handling:131](../examples/ublk-loop/main.go#L131); mount After/Requires daemon, Before user.slice; dependent services need RequiresMountsFor. | [shutdown storm](../scripts/vm-shutdown-storm.sh), [soak](../scripts/vm-soak.sh). No shutdown-storm result in the matrix. Historical TODO #15 reports arm64 6.17.0-41 (9 ordered reboots), x86_64 7.0.0-38 (0/5 writeback failures after user.slice fix); no archived per-run receipt here. |

## Unverified or unavailable paths

| Feature/mode | Tier | Code status | Tests and matrix passes for this behavior |
|---|---|---|---|
| Effective CPU affinity / pinning | U | [backend.go:754](../backend.go#L754), [engine.go:212](../internal/queue/engine.go#L212); explicit or GET_QUEUE_AFFINITY-derived placement attempted; SchedSetaffinity errors ignored. | Conversion/CPU-mask decoding tests only; no effective-TID placement oracle, no matrix pass proving affinity. |
| BUF_REG_OFF_DAEMON | U | [recover.go:36](../recover.go#L36), [uapi/constants.go:141](../internal/uapi/constants.go#L141); reported bit only; engine registers on its owning thread. | Names/constants/fixture coverage; no off-owner registration test or matrix pass. |
| Zone append and reset-all | U | [CompleteZoneAppend:169](../internal/queue/request.go#L169), [commit LBA:943](../internal/queue/engine.go#L943), [batch LBA:440](../internal/queue/engine.go#L440); exposed to Handler. | Duplicate completion has an LBA-preservation regression; `features/zoned` never issues these ops, and there is no append/reset-all I/O model or matrix pass. |
| Integrity formats other than tested CRC16 + RefTag | U | [backend.go:248](../backend.go#L248); none/IP/CRC64 NVMe and other layouts exposed. | Serialization/conversion coverage only; no format-specific corruption/durability test or matrix pass. |
| IO_DESC_SIZE other than standard 24 or tested 32 | U | [backend.go:354](../backend.go#L354); accepts multiples of 8 through 256. | Validation/layout coverage only; no descriptor-stride I/O test or matrix pass at other sizes. |
| DMA alignment and segment constraints | U | [device.go:80](../internal/ctrl/device.go#L80); DMAAlignment public; SEGMENT block only internal ctrl/UAPI, no public segment settings. | [parameter round trips:181](../test/ctrl/main.go#L181), UAPI/model tests exist; no recorded behavioral DMA/segment enforcement pass. |
| NOUNMAP, fail-fast, META and SWAP semantics | U | [flags:55](../request.go#L55), [dispatch:694](../internal/queue/engine.go#L694); raw Handler sees bits. Backend cannot receive NOUNMAP; ZC ZERO_RANGE fallback punches holes without testing this flag. | Constant/layout tests only; no per-flag semantic oracle or matrix pass. |
| WRITE_SAME | U | [request.go:41](../request.go#L41); symbol exposed, default Backend [rejects unsupported ops:78](../internal/queue/adapter.go#L78); no carriesData path for WRITE_SAME. | No working implementation test or matrix pass. |
| Shared-memory regions across Recover | U | [recover.go:277](../recover.go#L277); kernel registrations survive, new process lacks mappings; matching requests fail. | No recovery test for this limitation or successful remapping; no matrix pass. |
| SQPOLL / IOPOLL | U | [ring.go:53](../internal/uring/ring.go#L53), [rejection:142](../internal/uring/ring.go#L142); unsupported setup flags, no public enable option. | No supported-mode test or matrix pass; not implemented. |
| NBD backend | U | No implementation or example in this tree; [Backend:7](../interfaces.go#L7) permits a caller to build one. | No NBD tests or matrix pass; not implemented. |

## Geometry and request bounds follow-up

The bounds hardening after the inventory baseline uses one pure
[layout gate](../internal/validation/layout.go#L40) for SQ/CQ/SQE mappings,
provided-buffer rings, descriptor arrays and owned queue buffers. It checks
positive representable mmap sizes, offset/span arithmetic, word alignment,
power-of-two ring counts, matching mapped counts/masks and SQE64/128 and
CQE16/32 strides. [Ring setup](../internal/uring/ring.go#L192) checks geometry
before mmap, reads mask/count words through checked byte slices, validates them
again, then forms pointers and caches the validated masks. Descriptor mappings
check queue ownership, 24..256-byte strides in multiples of eight, fixed
maximum-depth queue offsets and page rounding before mapping.

[DispatchRequest](../internal/validation/request.go#L86) guards the common
[engine dispatch](../internal/queue/engine.go#L692) before buffer preparation,
backend callbacks or file/registration SQEs. Copy, user copy, NEED_GET_DATA,
shared-memory dispatch, batch copy/user copy, manual/auto zero copy and batch
zero copy all reach this guard. It checks signed sector-to-byte conversion,
logical-block alignment, capacity end and payload limits; zero copy also
checks the backing-file base plus capacity. Integrity spans have a checked
multiplication before slicing. Device creation/recovery snapshot the configured
capacity; successful Resize updates the queues' atomic capacity limits.

DISCARD and WRITE_ZEROES ranges may exceed the payload-buffer size but must
fit the device. REPORT_ZONES treats the count as zones, checks its starting
offset and caps its 64-byte-entry report buffer; it does not interpret the
zone count as a sector range. Unknown-op policy and read-only enforcement are
separate from these arithmetic checks.

The [portable boundary tests](../internal/validation/layout_test.go),
[request/backend guard tests](../internal/validation/request_test.go) and new
`FuzzRingLayout`/`FuzzRequestRange` run natively on Darwin. The ring fuzzer also
reaches descriptor layout validation; both use fixed-size synthetic inputs and
independent arithmetic oracles, without allocating input-sized mappings.
Removing the span-end check made the committed `SQ/head/past-end` test fail;
the check was restored. New
[Linux engine/mapping/resize tests](../internal/queue/bounds_test.go) and the
existing real-ring fixtures compile, but Linux runtime validation is pending.
The existing `make test-uapi-fuzz` also needs Linux for its ctrl/engine targets;
the new targets are separate commands, not yet part of that make target.

The local validation run used Go 1.26.2 on Darwin arm64: portable validation,
UAPI and logging tests passed; Linux amd64 build/vet and test compilation passed.
Each new fuzz target ran once with a 60-second budget and two workers:
`FuzzRingLayout` executed **2,465,377** cases and `FuzzRequestRange` executed
**2,430,286**, both passing. The existing make target passed its three UAPI
fuzzers, then failed to compile the ctrl target on Darwin's missing io_uring
syscall constants; its ctrl and engine campaigns still require Linux.

The coordinator should run these commands on the disposable Debian VM, at this
candidate revision; these are pending runtime gates, not recorded passes:

```sh
GOFLAGS='-p=2' make test-unit
make test-uapi-fuzz FUZZ_PARALLEL=2
go test -p 2 -gcflags=all=-d=checkptr=2 ./internal/queue ./internal/uring
GOFLAGS='-p=2' make suite
sudo ./bin/ublk-suite
sudo env GO_UBLK_DISPOSABLE_TEST=1 GOFLAGS='-p=2' make test-large-io-kernel
```

`make test-unit` covers the new Linux bounds tests plus existing queue fake-kernel
models, io_uring ABI/index/real-ring tests, control models and public device/backend
tests. The suite covers real-kernel I/O modes and lifecycle; the focused large-I/O
test exercises public startup paths. Advanced-mode skips retain their reasons.

These bounds checks cover mapping geometry and request arithmetic. The
completion follow-up below adds batch CQE and pending-STOP checks; shared-region
address decoding, registration/loan lifetime, stale backend completions,
recovery reply arithmetic and resize transition ordering still need work.
No advanced mode is promoted by portable tests or cross-compilation alone.

## Completion and pending STOP follow-up (2026-10-10)

Work starts at `8c2135a`. The supplied worker brief reports a real 6.12 kernel
run for that revision: **44 suite passes, zero failures, checkptr clean**.
That observation is the starting floor, not a runtime pass for these changes;
the brief supplies no exact kernel build hash here.

**Ticket 4 — batch completions.** Previously FETCH sliced the tag-buffer array
with an unchecked buffer ID and length, and COMMIT indexed an unchecked slot
and divided its result without checking alignment or the submitted count.
[FetchTags and the batch ledger](../internal/completion/batch.go) now reject
those fields before accesses or dispatch. Each commit submission has a distinct
echoed identity and snapshots `(tag, generation)` independently of mutable
Request pointers. A partial commit retries exactly the unconsumed suffix;
a contradictory suffix that has already been reused, a duplicate delivery or
an old CQE for a reused commit slot fails. Invalid counts fail the engine
without retransmitting an uncertain completion.

[Deterministic engine schedules](../internal/queue/completion_schedule_test.go)
replay all five unchanged `FuzzEngine` scripts and retain malformed-field,
duplicate and partial-commit/early-fetch regressions. The portable
[FuzzCompletionSchedule](../internal/completion/batch_test.go) drives the same
decoder and ledger with a fixed-buffer fake completion source and an independent
ownership/terminal-result oracle. Raw hostile fields are not modulo-normalized;
oracle selftests detect a premature tag return and duplicate terminal result.
These cover batch ownership, not all ordinary/registration/file/wake CQE phases
or partial io_uring submissions. Linux engine replays are compiled, not run here.

**Ticket 5 — one pending STOP transition.** `Device.Stop` previously discarded
its in-flight receipt and cleared `leaving` on timeout, so `Close` could submit
another STOP while the original still ran. It now retains the exact receipt;
Stop/Close reconcile a reaped late result, and Resize, Detach and shared-memory
mutations refuse while the receipt is pending. A late success performs stop
cleanup once and Close then deletes without resubmitting STOP. Reaped cancellation
permits a later retry; a transport return without a final kernel CQE remains
pending. The public state string remains `running` until reconciliation.

The bounded [high-level API model](../pending_stop_test.go) checks a real retained
char fd plus modeled device/control-ring/storage counts across timeout, refused
mutations, late success and Close/retry. Separate [control tests](../internal/ctrl/pending_outcome_test.go)
check real mmap scratch ownership and the receipt's `Reaped` flag for success,
cancellation and an unreaped wait failure. Both Linux models await VM execution.
The [portable reconciliation gate](../internal/completion/control.go) is tested
on Darwin. START/DEL/recovery pending operations, direct transport errors without
an in-flight receipt, process-wide quotas and eventual cleanup of unreaped
transport failures remain outside this narrow fix.

**Ticket 6 — backend completion ownership.** `Request.finish` previously wrote
the result, copied user-copy read bytes and (for append) set the LBA before
winning its CAS. [Completion ownership](../internal/completion/ownership.go)
now claims before staging and publishes only afterwards. Returning from an
Inline handler while another goroutine stages completion transfers publication
to the async path; the engine cannot commit half-staged bytes. Portable race
tests check the poisoned buffer, result and enqueue count; Linux
[regressions](../internal/queue/completion_ownership_test.go) check user-copy
and append side effects and retain the stale-pointer witness.

**Still open:** a retained public `*Request` aliases the tag's next request.
An old caller can successfully complete that newer request and commit its
poisoned read bytes. A generation field inside the recycled object cannot
authenticate which delivery the caller owns. Recommend an immutable completion
handle containing device epoch, queue, tag and generation, with the generation
validated before any result/copy/enqueue effect. Legacy pointer-only Complete
methods must be retired or backed by a distinct Request object per delivery;
keeping them unrestricted would bypass the handle. This public API migration
(or an allocation/lifetime change to the preallocated request path) is not
implemented here. Batch command generations do not fix backend pointer reuse.

Local gates use Go 1.26.2, Darwin arm64 and Linux amd64 cross-compilation:
portable completion/validation/UAPI/logging tests with race and checkptr;
Linux build/vet and all-package normal/checkptr test compilation. No Linux
binary is executed locally. The final two-worker fuzz runs passed:
`FuzzCompletionSchedule` **1,191,083** executions / 60 s,
`FuzzRequestRange` **1,187,155** / 30 s, and
`FuzzRingLayout` **1,127,630** / 30 s. These are smoke budgets, not the larger
assurance campaigns. Logs and compiled artifacts are retained in `.scratch/`.

`bin/ublk-suite` is rebuilt, as are checkptr binaries for the four internal
packages (queue, ctrl, uring, uapi) and the public package (`ublk.test`) so the
new high-level STOP model is included. From this clone in the disposable VM:

```sh
for p in ublk queue ctrl uring uapi; do
  sudo .scratch/vm-bin/$p.test -test.v -test.timeout=5m || exit 1
done
sudo ./bin/ublk-suite
```

VM runtime/checkptr and real-kernel suite results on this candidate are pending;
no lifecycle or advanced-mode support tier changes follow from these gates.

## Coverage limits and documentation corrections

The fuzz foundation includes [FuzzFixedUAPI/FuzzParamsUAPI/FuzzUAPIEncodings](../internal/uapi/fuzz_test.go#L12),
[FuzzCtrlDecoders](../internal/ctrl/fuzz_test.go#L15), [FuzzEngine](../internal/queue/fuzz_test.go#L21),
and the portable bounds and completion targets described above.
FuzzEngine exercises copy, NEED_GET_DATA and batch with depths 1..8, at most
64 scripted requests, valid tags, five operations and 24-byte descriptors.
It does not reach user copy, ZC, shared regions, recovery, zoned or integrity.
CI has race/vet and bounded fuzz targets; the [KVM matrix workflow](../.github/workflows/kernel-matrix.yml#L5)
is explicitly untested in Actions, and there is no saved arm64 matrix.
The basic/FS/stress tests in [integration_test.go:83](../test/integration/integration_test.go#L83)
are skipped placeholders; use the suite and guarded large-I/O test as evidence.

TODO's missing recovery/FUA/ZC/async/NEED_GET_DATA/unit/fuzz work and systemd
unit task contradicted implemented code; #4/#5 were already obsolete. The
claimed single-queue requirement and 6.8 minimum contradicted defaults and
6.4 results. Whole-UAPI claims in README/CLAUDE mean bindings exist, not that
every flag has a tested operational mode. INTERNALS' unconditional QUIESCE
handoff omits the batch exception. Public Request.CompleteN prose omits the
copy-only partial-read restriction. Historical shutdown text inferred an
actual coredump from a hung-task label; the cause remains unproven.

## Gaps

Highest risk first; these are missing tests, not claims that an observed defect
exists in every configuration. Run kernel tests in disposable Linux guests.

| Priority/path | Existing coverage and missing test that would close the gap |
|---|---|
| **1. Shutdown / #15 and failed teardown** | Storm/soak and copy close-under-load cover useful cases, but do not settle the unsupervised wedge or STOP timeout lifetime. Reboot full-distro guests under buffered filesystem load with bare, correctly ordered and deliberately busy-unmount deployments; include SIGTERM then SIGHUP and escalation, capture serial output and blocked `/proc/<tid>/stack`, assert acknowledged data, reboot completion and no persistent D-state. Inject failed/timed-out STOP, late CQEs and a held Handler request: assert queues/char fd remain usable, no premature unmap/DEL, and eventual cleanup releases fd/mmap/device counts. |
| **2. Zero-copy buffer lifetime** | Happy manual/auto/fallback models and ZC close-under-load do not hold real registrations/file CQEs across failure. Force manual, auto and batch-auto fallback, delay/reorder register/file/unregister CQEs, inject errors/ENOBUFS/partial commits and cancel/Detach at each stage; track tag/slot ownership and registration counts, canaries and byte oracle, prove no reuse/unpin before final access. For SHMEM, hold a Handler request while unregistering/re-registering/unmapping writable and read-only regions, and verify both matched and fallback I/O. |
| **3. Recovery failure states and combinations** | Copy, batch and CRC16 handoffs pass; zero-copy/zoned/shared-region recovery and failures between START/END_RECOVERY have no adequate coverage. Kill old/new servers at each acquisition/transition, inject quiesce/start/end timeout and partial queue setup, race two recoverers; test all recovery policies under multi-queue load with acknowledged-write and fd/mmap/registration/device oracles. Include ZC and zoned modes, explicit rejection or remapping of shared regions, and preservation of FUA/integrity semantics. |
| **4. Unprivileged boundary and real udev** | Current suite uses one nobody owner with root-emulated udev and root I/O. In a full-distro guest install the supplied rule/helper, run two unrelated UIDs and user namespaces; assert owner access and rejection of cross-owner control/I/O, forged/stale dev paths, ID reuse and forbidden flag combinations; delay/fail node ownership and verify bounded cleanup and no leaked device. |
| **5. FLUSH/FUA durability and ordering** | FUA tests count dispatch/acceptance; historical SIGKILL/guest-reset checks do not drop the host's virtual-disk cache. Use an independently validated lost/torn/aliased-block oracle on a durable file/block backend, reorder/delay writes across FLUSH and FUA, assert completion only after the promised writes are stable, then remove the backing host/cache failure domain. Cover buffered, O_DSYNC and ZC plus integrity metadata; retain synced and unsynced witnesses and demonstrate injected failures are detected. |

Further work: batch buffer exhaustion/partial SQ submission and hostile ordinary,
registration/file/wake CQE models, acquisition-failure resource accounting, effective CPU-affinity checks,
append/reset-all and other integrity/descriptor formats, public NOUNMAP semantics,
and fresh candidate runs on x86_64 and arm64. These qualify the scopes above.
