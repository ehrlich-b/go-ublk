# go-ublk kernel matrix

Generated 2026-10-04T15:14:20+00:00 against go-ublk `615c9a7`. 17 kernels: 10 fail, 7 no-ublk.

| Kernel id | Distro | uname -r | Base | ublk_drv | Features | Status | P/F/S/E | Oops | Wall | Failing tests |
|---|---|---|---|---|---|---|---|---|---|---|
| mainline-6.0.19 | Ubuntu mainline build | 6.0.19-060019-generic | 6.0 | no | - | **no-ublk** | 8/1/6/0 |  | 376s | unit/internal_uring |
| mainline-6.1.189 | Ubuntu mainline build | 6.1.189-0601189-generic | 6.1 | no | - | **no-ublk** | 9/0/6/0 |  | 347s |  |
| mainline-6.10.14 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | 0x1fe | **fail** | 43/3/0/0 |  | 285s | ctrl/ADD_DEV, io/write-zeroes-large, lifecycle/chaos |
| amazon-2023-6.1 | Amazon Linux 2023 (6.1) | 6.1.186-228.376.amzn2023.x86_64 | 6.1 | no | - | **no-ublk** | 8/1/6/0 |  | 590s | unit/internal_uring |
| amazon-2023-6.12 | Amazon Linux 2023 (6.12) | 6.12.103-129.197.amzn2023.x86_64 | 6.12 | no | - | **no-ublk** | 9/0/6/0 |  | 22s |  |
| arch-lts | Arch Linux LTS | 6.18.55-1-lts | 6.18 | yes | 0x7fff | **fail** | 101/6/5/1 |  | 874s | loop-e2e, ctrl/SET_PARAMS, ctrl/TRY_STOP_DEV, ctrl/live:, ctrl/live:, lifecycle/chaos ... |
| arch | Arch Linux | 7.2.8-arch1-2 | 7.2 | yes | 0xfffff | **fail** | 112/1/1/1 |  | 880s | lifecycle/chaos, suite/hygiene |
| debian-12 | Debian 12 | 6.1.0-53-amd64 | 6.1 | no | - | **no-ublk** | 9/0/6/0 |  | 350s |  |
| debian-12-backports | Debian 12 backports | 6.12.95+deb12-amd64 | 6.12 | yes | 0x1fe | **fail** | 44/2/0/1 |  | 867s | ctrl/ADD_DEV, lifecycle/chaos, suite/hygiene |
| debian-13 | Debian 13 | 6.12.111+deb13-amd64 | 6.12 | yes | 0x1fe | **fail** | 44/2/0/1 |  | 876s | ctrl/ADD_DEV, lifecycle/chaos, suite/hygiene |
| fedora-42 | Fedora 42 | 6.19.14-108.fc42.x86_64 | 6.19 | yes | 0x7fff | **fail** | 102/5/5/1 |  | 888s | ctrl/SET_PARAMS, ctrl/TRY_STOP_DEV, ctrl/live:, ctrl/live:, lifecycle/chaos, suite/hygiene |
| fedora-43 | Fedora 43 | 7.2.8-100.fc43.x86_64 | 7.2 | yes | 0xfffff | **fail** | 112/1/1/1 |  | 885s | lifecycle/chaos, suite/hygiene |
| fedora-44 | Fedora 44 | 7.2.8-200.fc44.x86_64 | 7.2 | yes | 0xfffff | **fail** | 112/1/1/0 |  | 303s | lifecycle/chaos |
| almalinux-9 | AlmaLinux 9 | 5.14.0-687.53.1.el9_8.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/6/0 |  | 20s | unit/internal_uring, unit/test_unit |
| centos-stream-9 | CentOS Stream 9 | 5.14.0-754.el9.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/6/0 |  | 18s | unit/internal_uring, unit/test_unit |
| almalinux-10 | AlmaLinux 10 | 6.12.0-211.56.1.el10_2.x86_64 | 6.12 | yes | unknown | **fail** | 7/36/0/0 |  | 55s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| centos-stream-10 | CentOS Stream 10 | 6.12.0-271.el10.x86_64 | 6.12 | yes | unknown | **fail** | 7/36/0/0 |  | 57s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |

## Details for runs that did not pass

### mainline-6.0.19 (6.0.19-060019-generic): no-ublk
- `unit/internal_uring` fail: 56 pass, 1 fail, 0 skip; failed: TestDeferTaskrunSingleIssuer     core_test.go:117: features 0x1fff     core_test.go:154: last opcode 47     core_test.go:490: NewIoUring({Entries:8 CQEntries:0 Flags:12288 OptionalFlags:512}): io_uring_setup entries=8 flags=0x3000: invalid argument 

### mainline-6.1.189 (6.1.189-0601189-generic): no-ublk

### mainline-6.10.14 (6.10.14-061014-generic): fail
- `ctrl/ADD_DEV` fail: ublk ADD_DEV: kernel lacks ublk features UPDATE_SIZE|QUIESCE (requested USER_RECOVERY|UPDATE_SIZE|QUIESCE; GET_FEATURES reports URING_CMD_COMP_IN_TASK|NEED_GET_DATA|USER_RECOVERY|USER_RECOVERY_REISSUE|UNPRIVILEGED_DEV|CMD_IOCTL_ENCODE|USER_COPY|ZONED)
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120
- `lifecycle/chaos` fail: close /dev/ublkb1: hung for 60s

