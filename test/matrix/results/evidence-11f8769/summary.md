# go-ublk kernel matrix

Generated 2026-10-04T15:39:24+00:00 against go-ublk `11f8769`. 26 kernels: 6 fail, 7 no-ublk, 13 pass.

| Kernel id | Distro | uname -r | Base | ublk_drv | Features | Status | P/F/S/E | Oops | Wall | Failing tests |
|---|---|---|---|---|---|---|---|---|---|---|
| mainline-6.0.19 | Ubuntu mainline build | 6.0.19-060019-generic | 6.0 | no | - | **no-ublk** | 8/1/5/0 |  | 52s | unit/internal_uring |
| mainline-6.1.189 | Ubuntu mainline build | 6.1.189-0601189-generic | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 52s |  |
| mainline-6.10.14 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | 0x1fe | **fail** | 53/1/5/0 |  | 230s | io/write-zeroes-large |
| mainline-6.11.11 | Ubuntu mainline build | 6.11.11-061111-generic | 6.11 | yes | 0x1fe | **pass** | 54/0/5/0 |  | 225s |  |
| mainline-6.12.112 | Ubuntu mainline build | 6.12.112-0612112-generic | 6.12 | yes | 0x1fe | **pass** | 54/0/5/0 |  | 222s |  |
| mainline-6.13.12 | Ubuntu mainline build | 6.13.12-061312-generic | 6.13 | yes | 0x3fe | **pass** | 54/0/5/0 |  | 232s |  |
| mainline-6.14.11 | Ubuntu mainline build | 6.14.11-061411-generic | 6.14 | yes | 0x3fe | **pass** | 54/0/5/0 |  | 223s |  |
| mainline-6.15.11 | Ubuntu mainline build | 6.15.11-061511-generic | 6.15 | yes | 0x3ff | **pass** | 55/0/4/0 |  | 225s |  |
| mainline-6.16.12 | Ubuntu mainline build | 6.16.12-061612-generic | 6.16 | yes | 0x3fff | **pass** | 57/0/2/0 |  | 225s |  |
| amazon-2023-6.1 | Amazon Linux 2023 (6.1) | 6.1.186-228.376.amzn2023.x86_64 | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 104s |  |
| amazon-2023-6.12 | Amazon Linux 2023 (6.12) | 6.12.103-129.197.amzn2023.x86_64 | 6.12 | no | - | **no-ublk** | 9/0/5/0 |  | 17s |  |
| arch-lts | Arch Linux LTS | 6.18.55-1-lts | 6.18 | yes | 0x7fff | **pass** | 57/0/2/0 |  | 213s |  |
| arch | Arch Linux | 7.2.8-arch1-2 | 7.2 | yes | 0xfffff | **pass** | 59/0/0/0 |  | 222s |  |
| debian-12 | Debian 12 | 6.1.0-53-amd64 | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 53s |  |
| debian-12-backports | Debian 12 backports | 6.12.95+deb12-amd64 | 6.12 | yes | 0x1fe | **pass** | 54/0/5/0 |  | 215s |  |
| debian-13 | Debian 13 | 6.12.111+deb13-amd64 | 6.12 | yes | 0x1fe | **pass** | 54/0/5/0 |  | 213s |  |
| fedora-42 | Fedora 42 | 6.19.14-108.fc42.x86_64 | 6.19 | yes | 0x7fff | **fail** | 57/1/2/0 | YES | 223s | kernel-log |
| fedora-43 | Fedora 43 | 7.2.8-100.fc43.x86_64 | 7.2 | yes | 0xfffff | **fail** | 59/1/0/0 | YES | 229s | kernel-log |
| fedora-44 | Fedora 44 | 7.2.8-200.fc44.x86_64 | 7.2 | yes | 0xfffff | **fail** | 59/1/0/0 | YES | 231s | kernel-log |
| almalinux-9 | AlmaLinux 9 | 5.14.0-687.53.1.el9_8.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 18s | unit/internal_uring, unit/test_unit |
| centos-stream-9 | CentOS Stream 9 | 5.14.0-754.el9.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 17s | unit/internal_uring, unit/test_unit |
| almalinux-10 | AlmaLinux 10 | 6.12.0-211.56.1.el10_2.x86_64 | 6.12 | yes | unknown | **fail** | 7/52/0/0 |  | 52s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| almalinux-10+io_uring | AlmaLinux 10 [sysctl.kernel.io_uring_disabled=0] | 6.12.0-211.56.1.el10_2.x86_64 | 6.12 | yes | 0x7fff | **pass** | 57/0/2/0 |  | 213s |  |
| centos-stream-10 | CentOS Stream 10 | 6.12.0-271.el10.x86_64 | 6.12 | yes | unknown | **fail** | 7/52/0/0 |  | 54s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| centos-stream-10+io_uring | CentOS Stream 10 [sysctl.kernel.io_uring_disabled=0] | 6.12.0-271.el10.x86_64 | 6.12 | yes | 0x7fff | **pass** | 57/0/2/0 |  | 215s |  |
| rocky-10+io_uring | Rocky Linux 10 [sysctl.kernel.io_uring_disabled=0] | 6.12.0-211.61.1.el10_2.x86_64 | 6.12 | yes | 0x7fff | **pass** | 57/0/2/0 |  | 213s |  |

