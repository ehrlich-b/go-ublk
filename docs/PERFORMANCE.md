# Dispatch measurements

`examples/ublk-mem` accepts `-backend ram|null` (default `ram`), `-inline`
(default false), and `-dispatch goroutine|pool` (default `goroutine`). `-zip`
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
requires exactly zero allocations per I/O for inline and pool dispatch.

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

| Mode | Initial 500 ms samples, ns/op | Final 1 s samples, ns/op | B/op | allocs/op |
|---|---|---|---:|---:|
| Inline | 180.5, 151.1, 158.8 | 187.6, 186.1, 199.3 | 0 | 0 |
| Goroutine | 914.7, 936.2, 1000 | 1054, 1141, 1116 | 24 | 1 |
| Pool | 1005, 832.3, 913.0 | 958.4, 986.6, 1041 | 0 | 0 |

No timing win is claimed from these samples. All raw outputs and the source
copy generator are in `.scratch/dispatch-*.log` and
`.scratch/make-dispatch-harness.py`. On Linux, run:

```sh
./queue.test -test.run '^TestDispatchHotPathDoesNotAllocate$' \
  -test.bench '^BenchmarkDispatch(Inline|Goroutine|Pool)$' \
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
