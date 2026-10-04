# go-ublk kernel matrix

Generated 2026-10-04T18:19:05+00:00 against go-ublk `4d9712f0f26e`. 67 kernels: 4 fail, 2 fetch-failed, 14 no-ublk, 47 pass.

Rows by the commit they ran: `0aa26d9` 13, `4d9712f0f26e` 52, `?` 2.

| Kernel id | Distro | uname -r | Base | ublk_drv | Features | Status | P/F/S/E | Oops | Wall | Failing tests |
|---|---|---|---|---|---|---|---|---|---|---|
| mainline-6.0.19 | Ubuntu mainline build | 6.0.19-060019-generic | 6.0 | no | - | **no-ublk** | 8/1/5/0 |  | 52s | unit/internal_uring |
| mainline-6.1.189 | Ubuntu mainline build | 6.1.189-0601189-generic | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 54s |  |
| mainline-6.2 | Ubuntu mainline build | 6.2.0-060200-generic | 6.2 | no | - | **no-ublk** | 9/0/5/0 |  | 63s |  |
| mainline-6.3 | Ubuntu mainline build | 6.3.0-060300-generic | 6.3 | no | - | **no-ublk** | 9/0/5/0 |  | 57s |  |
| mainline-6.4 | Ubuntu mainline build | 6.4.0-060400-generic | 6.4 | yes | - | **pass** | 50/0/25/0 |  | 227s |  |
| mainline-6.5.11 | Ubuntu mainline build | 6.5.11-060511-generic | 6.5 | yes | 0xfe | **pass** | 56/0/19/0 |  | 256s |  |
| mainline-6.6.158 | Ubuntu mainline build | 6.6.158-0606158-generic | 6.6 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 251s |  |
| mainline-6.7.10 | Ubuntu mainline build | 6.7.10-060710-generic | 6.7 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 244s |  |
| mainline-6.8.12 | Ubuntu mainline build | 6.8.12-060812-generic | 6.8 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 244s |  |
| mainline-6.9.12 | Ubuntu mainline build | 6.9.12-060912-generic | 6.9 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 243s |  |
| mainline-6.10.14 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 247s |  |
| mainline-6.11.11 | Ubuntu mainline build | 6.11.11-061111-generic | 6.11 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 243s |  |
| mainline-6.12.112 | Ubuntu mainline build | 6.12.112-0612112-generic | 6.12 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 237s |  |
| mainline-6.13.12 | Ubuntu mainline build | 6.13.12-061312-generic | 6.13 | yes | 0x3fe | **pass** | 59/0/16/0 |  | 252s |  |
| mainline-6.14.11 | Ubuntu mainline build | 6.14.11-061411-generic | 6.14 | yes | 0x3fe | **pass** | 59/0/16/0 |  | 252s |  |
| mainline-6.15.11 | Ubuntu mainline build | 6.15.11-061511-generic | 6.15 | yes | 0x3ff | **pass** | 61/0/14/0 |  | 245s |  |
| mainline-6.16.12 | Ubuntu mainline build | 6.16.12-061612-generic | 6.16 | yes | 0x3fff | **pass** | 63/0/12/0 |  | 242s |  |
| mainline-6.17.12 | Ubuntu mainline build | 6.17.12-061712-generic | 6.17 | yes | 0x7fff | **pass** | 63/0/13/0 |  | 248s |  |
| mainline-6.18.5 | Ubuntu mainline build | 6.18.5 | 6.18 | no | - | **fetch-failed** | 0/0/0/1 |  |  | fetch |
| mainline-6.18.55 | Ubuntu mainline build | 6.18.55-061855-generic | 6.18 | yes | 0x7fff | **pass** | 63/0/13/0 |  | 248s |  |
| mainline-6.19.14 | Ubuntu mainline build | 6.19.14-061914-generic | 6.19 | yes | 0x7fff | **pass** | 63/0/13/0 |  | 249s |  |
| mainline-7.0.14 | Ubuntu mainline build | 7.0.14-070014-generic | 7.0 | yes | 0x7ffff | **pass** | 73/0/3/0 |  | 257s |  |
| mainline-7.1.13 | Ubuntu mainline build | 7.1.13-070113-generic | 7.1 | yes | 0xfffff | **pass** | 74/0/2/0 |  | 338s |  |
| mainline-7.2.6 | Ubuntu mainline build | 7.2.6-070206-generic | 7.2 | yes | 0xfffff | **pass** | 74/0/2/0 |  | 265s |  |
| mainline-7.3-rc3 | Ubuntu mainline build | 7.3.0-070300rc3-generic | 7.3 | yes | 0x1fffff | **pass** | 75/0/0/0 |  | 259s |  |
| amazon-2023-6.1 | Amazon Linux 2023 (6.1) | 6.1.186-228.376.amzn2023.x86_64 | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 106s |  |
| amazon-2023-6.12 | Amazon Linux 2023 (6.12) | 6.12.103-129.197.amzn2023.x86_64 | 6.12 | no | - | **no-ublk** | 9/0/5/0 |  | 18s |  |
| arch-lts | Arch Linux LTS | 6.18.55-1-lts | 6.18 | yes | 0x7fff | **pass** | 63/0/12/0 |  | 247s |  |
| arch | Arch Linux | 7.2.8-arch1-2 | 7.2 | yes | 0xfffff | **pass** | 74/0/1/0 |  | 263s |  |
| debian-12 | Debian 12 | 6.1.0-53-amd64 | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 51s |  |
| debian-12-backports | Debian 12 backports | 6.12.95+deb12-amd64 | 6.12 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 223s |  |
| debian-13 | Debian 13 | 6.12.111+deb13-amd64 | 6.12 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 225s |  |
| debian-13-backports | Debian 13 backports | 7.1.13+deb13-amd64 | 7.1 | no | - | **fetch-failed** | 0/0/0/1 |  |  | fetch |
| fedora-42 | Fedora 42 | 6.19.14-108.fc42.x86_64 | 6.19 | yes | 0x7fff | **pass** | 63/0/13/0 |  | 259s |  |
| fedora-43 | Fedora 43 | 7.2.8-100.fc43.x86_64 | 7.2 | yes | 0xfffff | **pass** | 74/0/2/0 |  | 276s |  |
| fedora-44 | Fedora 44 | 7.2.8-200.fc44.x86_64 | 7.2 | yes | 0xfffff | **pass** | 74/0/2/0 |  | 273s |  |
| oracle-9-uek7 | Oracle Linux 9 UEK7 | 5.15.0-324.217.5.3.el9uek.x86_64 | 5.15 | no | - | **no-ublk** | 7/2/5/0 |  | 18s | unit/internal_uring, unit/test_unit |
| oracle-9-uek8 | Oracle Linux 9 UEK8 | 6.12.0-206.104.4.4.el9uek.x86_64 | 6.12 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 226s |  |
| almalinux-9 | AlmaLinux 9 | 5.14.0-687.53.1.el9_8.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 18s | unit/internal_uring, unit/test_unit |
| centos-stream-9 | CentOS Stream 9 | 5.14.0-754.el9.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 18s | unit/internal_uring, unit/test_unit |
| rocky-9 | Rocky Linux 9 | 5.14.0-687.54.1.el9_8.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 18s | unit/internal_uring, unit/test_unit |
| almalinux-10 | AlmaLinux 10 | 6.12.0-211.56.1.el10_2.x86_64 | 6.12 | yes | - | **fail** | 7/68/0/0 |  | 53s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| almalinux-10+io_uring | AlmaLinux 10 [sysctl.kernel.io_uring_disabled=0] | 6.12.0-211.56.1.el10_2.x86_64 | 6.12 | yes | 0x7fff | **pass** | 63/0/12/0 |  | 226s |  |
| centos-stream-10 | CentOS Stream 10 | 6.12.0-271.el10.x86_64 | 6.12 | yes | - | **fail** | 7/68/0/0 |  | 51s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| centos-stream-10+io_uring | CentOS Stream 10 [sysctl.kernel.io_uring_disabled=0] | 6.12.0-271.el10.x86_64 | 6.12 | yes | 0x7fff | **pass** | 63/0/12/0 |  | 230s |  |
| rocky-10 | Rocky Linux 10 | 6.12.0-211.61.1.el10_2.x86_64 | 6.12 | yes | - | **fail** | 7/68/0/0 |  | 59s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| rocky-10+io_uring | Rocky Linux 10 [sysctl.kernel.io_uring_disabled=0] | 6.12.0-211.61.1.el10_2.x86_64 | 6.12 | yes | 0x7fff | **pass** | 63/0/12/0 |  | 230s |  |
| opensuse-leap-15.6 | openSUSE Leap 15.6 | 6.4.0-150600.23.103-default | 6.4 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 233s |  |
| opensuse-leap-16.0 | openSUSE Leap 16.0 | 6.12.0-160000.38-default | 6.12 | no | - | **no-ublk** | 9/0/5/0 |  | 17s |  |
| opensuse-tumbleweed | openSUSE Tumbleweed | 7.2.8-1-default | 7.2 | yes | 0xfffff | **pass** | 74/0/1/0 |  | 251s |  |
| ubuntu-22.04-ga | Ubuntu 22.04 GA | 5.15.0-198-generic | 5.15 | no | - | **no-ublk** | 7/2/5/0 |  | 18s | unit/internal_uring, unit/test_unit |
| full-ubuntu-24.04 | Ubuntu 24.04 cloud image | 6.8.0-142-generic | 6.8 | no | - | **no-ublk** | 9/0/5/0 |  | 94s |  |
| full-ubuntu-24.04-extra | Ubuntu 24.04 cloud image + linux-modules-extra | 6.8.0-142-generic | 6.8 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 565s |  |
| ubuntu-22.04-hwe | Ubuntu 22.04 HWE | 6.8.0-138-generic | 6.8 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 234s |  |
| ubuntu-24.04-ga | Ubuntu 24.04 GA | 6.8.0-146-generic | 6.8 | yes | 0x1fe | **pass** | 57/0/18/0 |  | 237s |  |
| ubuntu-24.04-hwe-6.11 | Ubuntu 24.04 HWE 6.11 | 6.11.0-29-generic | 6.11 | yes | 0x1fe | **pass** | 58/0/17/0 |  | 239s |  |
| ubuntu-24.04-hwe-6.14 | Ubuntu 24.04 HWE 6.14 | 6.14.0-37-generic | 6.14 | yes | 0x3fe | **pass** | 59/0/16/0 |  | 240s |  |
| ubuntu-25.04 | Ubuntu 25.04 | 6.14.0-37-generic | 6.14 | yes | 0x3fe | **pass** | 59/0/16/0 |  | 243s |  |
| ubuntu-24.04-hwe-6.17 | Ubuntu 24.04 HWE 6.17 | 6.17.0-42-generic | 6.17 | yes | 0x7fff | **pass** | 63/0/12/0 |  | 244s |  |
| ubuntu-24.04-hwe-6.17.0-40 | Ubuntu 24.04 HWE 6.17.0-40 (known-bad control) | 6.17.0-40-generic | 6.17 | yes | 0x7fff | **fail** | 10/1/0/2 | YES | 815s | integration/large-io, verify-sweep, kernel-log |
| ubuntu-25.10 | Ubuntu 25.10 | 6.17.0-41-generic | 6.17 | yes | 0x7fff | **pass** | 63/0/13/0 |  | 240s |  |
| ubuntu-24.04-aws | Ubuntu 24.04 linux-aws | 7.0.0-1014-aws | 7.0 | yes | 0x7ffff | **pass** | 73/0/2/0 |  | 263s |  |
| ubuntu-24.04-azure | Ubuntu 24.04 linux-azure | 7.0.0-1017-azure | 7.0 | yes | 0x7ffff | **pass** | 73/0/2/0 |  | 259s |  |
| ubuntu-24.04-gcp | Ubuntu 24.04 linux-gcp | 7.0.0-1014-gcp | 7.0 | yes | 0x7ffff | **pass** | 73/0/2/0 |  | 257s |  |
| ubuntu-24.04-hwe | Ubuntu 24.04 HWE | 7.0.0-38-generic | 7.0 | yes | 0x7ffff | **pass** | 73/0/2/0 |  | 265s |  |
| ubuntu-24.04-hwe-7.0.0-38 | Ubuntu 24.04 HWE 7.0.0-38 (Slide-tested) | 7.0.0-38-generic | 7.0 | yes | 0x7ffff | **pass** | 73/0/2/0 |  | 259s |  |
| ubuntu-26.04 | Ubuntu 26.04 | 7.0.0-38-generic | 7.0 | yes | 0x7ffff | **pass** | 73/0/3/0 |  | 238s |  |

## Details for runs that did not pass

### mainline-6.0.19 (6.0.19-060019-generic): no-ublk
- `unit/internal_uring` fail: 56 pass, 1 fail, 0 skip; failed: TestDeferTaskrunSingleIssuer     core_test.go:117: features 0x1fff     core_test.go:154: last opcode 47     core_test.go:490: NewIoUring({Entries:8 CQEntries:0 Flags:12288 OptionalFlags:512}): io_uring_setup entries=8 flags=0x3000: invalid argument 

### mainline-6.1.189 (6.1.189-0601189-generic): no-ublk

### mainline-6.2 (6.2.0-060200-generic): no-ublk

### mainline-6.3 (6.3.0-060300-generic): no-ublk

### mainline-6.18.5 (6.18.5): fetch-failed
- `fetch` error: v6.18.5: amd64 build failed

### amazon-2023-6.1 (6.1.186-228.376.amzn2023.x86_64): no-ublk

### amazon-2023-6.12 (6.12.103-129.197.amzn2023.x86_64): no-ublk

### debian-12 (6.1.0-53-amd64): no-ublk

### debian-13-backports (7.1.13+deb13-amd64): fetch-failed
- `fetch` error: extract failed

### oracle-9-uek7 (5.15.0-324.217.5.3.el9uek.x86_64): no-ublk
- `unit/internal_uring` fail: 30 pass, 23 fail, 4 skip; failed: TestHotPathDoesNotAllocate TestSetupDropsRejectedOptionalFlags TestProbe TestNopRoundTrips TestMsgRingWakesAnotherRing TestDeferTaskrunSingleIssuer TestRingCloseReleasesMappingsAndFds TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSparseBufferTable TestFixedFil
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: invalid argument 

### almalinux-9 (5.14.0-687.53.1.el9_8.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### centos-stream-9 (5.14.0-754.el9.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### rocky-9 (5.14.0-687.54.1.el9_8.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:104: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 

### almalinux-10 (6.12.0-211.56.1.el10_2.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"5799E24E6A58591A9F62173","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted","library":"error: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:133: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kern
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/concurrent-create` fail: worker 3 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/chaos` fail: server create: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/list-high-id` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/delete-async` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/probe` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-inline` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-need-get-data` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-threads-per-queue` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-batch-io` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-batch-io-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/batch-io-zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/batch-io-close-under-load` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zero-copy-close-under-load` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/handler-async` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/fua` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/tag-find` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/resize` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/no-partition-scan` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/safe-stop` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/ctx-cancel-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/queue-mode` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/fail-io-mode` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/batch-kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/batch-detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/integrity-kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zoned` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/shared-memory` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/io-desc-size` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/unprivileged` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)

### centos-stream-10 (6.12.0-271.el10.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"A72A5ED01780D367B935BB1","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted","library":"error: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:133: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kern
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/concurrent-create` fail: worker 3 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/chaos` fail: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/list-high-id` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/delete-async` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/probe` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-inline` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-need-get-data` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-threads-per-queue` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-batch-io` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-batch-io-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/batch-io-zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/batch-io-close-under-load` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zero-copy-close-under-load` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/handler-async` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/fua` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/tag-find` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/resize` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/no-partition-scan` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/safe-stop` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/ctx-cancel-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/queue-mode` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/fail-io-mode` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/batch-kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/batch-detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/integrity-kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zoned` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/shared-memory` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/io-desc-size` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/unprivileged` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)

### rocky-10 (6.12.0-211.61.1.el10_2.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"5799E24E6A58591A9F62173","error":"io_uring setup: io_uring_setup entries=4 flags=0xc00: operation not permitted","library":"error: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)"}
- `unit/internal_uring` fail: 18 pass, 12 fail, 27 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     bench_test.go:18: io_uring unavailable: io_uring_setup entries=8 flags=0x0: operation not permitted     core_test.go:133: io_
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kern
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=1) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=64) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=1 d=128) FAIL: device did not appear (q=2 d=1) FAIL: device did not appear (q=2 d=1) 
- `loop-e2e` fail: ; 
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/concurrent-create` fail: worker 0 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/chaos` fail: create: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/list-high-id` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/delete-async` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/probe` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-inline` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-need-get-data` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-threads-per-queue` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-batch-io` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity-batch-io-user-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/batch-io-zero-copy` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/batch-io-close-under-load` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zero-copy-close-under-load` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/handler-async` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/fua` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/tag-find` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/resize` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/no-partition-scan` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/safe-stop` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `lifecycle/ctx-cancel-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/queue-mode` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/fail-io-mode` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/batch-kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/batch-detach-handoff` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `recovery/integrity-kill-and-recover` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/zoned` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/integrity` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/shared-memory` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/io-desc-size` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)
- `features/unprivileged` fail: failed to create io_uring: io_uring_setup entries=4 flags=0xc00: operation not permitted (io_uring is disabled: set sysctl kernel.io_uring_disabled=0, or 1 with this process in kernel.io_uring_group)