## Details for runs that did not pass

### mainline-6.0.19 (6.0.19-060019-generic): no-ublk
- `unit/internal_uring` fail: 56 pass, 1 fail, 0 skip; failed: TestDeferTaskrunSingleIssuer     core_test.go:117: features 0x1fff     core_test.go:154: last opcode 47     core_test.go:490: NewIoUring({Entries:8 CQEntries:0 Flags:12288 OptionalFlags:512}): io_uring_setup entries=8 flags=0x3000: invalid argument 

### mainline-6.1.189 (6.1.189-0601189-generic): no-ublk

### mainline-6.10.14 (6.10.14-061014-generic): fail
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120

### amazon-2023-6.1 (6.1.186-228.376.amzn2023.x86_64): no-ublk

### amazon-2023-6.12 (6.12.103-129.197.amzn2023.x86_64): no-ublk

### debian-12 (6.1.0-53-amd64): no-ublk

### fedora-42 (6.19.14-108.fc42.x86_64): fail
- `kernel-log` fail:
  ```
  2 oops/hang line(s); first: [  218.880890] UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:992:12
  [  218.881009] index 0 is out of range for type 'bio_vec [*]'
  [  218.881621] CPU: 1 UID: 0 PID: 2954 Comm: ublk-suite Not tainted 6.19.14-108.fc42.x86_64 #1 PREEMPT(lazy) 
  [  218.881773] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  218.881898] Call Trace:
  [  218.882067]  <TASK>
  [  218.882134]  dump_stack_lvl+0x5d/0x80
  [  218.882726]  ubsan_epilogue+0x5/0x2b
  [  218.882784]  __ubsan_handle_out_of_bounds.cold+0x54/0x59
  [  218.882794]  io_buffer_register_bvec+0x25a/0x2b0
  [  218.882837]  ublk_dispatch_req+0x177/0x240 [ublk_drv]
  [  218.882920]  ublk_cmd_list_tw_cb+0x2d/0x40 [ublk_drv]
  [  218.882929]  __io_run_local_work_loop+0x7c/0x80
  [  218.882938]  __io_run_local_work+0x14f/0x220
  [  218.882947]  io_cqring_wait+0x28e/0x740
  [  218.882955]  ? __pfx_io_wake_function+0x10/0x10
  [  218.882966]  ? __pfx_io_cqring_timer_wakeup+0x10/0x10
  [  218.882973]  __do_sys_io_uring_enter+0x136/0x440
  [  218.882982]  do_syscall_64+0x7e/0x690
  [  218.883038]  ? do_syscall_64+0xbb/0x690
  [  218.883046]  ? ida_alloc_range+0x406/0x480
  [  218.883056]  ? do_eventfd+0xf3/
  ```