### amazon-2023-6.1 (6.1.186-228.376.amzn2023.x86_64): no-ublk
- `unit/internal_uring` fail: 44 pass, 0 fail, 0 skip; failed:     core_test.go:117: features 0x1fff     core_test.go:154: last opcode 48     leak_test.go:81: 5000 cycles: /proc/self/maps 27 -> 27 lines, /proc/self/fd 8 -> 8 

### amazon-2023-6.12 (6.12.103-129.197.amzn2023.x86_64): no-ublk

### arch-lts (6.18.55-1-lts): fail
- `loop-e2e` fail: PASS=13 FAIL=1;   FAIL: shadow oracle mismatch on compressed backend   PASS=13 FAIL=1 
- `ctrl/SET_PARAMS` fail: INTEGRITY w/o F_INTEGRITY got ublk SET_PARAMS dev 0: kernel ignored param types INTEGRITY (requested BASIC|DISCARD|DMA_ALIGN|SEGMENT|INTEGRITY), want invalid argument
- `ctrl/TRY_STOP_DEV` fail: before START_DEV      got ublk TRY_STOP_DEV dev 0: operation not supported, want no such device
- `ctrl/live:` fail: TRY_STOP_DEV while open      got ublk TRY_STOP_DEV dev 0: operation not supported, want device or resource busy
- `ctrl/live:` fail: TRY_STOP_DEV                 ublk TRY_STOP_DEV dev 0: operation not supported
- `lifecycle/chaos` timeout: no result after 10m0s
- `suite/hygiene` fail: leaked: /dev/ublkb2  reaper-stuck D-state: none

### arch (7.2.8-arch1-2): fail
- `lifecycle/chaos` timeout: no result after 10m0s
- `suite/hygiene` fail: leaked: /dev/ublkb3 /dev/ublkb6  reaper-stuck D-state: none

### debian-12 (6.1.0-53-amd64): no-ublk

### debian-12-backports (6.12.95+deb12-amd64): fail
- `ctrl/ADD_DEV` fail: ublk ADD_DEV: kernel lacks ublk features UPDATE_SIZE|QUIESCE (requested USER_RECOVERY|UPDATE_SIZE|QUIESCE; GET_FEATURES reports URING_CMD_COMP_IN_TASK|NEED_GET_DATA|USER_RECOVERY|USER_RECOVERY_REISSUE|UNPRIVILEGED_DEV|CMD_IOCTL_ENCODE|USER_COPY|ZONED)
- `lifecycle/chaos` timeout: no result after 10m0s
- `suite/hygiene` fail: leaked: /dev/ublkb3  reaper-stuck D-state: none

### debian-13 (6.12.111+deb13-amd64): fail
- `ctrl/ADD_DEV` fail: ublk ADD_DEV: kernel lacks ublk features UPDATE_SIZE|QUIESCE (requested USER_RECOVERY|UPDATE_SIZE|QUIESCE; GET_FEATURES reports URING_CMD_COMP_IN_TASK|NEED_GET_DATA|USER_RECOVERY|USER_RECOVERY_REISSUE|UNPRIVILEGED_DEV|CMD_IOCTL_ENCODE|USER_COPY|ZONED)
- `lifecycle/chaos` timeout: no result after 10m0s
- `suite/hygiene` fail: leaked: /dev/ublkb6  reaper-stuck D-state: none

### fedora-42 (6.19.14-108.fc42.x86_64): fail
- `ctrl/SET_PARAMS` fail: INTEGRITY w/o F_INTEGRITY got ublk SET_PARAMS dev 0: kernel ignored param types INTEGRITY (requested BASIC|DISCARD|DMA_ALIGN|SEGMENT|INTEGRITY), want invalid argument
- `ctrl/TRY_STOP_DEV` fail: before START_DEV      got ublk TRY_STOP_DEV dev 0: operation not supported, want no such device
- `ctrl/live:` fail: TRY_STOP_DEV while open      got ublk TRY_STOP_DEV dev 0: operation not supported, want device or resource busy
- `ctrl/live:` fail: TRY_STOP_DEV                 ublk TRY_STOP_DEV dev 0: operation not supported
- `lifecycle/chaos` timeout: no result after 10m0s
- `suite/hygiene` fail: leaked: /dev/ublkb4 /dev/ublkb5  reaper-stuck D-state: none

### fedora-43 (7.2.8-100.fc43.x86_64): fail
- `lifecycle/chaos` timeout: no result after 10m0s
- `suite/hygiene` fail: leaked: /dev/ublkb2  reaper-stuck D-state: none

### fedora-44 (7.2.8-200.fc44.x86_64): fail
- `lifecycle/chaos` fail: close /dev/ublkb0: hung for 60s

### almalinux-9 (5.14.0-687.53.1.el9_8.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### centos-stream-9 (5.14.0-754.el9.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### almalinux-10 (6.12.0-211.56.1.el10_2.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"5799E24E6A58591A9F62173","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted 
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
- `ctrl/open` fail: control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/concurrent-create` fail: worker 1 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/chaos` fail: list: open control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted

### centos-stream-10 (6.12.0-271.el10.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"A72A5ED01780D367B935BB1","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted 
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
- `ctrl/open` fail: control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/concurrent-create` fail: worker 3 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/chaos` fail: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted

