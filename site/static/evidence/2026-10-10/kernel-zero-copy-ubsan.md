# UBSAN report during ublk zero-copy buffer registration

Recorded 2026-10-10. Public extract of the zero-copy observation and source
analysis. This records a sanitizer report and a likely cause; an upstream fixing
commit and stable backports haven't been confirmed.

## Signature

```text
UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:1070:12
index 0 is out of range for type 'bio_vec [*]'
 io_buffer_register_bvec+0x27e/0x2f0
 ublk_dispatch_req+0x18d/0x270 [ublk_drv]
 ublk_cmd_tw_cb+0x16/0x20 [ublk_drv]
 __io_run_local_work -> io_run_local_work -> io_cqring_wait -> io_uring_enter
```

## Observed kernels

| Kernel | Result |
|---|---|
| Ubuntu 26.04, 7.0.0-38 | UBSAN report observed |
| Mainline 7.2.6-070206 | UBSAN report observed |
| Fedora 7.2.9-200.fc44 | UBSAN report observed |
| Mainline 7.3.0-rc3 | Suite 62/62 pass, zero warnings |
| 6.8 and 6.12 | Zero-copy feature unavailable; reproduction not applicable |

The feature test still passed on the reporting kernels: no oops or data mismatch
was observed. A clean 7.3-rc3 run is an observation, rather than proof that every
later build is fixed.

## Reproduction

On a fresh boot with the ublk driver and bounds-sanitizer instrumentation, create
a zero-copy ublk device, use automatic buffer registration, and issue data I/O.
The observed trigger was go-ublk's feature test:

```sh
ublk-suite -run '^features/zero-copy$'
```

Fresh-boot mainline 7.2.6 reproduction was 1/1 for base go-ublk revision 4c529de
(v0.2.0 plus documentation) and 1/1 for the contemporaneous queue-engine changes.
The report therefore predates those changes. UBSAN reports once per boot per
site, so repeated observations require fresh boots.

## Source analysis

In 7.2.6, io_uring/rsrc.c:1070 writes `imu->bvec[nr_bvecs++] = bv;` inside
`rq_for_each_bvec`. The array is declared `bvec[] __counted_by(nr_bvecs)`, while
`imu->nr_bvecs = nr_bvecs` is assigned after the loop. A zero current count can
make the annotation check fire on the first write.

The object comes from a fixed-capacity buffer cache or an allocation sized with
`blk_rq_nr_phys_segments(rq)`. In 7.3-rc3, the refactored
`io_kernel_buffer_init(ctx, nr_bvecs, ...)` receives the count before the entries
are filled. This is consistent with the clean run on that kernel.

Moderate-confidence explanation: a counted_by ordering issue removed by the
7.3 refactor. The allocation-bound assumption still needs verification, as does
the exact upstream fixing commit. No unconditional memory-safety verdict follows
from these observations.