### fedora-43 (7.2.8-100.fc43.x86_64): fail
- `kernel-log` fail:
  ```
  2 oops/hang line(s); first: [  225.247866] UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:1070:12
  [  225.248123] index 0 is out of range for type 'bio_vec [*]'
  [  225.248604] CPU: 1 UID: 0 PID: 2975 Comm: ublk-suite Not tainted 7.2.8-100.fc43.x86_64 #1 PREEMPT(lazy) 
  [  225.248663] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  225.248663] Call Trace:
  [  225.248663]  <TASK>
  [  225.248663]  dump_stack_lvl+0x5d/0x80
  [  225.248663]  ubsan_epilogue+0x5/0x2b
  [  225.248663]  __ubsan_handle_out_of_bounds.cold+0x54/0x59
  [  225.248663]  io_buffer_register_bvec+0x252/0x2c0
  [  225.248663]  ublk_dispatch_req+0x163/0x230 [ublk_drv]
  [  225.248663]  __io_run_local_work+0x261/0x280
  [  225.248663]  io_run_local_work+0x31/0x50
  [  225.248663]  io_cqring_wait+0x28f/0x6b0
  [  225.248663]  ? __pfx_io_wake_function+0x10/0x10
  [  225.248663]  ? __pfx_io_cqring_timer_wakeup+0x10/0x10
  [  225.248663]  __do_sys_io_uring_enter+0x2bb/0x360
  [  225.248663]  do_syscall_64+0xe2/0x570
  [  225.248663]  ? do_futex+0x10c/0x210
  [  225.248663]  ? __x64_sys_futex+0x12d/0x220
  [  225.248663]  ? __x64_sys_clock_gettime+0xa9/0x100
  [  225.248663]  ? do_syscall_64+0x11f/0x570
  [  225
  ```

### fedora-44 (7.2.8-200.fc44.x86_64): fail
- `kernel-log` fail:
  ```
  2 oops/hang line(s); first: features/zero-copy: features SUPPORT_ZERO_COPY|URING_CMD_COMP_IN_TASK|CMD_IOCTL_ENCODE|UPDATE_SIZE|AUTO_BUF_REG|PER_IO_DAEMON|BUF_REG_OFF_DA[  225.234150] UBSAN: array-index-out-of-bounds in io_uring/rsrc.c:1070:12
  EMON|SAFE_STOP_DEV
  [  225.235177] index 0 is out of range for type 'bio_vec [*]'
  [  225.235572] CPU: 0 UID: 0 PID: 2920 Comm: ublk-suite Not tainted 7.2.8-200.fc44.x86_64 #1 PREEMPT(lazy) 
  [  225.235833] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  225.235958] Call Trace:
  [  225.235958]  <TASK>
  [  225.236805]  dump_stack_lvl+0x5d/0x80
  [  225.236863]  ubsan_epilogue+0x5/0x2b
  [  225.236863]  __ubsan_handle_out_of_bounds.cold+0x4e/0x58
  [  225.236863]  io_buffer_register_bvec+0x269/0x2e0
  [  225.236863]  ublk_dispatch_req+0x161/0x230 [ublk_drv]
  [  225.236863]  __io_run_local_work+0x258/0x280
  [  225.236863]  io_run_local_work+0x3e/0x70
  [  225.236863]  io_cqring_wait+0x2b5/0x6d0
  [  225.236863]  ? __pfx_io_wake_function+0x10/0x10
  [  225.236863]  ? __pfx_io_cqring_timer_wakeup+0x10/0x10
  [  225.236863]  __do_sys_io_uring_enter+0x2ee/0x3d0
  [  225.236863]  do_syscall_64+0xe2/0x570
  [  225.236863]  ? avc_has_per
  ```

### almalinux-9 (5.14.0-687.53.1.el9_8.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### centos-stream-9 (5.14.0-754.el9.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### almalinux-10 (6.12.0-211.56.1.el10_2.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"5799E24E6A58591A9F62173","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted","library":"error: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted 
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
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
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/chaos` fail: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/probe` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-inline` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-need-get-data` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-threads-per-queue` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/handler-async` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/fua` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/tag-find` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/resize` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/no-partition-scan` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/safe-stop` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/ctx-cancel-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `recovery/kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `recovery/detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/zoned` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted

### centos-stream-10 (6.12.0-271.el10.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"A72A5ED01780D367B935BB1","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted","library":"error: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted 
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
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
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/chaos` fail: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/probe` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-inline` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-need-get-data` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/integrity-threads-per-queue` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/handler-async` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/fua` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/tag-find` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/resize` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/no-partition-scan` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/safe-stop` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/ctx-cancel-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `recovery/kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `recovery/detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `features/zoned` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted

