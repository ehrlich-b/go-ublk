# Dispatch measurements

`examples/ublk-mem` accepts `-backend ram|null` (default `ram`), `-inline`
(default false), and `-dispatch goroutine|pool|auto|adaptive` (default
`goroutine`). `examples/ublk-loop` accepts the same dispatch and inline flags.
`-zip`
compresses the RAM backend; combining it with `-backend null` is rejected.
The null handler completes requests without reading or writing their buffers.
It is a transport benchmark target, so it does not provide stored data.

The library selects asynchronous scheduling through `DeviceParams.Dispatch`:
`DispatchGoroutine` launches a goroutine per request, while `DispatchPool`
creates `QueueDepth` long-lived handler goroutines per queue. With multiple
engine threads, each engine owns one worker per tag in its assigned range.
Pointers travel through a depth-sized buffered channel; no per-request job is
allocated. Each tag can have only one uncompleted job, so the engine can hand
off every tag even when all workers block. Workers run the same handler,
user-copy and generation-bound panic recovery code as goroutine dispatch.
The ownership state machine and completion-list wakeup path are unchanged.

## Automatic dispatch candidates (G2b)

The coordinator's Linux 6.12 measurements, medians of three interleaved
rounds with two queues and depth 64, establish the starting point:

| RAM mode | qd1 IOPS | qd1 p50 µs | syscalls/I/O | context switches/I/O | 2×qd32 IOPS | p99 µs |
|---|---:|---:|---:|---:|---:|---:|
| Goroutine | 20.3k | 42.2 | 7.12 | 4.12 | 553k | 185 |
| Pool | 19.8k | 43.3 | 7.13 | 4.13 | 539k | 183 |
| Inline | 43.5k | 16.5 | 1.08 | 1.07 | 689k | 148 |

Pooling did not reduce the handoff cost. Inline improved both workloads,
but an arbitrary file/network callback can block its entire queue.

`DispatchAuto` implements the declared strategy. A Backend or Handler can
implement `NonBlockingDeclarer`, whose `NonBlocking() bool` method promises
prompt return for every operation when true. The engine samples it once at
creation. True selects the existing inline completion protocol; absence or
false selects goroutine dispatch. The Backend adapter forwards the declaration
only when its synchronous observer also declares nonblocking behavior, or is
nil. Built-in metrics and no-op observers declare it. Plain ReaderAt/WriterAt
backends do not implicitly qualify. The uncompressed example RAM backend and
null handler declare it; compression and the loop file backend do not.
Automatic modes keep user-copy engines asynchronous because copy-in/copy-out
perform syscalls even when the handler itself only uses RAM. Explicit `Inline`
continues to override the scheduling choice; zero copy bypasses handlers.

`DispatchAdaptive` is a conservative variant of the declared strategy. For
eligible handlers, it snapshots the ready CQEs before invoking any callback.
A singleton request runs inline when no prior handler still holds a request.
Multiple ready requests all run on goroutines, including every tag in a
multi-tag batch FETCH and singleton FETCHs sharing the same CQE snapshot.
Control-only CQEs do not count as ready requests. Snapshot storage is allocated
once per engine, reused, and capped at the requested ring entry count, so a
continuous arrival stream cannot postpone dispatch indefinitely. The ready
count is frozen before collecting the snapshot; a backlog exceeding the cap
conservatively uses goroutines even if the captured prefix has one request.
Other handlers use goroutines at every depth.

**Adaptive bound and the change from speculative inline:** readiness alone
cannot safely decide whether an arbitrary synchronous Go callback will block.
Once called on the locked queue thread, its stack cannot be moved to a worker
after a timeout. A watchdog cannot resume that queue thread, and a historical
latency estimate cannot bound the first slow call. Consequently this candidate
does not inline undeclared singleton handlers. Their queue delay attributable
to an inline handler is **zero**: a later ready request is dispatched without
waiting for the blocked callback, even if that callback never returns.
This is a scheduling property, not a real-time wall-clock guarantee from Go
or the OS. The fake-ring regression holds the first callback blocked and
requires a later arrival to dispatch and commit within a 250 ms test watchdog,
in both ordinary and batch I/O. Declared inline callbacks can delay a later
arrival for their entire execution; their promise is not a forced time limit.
An incorrect declaration, contended RAM locks, CPU-heavy work, GC pauses and
callback code after completion remain risks. There is no hard numeric bound
for a handler that violates the declaration.

Both candidates reuse the same generation-bound ownership state, panic-to-EIO
recovery, async completion after inline return, draining and batch ledger.
No per-request ownership atomic or allocation is added to the successful inline
path. The engine fake-kernel tables exercise both candidates with declared and
undeclared handlers, including user copy, NEED_GET_DATA, shared memory,
integrity, stale handles, panic recovery, stop, detach and batch retries.

**Recommendation:** after the coordinator validates these candidates, ship
`DispatchAuto` as the default. It applies the measured RAM fast path at both
depths and preserves goroutine concurrency for unknown/blocking backends.
Adaptive is an experiment: it deliberately restores the expensive handoff
under RAM load, so the supplied numbers predict a throughput regression there.
Require backend authors to audit every operation and observer before declaring
nonblocking behavior. Keep the library and example defaults as
`DispatchGoroutine` until Bryan decides from fresh VM correctness and A/B data.

`Inline` takes precedence over `Dispatch`, and zero copy bypasses handlers.
All modes permit a handler to return before arranging asynchronous completion.
Completion releases its request; pool workers finish any callback code after
completion before taking another job. Stop and abandon drain all delivered
requests, then close the pool channel. Like goroutine dispatch, teardown does
not wait for callback code that continues after completion. A callback that
never returns still occupies its worker. Blocking handlers should finish
their work before calling `Complete`.

These options are experimental performance candidates. Defaults have not
changed. Fake-ring timings exclude the kernel round trip and cannot establish
a fio win; the coordinator must run interleaved VM measurements and the Linux
correctness suite before selecting an optimization.

## Fake-ring dispatch

`make benchmark-dispatch` runs the real engine's CQE decoder, range guard,
buffer setup, backend adapter, completion ownership, handoff and commit
submission against an allocation-free fake ring. It rotates through a
depth-64 queue, delivering one 4 KiB read at a time. It warms workers first,
then measures steady-state time and allocations. It excludes data copying,
kernel waits and eventfd syscalls. `TestDispatchHotPathDoesNotAllocate`
requires exactly zero allocations per I/O for inline, pool, auto and adaptive
singleton dispatch. Adaptive measurements include the CQE snapshot.

This Mac cannot compile the Linux transport natively. For the local numbers,
the production engine, request, handle, adapter, pool and benchmark source
files were copied to `.scratch/dispatch-harness/queue`. Only the io_uring
imports and unavailable Linux operations were redirected: the SQE/CQE types,
preparation and off-heap helpers are copied production code; eventfd and
affinity substitutes panic if reached. No substitute is reached by these
benchmarks. The completion and validation packages are imported unchanged.
The real Linux queue benchmark is cross-compiled for the coordinator; these
Darwin numbers are userspace bookkeeping measurements, not Linux results.

Go 1.26.2, Darwin arm64, Apple M4, `GOMAXPROCS=2`,
`taskpolicy -b nice -n 15`, three serial samples per mode:

| Mode | Final 1 s samples, ns/op | B/op | allocs/op |
|---|---|---:|---:|
| Inline | 104.0, 145.2, 167.0 | 0 | 0 |
| Goroutine | 1183, 1103, 1101 | 24 | 1 |
| Pool | 1050, 998.8, 908.1 | 0 | 0 |

Earlier passes, with the same allocation counts:

| Pass | Inline ns/op | Goroutine ns/op | Pool ns/op |
|---|---|---|---|
| Initial, 500 ms | 180.5, 151.1, 158.8 | 914.7, 936.2, 1000 | 1005, 832.3, 913.0 |
| Errno/batch fixes, 1 s | 187.6, 186.1, 199.3 | 1054, 1141, 1116 | 958.4, 986.6, 1041 |
| Preserved handoff layout, 1 s | 185.2, 195.2, 197.7 | 1199, 1305, 1311 | 1181, 1137, 1130 |

The final implementation stores pool state at the end of the engine and
selects dispatch using the mode byte beside `inline` in its configuration.
Goroutine dispatch does not load the pool channel. A local comparison with
`1c80053` verifies that configuration size and the offsets of `handlers`,
`head`, `sleeping` and `wakeMu` match the base.

No timing win is claimed from these samples. All raw outputs and the source
copy generator are in `.scratch/dispatch-*.log` and
`.scratch/make-dispatch-harness.py`. On Linux, run:

```sh
./queue.test -test.run '^TestDispatchHotPathDoesNotAllocate$' \
  -test.bench '^BenchmarkDispatch(Inline|Goroutine|Pool|Auto|Adaptive)$' \
  -test.benchmem -test.count 3 -test.benchtime 1s -test.cpu 2
```

## Escapes

Linux amd64 `go build -gcflags=-m ./internal/queue ./internal/completion`
reports no heap escape for request handles, completion staging/enqueue
callbacks, the range-guard callback or panic-recovery closure. Engine arrays,
worker launch records, channels, maps and the backend adapter are setup
allocations. Dispatch adds no ownership atomics; generation and phase remain
in the existing single word.

The allocations that can occur while serving I/O are:

| Path | Allocation | Result |
|---|---|---|
| Goroutine dispatch | Compiler-generated launch record for `go e.call(r)` | 24 B / 1 allocation per I/O remains in the default; pool avoids this record. |
| Errno conversion | `request.go`'s `errors.As` target escapes | Fixed nil and plain errno cases with early returns; wrapped/custom errors still allocate the target. The new allocation regression passes for nil, valid and invalid plain errno values. |
| Batch fetch | `completion.FetchTags` allocates the tag snapshot; `Batch.Deliver` allocates delivery tokens | Retained: these snapshots validate the entire fetch before dispatch and protect buffer reuse. |
| Batch submission | `engine.flushBatchCommits` copies sent request pointers; `Batch.Submit` copies immutable tokens; pending slices may grow | Retained. The temporary token staging slice does not escape, though large dynamic slices may still use heap storage. |
| Partial batch completion | `engine.committed` allocates retry pointers and prepends them to pending requests | Retained for a nonempty retry; fixed the full-consumption path so it no longer copies unrelated pending requests. |
| Invalid request, protocol, user-copy or backing-file error | Formatted errors box diagnostic fields; `Errno` may allocate its unwrap target | Exceptional paths retained. No such allocation occurs on the measured successful copy path. |
| Handler panic or duplicate legacy completion | Logger variadic arguments/fields, errno boxing, formatted panic string | Exceptional paths retained; recovery still uses the captured generation. |
| Unknown operation rendered as text | `Op.String` boxes the operation for formatting | Exceptional diagnostic path retained. |

The complete compiler receipts are `.scratch/escapes-linux.log`; compiler
escape diagnostics alone are not an allocation count. The zero-allocation
gate measures the successful non-batch dispatch path. Batch allocation
removal needs a separate ownership-preserving design and VM validation.
The plain-errno allocation regression fails against the pre-fix source with
one allocation in each case, and passes against the final source with zero.

## VM profiling

Run as root on a disposable Linux VM with `fio`, `perf`, Python 3 and the
ublk driver available:

```sh
ROUNDS=3 OUT=.scratch/profile scripts/perf-profile.sh ./ublk-mem \
  '-backend ram' '-backend ram' '-backend ram -inline' \
  '-backend null' '-backend null -dispatch pool' '-backend null -inline'
```

For the G2b candidates, run these exact commands from the repository root on
the disposable VM after copying the binaries into `.scratch/vm-bin/`:

```sh
ROUNDS=3 OUT=.scratch/profile-auto-ram scripts/perf-profile.sh .scratch/vm-bin/ublk-mem \
  '-backend ram' '-backend ram' '-backend ram -inline' \
  '-backend ram -dispatch auto' '-backend ram -dispatch adaptive'
SERVER=loop BACKING_DIR=/dev/shm ROUNDS=3 OUT=.scratch/profile-auto-loop \
  scripts/perf-profile.sh .scratch/vm-bin/ublk-loop \
  '-dispatch goroutine' '-dispatch goroutine' '-inline' \
  '-dispatch auto' '-dispatch adaptive'
```

The loop pass requires room for 512 MiB in the selected tmpfs. The harness
creates one private backing file, materializes all its pages before profiling,
and reuses it across the read-only workloads. It removes that file after its
server has stopped; a stuck server keeps the backing file for investigation.
`SERVER=loop` rejects RAM-only flags and `CPU_PROFILE=1`. It owns `-file`, so
flag sets cannot point the benchmark at unrelated data. Loop auto/adaptive
retain goroutine dispatch at qd1 too; this pass checks that conservative
fallback against the baseline, rather than claiming speculative file inlining.

Each flag set gets a fresh 512 MiB device with two queues and depth 64. Rounds
interleave variants and rotate their starting position. Duplicate baseline
sets supply an A/A comparison. Each device runs direct `io_uring` 4 KiB
randread for 10 seconds at one job/depth 1, then two jobs/depth 32. Flag sets
accept `-backend`, `-dispatch`, `-inline`, `-zip` and `-v` for RAM, or
`-dispatch`, `-inline` and `-v` for loop; booleans use
`-inline=false` syntax. Geometry, cleanup and profile paths belong to the
harness. Argument strings are split on whitespace and never evaluated as
shell code.

`perf stat` attaches to the server's PID and threads for each fio process's
lifetime and counts `context-switches`, `cpu-migrations` and
`raw_syscalls:sys_enter`. Counts are divided by fio's aggregate completed
read I/Os. IOPS and p50/p99 come from the group-reported read result's
`clat_ns` percentiles, converted to microseconds. Counters cover server
threads, rather than fio or total system CPU. The brief process launch/exit
interval around fio is included in the perf window. Unsupported, uncounted
or missing events fail the report instead of becoming zero.

Per-run rows are printed and saved as `results.tsv`. The final table groups
by flag set and workload, reports the median of each round's IOPS/p50/p99,
and divides summed counters by summed I/Os. Raw fio JSON, perf CSV, server
logs, binary checksum and run geometry remain in the output directory.
`CPU_PROFILE=1` additionally writes a per-device `cpu.pprof`; use it in a
separate profiling pass because sampling can perturb comparisons.

The harness discovers only the device announced by its own server. Cleanup
signals only that PID, waits up to 30 seconds and reports a stuck daemon
without killing or reaping unrelated devices. Output directories must be
new. Local validation includes Bash syntax, ShellCheck, Python compilation,
synthetic fio/perf normalization and aggregation, invalid-event rejection,
argument rejection and mocked startup failure/cleanup. Actual perf/fio and
CPU-profile capture remain coordinator-owned VM gates.

`make check-dispatch-linux` runs Linux amd64 vet and the full package/example
build. `make test-dispatch-portable` runs the Darwin-compatible packages and
metrics tests normally and with the race detector. `make dispatch-vm-binaries`
also builds `queue.test`, `ublk.test`, both example test binaries and both
daemons into `.scratch/vm-bin/`, with `SHA256SUMS`. Run every test binary on
Linux with `-test.v -test.timeout 5m`; use `.scratch/vm-bin/queue.test -test.run '^$'
-test.bench '^BenchmarkDispatch(Inline|Goroutine|Pool|Auto|Adaptive)$'
-test.benchmem -test.count 3 -test.benchtime 1s -test.cpu 2` for bookkeeping
measurements. Cross-compilation is not a Linux execution receipt.

This candidate also passed the full fake-ring queue suite and its fuzz seeds
normally and with `-race` on Darwin. `.scratch/make-auto-harness.py` copies
every queue source and test, redirecting only io_uring imports and unavailable
Linux syscall bindings/constants. The copied SQE/CQE layouts, preparation,
off-heap allocation, engine, adapter and ownership logic run unchanged.
Real io_uring and affinity entry points panic if reached; an AF_UNIX socket
pair substitutes for eventfd wakeups. This is userspace validation, with the
Linux binaries still requiring coordinator execution. The full gate and
zero-allocation receipts are in `.scratch/validation-final.log`.
