# Evidence: lifecycle/restart-after-stop

Matrix run m2 (go-ublk 595536b, TCG): 31 kernels with ublk-suite still
running `lifecycle/restart-after-stop` (Start on a device after Stop). The
run was stopped early once the test proved harmful. These rows are kept as
evidence and are not the current results; those are in `../matrix.json`,
which comes from run m3 with the test skipped.

What the test did:

- Oopsed Arch 7.2.8: NULL dereference in `ublk_queue_rq`. The trace is in
  `../oops-arch-7.2.8-ublk_queue_rq.txt`.
- On mainline 6.4-6.15 and Debian 6.12 (12-backports, 13), the second
  START_DEV timed out after 10s, and tasks then hung. Most of these guests
  could not power off. Mainline 6.4 also logged
  `refcount_t: underflow; use-after-free` from `iou-wrk`. On mainline 6.16 the
  test hung outright.
- On mainline 6.17.12 and 6.18.55, Arch LTS 6.18.55 and Fedora 6.19.14 / 7.2.8,
  the second Start failed with EBUSY opening `/dev/ublkc0`.
- Every `lifecycle/chaos` failure here followed this test, so those are
  collateral.
- Mainline 6.19.14 had already been wedged, earlier in the run, by
  `lifecycle/ctx-cancel-idle`; see `../targeted`.

go-ublk will refuse Start after Stop, and the suite test will assert that.