### opensuse-leap-16.0 (6.12.0-160000.38-default): no-ublk

### ubuntu-22.04-ga (5.15.0-198-generic): no-ublk
- `unit/internal_uring` fail: 30 pass, 23 fail, 4 skip; failed: TestHotPathDoesNotAllocate TestSetupDropsRejectedOptionalFlags TestProbe TestNopRoundTrips TestMsgRingWakesAnotherRing TestDeferTaskrunSingleIssuer TestRingCloseReleasesMappingsAndFds TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestSubmitCtrlCmdStagesCallerBuffer TestCtrlTimeoutAndStragglerCQE TestCtrlContextCancelReapsCommand TestCtrlContextCancelGraceExpires TestCloseDuringUnboundedCtrlWait TestCloseWhileWaitForCompletionParked TestRealRingOpenCloseNoRegistration TestSparseBufferTable TestFixedFil
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup entries=32 flags=0xc00: invalid argument 

### full-ubuntu-24.04 (6.8.0-142-generic): no-ublk

### ubuntu-24.04-hwe-6.17.0-40 (6.17.0-40-generic): fail
- `integration/large-io` timeout: 0 pass, 0 fail, 0 skip; killed after 300s; last:  /usr/local/go/src/testing/testing.go:1934 +0xea created by testing.(*T).Run in goroutine 19  /usr/local/go/src/testing/testing.go:1997 +0x465 
- `verify-sweep` error: guest died (qemu rc=0)
- `kernel-log` fail:
  ```
  6 oops/hang line(s); first: [   18.002829] BUG: kernel NULL pointer dereference, address: 0000000000000000
  [   18.005440] #PF: supervisor read access in kernel mode
  [   18.005538] #PF: error_code(0x0000) - not-present page
  [   18.005644] PGD 537a067 P4D 5297067 PUD 5393067 PMD 0 
  [   18.006348] Oops: Oops: 0000 [#1] SMP NOPTI
  [   18.006620] CPU: 1 UID: 0 PID: 416 Comm: iou-wrk-413 Not tainted 6.17.0-40-generic #40~24.04.1-Ubuntu PREEMPT(voluntary) 
  [   18.006982] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [   18.007337] RIP: 0010:ublk_init_queues+0x4e/0x1f0 [ublk_drv]
  [   18.008109] Code: 48 c7 c0 40 bc 7c 9a 31 db 48 89 45 d0 45 0f b7 6f 0a 8b 35 c4 cb e7 d9 31 c0 45 8d 65 01 41 c1 e4 06 eb 12 49 8b 4f 50 89 c2 <3b> 1c 91 0f 84 25 01 00 00 83 c0 01 89 c2 48 c7 c7 60 b5 63 99 e8
  [   18.008450] RSP: 0018:ff5b0310806a3bd0 EFLAGS: 00000293
  [   18.008536] RAX: 0000000000000000 RBX: 0000000000000000 RCX: 0000000000000000
  [   18.008633] RDX: 0000000000000000 RSI: 0000000000000002 RDI: 0000000000000000
  [   18.008721] RBP: ff5b0310806a3c08 R08: 0000000000000000 R09: 0000000000000000
  [   18.008995] R10: 0000000000000000 R11: 0000000000000000 R12:
  ```

