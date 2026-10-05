# Compatibility and acceptance evidence

This checkpoint distinguishes C-header ABI checks, Go userspace execution,
cross-compilation, and actual ublk device I/O. A kernel release number alone does
not establish support: the driver configuration and distribution backports matter.
The documented device requirement remains Linux 6.8+ pending older-kernel acceptance.
The bindings use little-endian command fields and native parameter/descriptor copies;
big-endian operation is not supported or tested.

The [saved execution summary](verification/uapi-20261002/summary.json) identifies
tested source commit `f85f9fc`, exact commands, header hashes, resource limits,
review disposition and the released task lane. Final Linux fuzzing completed
5,019,623 fixed-record executions in 30 seconds and 14,369,754 parameter executions
in 90 seconds, with no failures. The complete userspace/race/vet/build checks and
integration-binary compilation passed. The source review's single P2 was corrected
and [reviewed again](verification/uapi-20261002/correction-review.md).

The existing five `MarshalInto` benchmarks each reported 0 B/op and 0 allocs/op
on macOS arm64 ([allocation output](verification/uapi-20261002/marshal-allocations-darwin-arm64.log)).
These short runs check allocation behavior; their timing is not a device benchmark.

## 2026-10-03 feature and error hardening

The source-only follow-up at `5ad6224` rejects `EnableUserCopy` with an
`errors.Is`-compatible `ErrNotImplemented` before either public creation path
opens a controller. The runner does not implement the character-device
`pread`/`pwrite` data path required by that flag. Default address-buffer operation
is unchanged. No feature implementation or new kernel support is claimed.

`backend.go` now wraps error causes with `%w`, preserving controller failures
through public lifecycle calls. `ListDevices` skips only wrapped `ENODEV`, the
kernel's absent-ID response, and fails with the queried ID for other errors.
An incomplete scan returns no partial list. This retains the existing bounded
ID scan; it does not expand enumeration above its current 64-ID limit.

The [saved verification summary](verification/feature-errors-20261003/summary.json)
records native WSL userspace unit/race/vet/build passes, targeted regressions,
Linux arm64 cross-build and integration compilation. Deliberately restoring
the prior guard/wrapping/enumeration behaviors in the task-private Linux copy
made all three regression groups fail; exact source bytes were restored before
the passing checks. Those negative controls are not a device test or a checkout
of the original commit. The independent source [review](verification/feature-errors-20261003/review.md)
found no blocking issues. Direct synthetic coverage of public command-result,
Start, character-open and runner failures remains incomplete; lower-level queue
wrappers can still lose causes before a public wrapper sees them.

The [proposed personal guest acceptance](kernel-acceptance.md) records ownership,
startup effects, resource/data isolation, optional guest-only module action and
cleanup. No guest was booted. The existing read-only home mount must be omitted
before an approved run; current guest kernel capability is unverified.

## 2026-10-02 UAPI hardening

`struct ublk_params` contains fixed-position blocks. `types` selects which blocks
are valid; it does not remove preceding fields. The corrected marshaler puts basic,
discard, devt and zoned at offsets 8, 40, 60 and 76, sending the prefix through the
last selected block. A BASIC|DEVT response has a 20-byte discard hole. The old
implementation packed DEVT at 40 and emitted 56 bytes instead of 76.

Serialized-record decoding now rejects lengths below the header, lengths past the received buffer,
and selected blocks beyond the declared length, without changing the destination.
It clears absent blocks on successful reuse and tolerates appended data and unknown
type bits inside the declared length. It does not implement the newer appended
parameter types. `MarshalInto` clears holes in reused buffers and preserves its
short-buffer/no-write and trailing-canary contracts.

`GET_PARAMS` now supplies the input length required by the driver, reserves 256
bytes for appended fields, and keeps that buffer alive through submission. Its
response decoder bounds known fields by the supplied buffer capacity, separately
from strict serialized-record validation: the kernel retains the SET_PARAMS
length while adding DEVT on GET, and can report zero before the first SET.
Regressions cover retained lengths 0, 40, 60, 112 and 4096 and truncated capacity.
Control operations and io_uring wrappers retain errno chains;
`WrapError` classifies errors through intermediate wrapping. Controller close
consumes resources once and returns both ring and descriptor close errors. Close
is sequentially idempotent; concurrent controller use is not newly guaranteed.
Some unchanged public lifecycle wrappers in `backend.go` still format causes
with `%v`; these commits do not make every public Create/Start failure classifiable.

The regressions fail against the original `25f8e5e` marshaler: the independent C
BASIC|DEVT fixture differs, and a declared length of zero is accepted while mutating
the destination. Existing golden fixtures for contiguous block selections still pass.

## Independently compiled Linux headers

`scripts/uapi-fixtures.c` was compiled with `cc -Wall -Werror` on Linux amd64,
using each pinned upstream header. Its emitted little-endian prefixes match
`internal/uapi/testdata/linux-le-fixtures.txt`. These programs open no device.

| Header | C params sizeof | Known offsets | C byte fixtures | Kernel/device run |
| --- | ---: | --- | --- | --- |
| [v6.0](https://raw.githubusercontent.com/torvalds/linux/v6.0/include/uapi/linux/ublk_cmd.h) | 64 | basic 8, discard 40 | ctrl, io, basic | None |
| [v6.6](https://raw.githubusercontent.com/torvalds/linux/v6.6/include/uapi/linux/ublk_cmd.h) | 112 | plus devt 60, zoned 76 | plus BASIC|DEVT | None |
| [v6.8](https://raw.githubusercontent.com/torvalds/linux/v6.8/include/uapi/linux/ublk_cmd.h) | 112 | same prefix | same | None |
| [v7.0](https://raw.githubusercontent.com/torvalds/linux/v7.0/include/uapi/linux/ublk_cmd.h) | 152 | same known prefix; appended fields | same | None |

`sizeof` includes C tail padding and later fields. Sending a prefix through the
last selected known field does not imply that the complete C struct is 108 bytes.
The earlier v6.0 control header has a different final field definition; the tested
common prefix with a zero tail does not establish legacy-command support.

The [v6.6 driver GET/SET_PARAMS implementation](https://github.com/torvalds/linux/blob/v6.6/drivers/block/ublk_drv.c#L2348)
reads the input length and copies the C structure, confirming both requirements.
The [v7.0 driver](https://raw.githubusercontent.com/torvalds/linux/v7.0/drivers/block/ublk_drv.c)
also retains the SET length in the copied GET response. Independent review caught
and corrected the first hardening revision's overly strict response-length check.
The v6.6 header defines encoded commands, so the earlier explanation that IOCTL
encoding itself requires 6.8 was too strong. Actual 6.6 ublk lifecycle, command
negotiation, and data I/O remain untested; no legacy fallback was added.

## Go execution and device coverage

| Environment | Evidence for this hardening | Actual kernel/device coverage |
| --- | --- | --- |
| macOS arm64 | UAPI fixtures and bounds tests; Linux amd64 test compilation and Linux arm64 build | None |
| WSL2 amd64, `6.6.87.2-microsoft-standard-WSL2`, Go 1.25.5 | Full userspace suite, race, vet, build; bounded UAPI fuzzing; integration binary compilation | None: `/dev/ublk-control` and loaded `ublk_drv` absent |
| Ubuntu arm64 `6.17.0-41-generic` | Historical results recorded in TODO.md at earlier revisions | Earlier integrity/lifecycle/crash suites passed; this change not rerun |
| Ubuntu x86_64 `6.17.0-1020-aws` | Historical results recorded in TODO.md at earlier revisions | Earlier integrity/lifecycle tests passed; this change not rerun |
| Ubuntu arm64 `7.0.0-30-generic` / `7.0.0-34-generic` | Historical suites / benchmark recorded in TODO.md / README.md | Earlier device tests; this change not rerun |

The historical rows are repository records, not new observations. The known
Ubuntu `6.17.0-1019-aws` ADD_DEV crash and generic backport caveats remain in TODO.md;
header matching does not repair those kernel bugs. There is no new evidence for
6.1/6.6 LTS device support, x86_64 7.0 device support, big-endian systems, zoned
operation, recovery, or newly appended parameter features.

The WSL service uses the approved task partition: actual affinity 8,10, aggregate
CPU quota 200%, memory maximum 4 GiB, nice 10, `GOMAXPROCS=2`, and test/fuzz
parallelism 2. Shared cache, power and memory effects remain; this is correctness
evidence, not a timing benchmark. The cpuset controller was not delegated to the
user service; explicit `taskset` plus `/proc` affinity verification enforced the
approved CPUs without changing global settings. An initial runner failure before
tests and the corrected synthetic-boundary vet failure are preserved in evidence.

## Repeatable userspace checks

```sh
GOMAXPROCS=2 go test -p=2 -parallel=2 -count=1 -timeout=90s ./...
GOMAXPROCS=2 go test -race -p=2 -parallel=2 -count=1 -timeout=90s ./...
go vet ./...
go build ./...
make test-uapi-fuzz FUZZ_TIME=90s FUZZ_PARALLEL=2
make test-large-io-kernel-compile  # compilation only
```

Both fuzz targets cap input and destination buffers at 512 bytes, cover negative
I/O results and all known parameter masks, and verify atomic errors, bounds,
unchanged inputs, fixed block placement, canonical round trips, and canary tails.
Default CI uses 15 seconds per target; normal fuzz seeds also run in `go test`.

To regenerate C fixtures against a chosen downloaded header (the include root must
contain `linux/ublk_cmd.h`):

```sh
cc -Wall -Werror -I/path/to/pinned/include scripts/uapi-fixtures.c -o /tmp/uapi-fixtures
/tmp/uapi-fixtures
```

## Remaining real-kernel acceptance

Acceptance needs separately authorized disposable Linux guests for the exact
kernel/architecture cells, existing `CONFIG_BLK_DEV_UBLK` and io_uring support,
a working `/dev/ublk-control`, and permission to create only throwaway RAM/file-backed
ublk devices. Installing kernels/modules or changing device permissions is not
part of this checkpoint. No live block device or data was used here.

For each admitted guest, record kernel/build/module identity, architecture, Go
version and source commit. Verify ADD_DEV → SET_PARAMS → GET_PARAMS → START_DEV
with BASIC-only and BASIC|DISCARD backends, devt major/minor values against the
created character/block device nodes, and query after start. Then run the guarded
large-I/O public-runner test, integrity verification, and graceful busy teardown;
record both successful and failing results and check that no test devices remain.
The current integration binary compilation cannot substitute for these steps.
The guarded large-I/O test requires `GO_UBLK_DISPOSABLE_TEST=1` in an authorized
disposable guest. It was not executed in this session.
