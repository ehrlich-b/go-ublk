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
