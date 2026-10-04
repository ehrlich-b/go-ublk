# go-ublk Internals

How the library is put together, for people changing it. The kernel side of ublk
is documented on the site's guide (site/content/guide/); this file covers
go-ublk's own structure. Kernel facts cited here are from
`drivers/block/ublk_drv.c` at v7.3-rc5 unless noted.

## Packages

| Package | Role |
|---|---|
| `ublk` (root) | Public API: `Device`, `DeviceParams`, `Create`/`Start`/`Stop`/`Close`, `Detach`/`Recover`, `Handler`/`Request`, feature probing |
| `internal/ctrl` | One typed method per control command on `/dev/ublk-control`; feature negotiation; unprivileged dev-path handling; off-heap command buffers |
| `internal/queue` | The data plane: `Queue` (one per hardware queue) and `engine` (one OS thread + io_uring per tag range) |
| `internal/uring` | A small, allocation-free io_uring core (`IoUring`) plus the older `Ring` wrapper the control plane uses |
| `internal/uapi` | Every constant and struct of the 7.3-rc5 `ublk_cmd.h`, with C-fixture layout parity tests |

## Device lifecycle

```
Create:  GET_FEATURES -> ADD_DEV -> SET_PARAMS (read back with GET_PARAMS)
Start:   open /dev/ublkcN -> per queue: mmap descriptors, start engines (FETCH every tag)
         -> START_DEV (waits until every tag has a FETCH outstanding)
Stop:    STOP_DEV (the kernel drains in-flight I/O through the running engines)
         -> engines see ABORT on every tag and exit -> free mappings -> close /dev/ublkcN
Close:   Stop -> DEL_DEV (waits for the last /dev/ublkcN reference)
Detach:  QUIESCE_DEV (6.16+) -> abandon engines (commit what handlers hold, close rings)
         -> close /dev/ublkcN; the kernel moves the device to QUIESCED or FAIL_IO
Recover: GET_DEV_INFO + GET_PARAMS -> START_USER_RECOVERY (retried while EBUSY)
         -> start engines -> END_USER_RECOVERY (waits for every FETCH) -> LIVE
```

Ordering rules that cost real bugs to learn:

- `STOP_DEV` must be sent while the engines still run: `del_gendisk` waits for
  in-flight requests that only they can complete (Critical Bug #8). Cancelling
  the serving context therefore triggers `Stop`, never a raw engine shutdown
  (#22).
- `DEL_DEV` blocks until every reference to `/dev/ublkcN` is gone: every ring
  that registered it, every dup, every mapping. Engines close their rings on
  their own thread before signalling exit.
- A stopped device is never started again (`ErrStopped`); kernels oops or wedge
  (#23).
- Memory the kernel or a handler may still touch is leaked, never freed: a
  `Queue` unmaps only after every engine has exited and no handler holds a
  request (#19).

## The engine (internal/queue)

An engine serves a contiguous range of one queue's tags (the whole queue unless
`ThreadsPerQueue` > 1, which uses `UBLK_F_PER_IO_DAEMON`). It is a goroutine
locked to its OS thread for life — never unlocked, so the thread dies with it,
which is how kernels without uring_cmd cancellation notice a dead server — owning
one io_uring set up with `COOP_TASKRUN | SINGLE_ISSUER | DEFER_TASKRUN` where
available (the flags the kernel's kublk selftest server uses).

Per-tag states: `Fetching` (kernel owns), `GetData` (NEED_GET_DATA in flight),
`Registering` / `FileIO` (zero copy), `Handling` (server owns), `Aborted`,
`Orphaned` (arrived while abandoning; left for the kernel).

The loop: drain the completion list, flush batch commits, then
`SubmitAndWait(1)` and dispatch CQEs by the kind in the top byte of user_data:
`IO` (FETCH / COMMIT_AND_FETCH / NEED_GET_DATA results), `Wake` (the eventfd
read), `Reg`/`ZC`/`Unreg` (zero copy), `Prep`/`Fetch`/`Batch` (batch I/O).

**Dispatch.** Inline mode calls the handler on the engine thread; otherwise each
request runs on its own goroutine. `Request.Complete` finishes through an atomic
state machine (`dispatching -> done` commits inline without a syscall;
`async -> queued` pushes onto a lock-free stack). A completer that pushes and
sees the engine `sleeping` writes an eventfd whose read is always armed in the
engine's ring. The engine sets `sleeping` and then re-checks the stack before
blocking, so a push between drain and sleep is never lost
(`TestEngineNoLostWakeup` fails without that re-check).

**Results.** READ/WRITE report bytes; everything else 0. Only a READ in copy
mode may complete partially (the kernel requeues the rest); in user-copy and
zero-copy modes, and for writes, the kernel treats any non-negative result as
complete success, so `CompleteN` fails short transfers with EIO. Errors map to
errnos (`queue.Errno`), which newer kernels pass through `errno_to_blk_status`.

**Data modes.**

| Mode | FETCH/COMMIT addr | Data path |
|---|---|---|
| copy (default) | the tag's buffer (anonymous mmap, `QueueDepth × MaxIOSize` per queue) | kernel copies into/out of it |
| NEED_GET_DATA | same | a WRITE first completes with RES_NEED_GET_DATA |
| user copy | 0 | `pread`/`pwrite` on `/dev/ublkcN` at `UBLKSRV_IO_BUF_OFFSET + qid<<41 + tag<<25` (integrity: bit 62) |
| zero copy | 0; `sqe->addr` = auto-buf-reg slot (tag) | `READ_FIXED`/`WRITE_FIXED` on the backing file from the ring's sparse buffer table; `FSYNC`, `FALLOCATE` |
| shared memory | as copy | requests flagged `SHMEM_ZC` point into a registered region; no copy |
| batch | element buffers | PREP_IO_CMDS once; multishot FETCH_IO_CMDS into a provided-buffer ring; COMMIT_IO_CMDS per round |

## Memory safety rules

- Anything whose address is handed to the kernel lives off-heap (`uring.AllocOffHeap`)
  or on the heap kept reachable until its CQE; never on a goroutine stack, which
  the runtime moves. Control-command buffers are mmap'd pages owned by a ring
  slot and leaked if a command is abandoned (#21).
- Shared ring indices are touched only through `sync/atomic`; Go's atomics give
  the release/acquire ordering the io_uring protocol needs (see `ring.go`).
- `uring.IoUring.Close` unmaps every region (#17); the legacy `Ring` defers it
  until no caller is inside a method.

## Tests

- `internal/queue/fakekernel_test.go` is a model of ublk_drv's per-tag protocol
  (including zero copy, batch I/O with partial commits, shared memory) behind the
  engine's `ring` interface. `FuzzEngine` drives the real engine through random
  scripts against it.
- `test/suite` (`make suite`) is the real-kernel conformance suite; `test/matrix`
  boots it under many kernels. Every finding from those runs is in `TODO.md`.

## Key files

| File | Purpose |
|---|---|
| `backend.go`, `recover.go`, `request.go`, `manage.go` | public API |
| `internal/queue/engine.go` | per-tag state machine, dispatch, data modes |
| `internal/queue/queue.go` | per-queue mappings and engine lifecycle |
| `internal/ctrl/commands.go`, `features.go` | control commands, negotiation |
| `internal/uring/ring.go`, `prep.go`, `register.go` | io_uring core |
| `internal/uapi/constants.go`, `structs.go` | kernel UAPI |
