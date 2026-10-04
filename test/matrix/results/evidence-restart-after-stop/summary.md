# go-ublk kernel matrix

Generated 2026-10-04T14:39:55+00:00 against go-ublk `595536b`. 31 kernels: 22 fail, 9 no-ublk.

| Kernel id | Distro | uname -r | Base | ublk_drv | Features | Status | P/F/S/E | Oops | Wall | Failing tests |
|---|---|---|---|---|---|---|---|---|---|---|
| mainline-6.0.19 | Ubuntu mainline build | 6.0.19-060019-generic | 6.0 | no | - | **no-ublk** | 9/0/5/0 |  | 16s |  |
| mainline-6.1.189 | Ubuntu mainline build | 6.1.189-0601189-generic | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 16s |  |
| mainline-6.2 | Ubuntu mainline build | 6.2.0-060200-generic | 6.2 | no | - | **no-ublk** | 9/0/5/0 |  | 15s |  |
| mainline-6.3 | Ubuntu mainline build | 6.3.0-060300-generic | 6.3 | no | - | **no-ublk** | 9/0/5/0 |  | 15s |  |
| mainline-6.4 | Ubuntu mainline build | 6.4.0-060400-generic | 6.4 | yes | unknown | **fail** | 38/6/0/0 | YES | 252s | probe, io/write-zeroes-large, lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, kernel-log |
| mainline-6.5.11 | Ubuntu mainline build | 6.5.11-060511-generic | 6.5 | yes | 0xfe | **fail** | 39/7/0/0 | YES | 358s | io/write-zeroes-large, lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff ... |
| mainline-6.6.158 | Ubuntu mainline build | 6.6.158-0606158-generic | 6.6 | yes | 0x1fe | **fail** | 39/7/0/0 | YES | 360s | io/write-zeroes-large, lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff ... |
| mainline-6.10.14 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | 0x1fe | **fail** | 38/8/0/0 | YES | 362s | verify-sweep, io/write-zeroes-large, lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene ... |
| mainline-6.11.11 | Ubuntu mainline build | 6.11.11-061111-generic | 6.11 | yes | 0x1fe | **fail** | 40/6/0/0 | YES | 369s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| mainline-6.12.112 | Ubuntu mainline build | 6.12.112-0612112-generic | 6.12 | yes | 0x1fe | **fail** | 40/6/0/0 | YES | 363s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| mainline-6.13.12 | Ubuntu mainline build | 6.13.12-061312-generic | 6.13 | yes | 0x3fe | **fail** | 40/6/0/0 | YES | 484s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| mainline-6.14.11 | Ubuntu mainline build | 6.14.11-061411-generic | 6.14 | yes | 0x3fe | **fail** | 40/5/0/0 | YES | 461s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, kernel-log |
| mainline-6.15.11 | Ubuntu mainline build | 6.15.11-061511-generic | 6.15 | yes | 0x3ff | **fail** | 40/5/0/0 | YES | 248s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, kernel-log |
| mainline-6.16.12 | Ubuntu mainline build | 6.16.12-061612-generic | 6.16 | yes | 0x3fff | **fail** | 40/5/0/1 | YES | 413s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| mainline-6.17.12 | Ubuntu mainline build | 6.17.12-061712-generic | 6.17 | yes | 0x7fff | **fail** | 40/3/0/0 |  | 255s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos |
| mainline-6.18.55 | Ubuntu mainline build | 6.18.55-061855-generic | 6.18 | yes | 0x7fff | **fail** | 40/3/0/0 |  | 236s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos |
| mainline-6.19.14 | Ubuntu mainline build | 6.19.14-061914-generic | 6.19 | yes | 0x7fff | **fail** | 36/11/0/0 | YES | 434s | lifecycle/ctx-cancel-idle, lifecycle/churn-leaks, lifecycle/concurrent-create, lifecycle/close-under-load, lifecycle/server-killed, lifecycle/restart-after-stop ... |
| amazon-2023-6.1 | Amazon Linux 2023 (6.1) | 6.1.186-228.376.amzn2023.x86_64 | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 15s |  |
| amazon-2023-6.12 | Amazon Linux 2023 (6.12) | 6.12.103-129.197.amzn2023.x86_64 | 6.12 | no | - | **no-ublk** | 9/0/5/0 |  | 14s |  |
| arch-lts | Arch Linux LTS | 6.18.55-1-lts | 6.18 | yes | 0x7fff | **fail** | 40/3/0/0 |  | 254s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos |
| arch | Arch Linux | 7.2.8-arch1-2 | 7.2 | yes | 0xfffff | **fail** | 40/5/0/1 | YES | 415s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| debian-12 | Debian 12 | 6.1.0-53-amd64 | 6.1 | no | - | **no-ublk** | 9/0/5/0 |  | 17s |  |
| debian-12-backports | Debian 12 backports | 6.12.95+deb12-amd64 | 6.12 | yes | 0x1fe | **fail** | 40/6/0/0 | YES | 363s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| debian-13 | Debian 13 | 6.12.111+deb13-amd64 | 6.12 | yes | 0x1fe | **fail** | 40/6/0/0 | YES | 363s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos, suite/hygiene, poweroff, kernel-log |
| fedora-42 | Fedora 42 | 6.19.14-108.fc42.x86_64 | 6.19 | yes | 0x7fff | **fail** | 40/3/0/0 |  | 266s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos |
| fedora-43 | Fedora 43 | 7.2.8-100.fc43.x86_64 | 7.2 | yes | 0xfffff | **fail** | 40/3/0/0 |  | 244s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos |
| fedora-44 | Fedora 44 | 7.2.8-200.fc44.x86_64 | 7.2 | yes | 0xfffff | **fail** | 40/3/0/0 |  | 267s | lifecycle/churn-leaks, lifecycle/restart-after-stop, lifecycle/chaos |
| almalinux-9 | AlmaLinux 9 | 5.14.0-687.53.1.el9_8.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 15s | unit/internal_uring, unit/test_unit |
| centos-stream-9 | CentOS Stream 9 | 5.14.0-754.el9.x86_64 | 5.14 | no | - | **no-ublk** | 7/2/5/0 |  | 15s | unit/internal_uring, unit/test_unit |
| almalinux-10 | AlmaLinux 10 | 6.12.0-211.56.1.el10_2.x86_64 | 6.12 | yes | unknown | **fail** | 7/36/0/0 |  | 51s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |
| centos-stream-10 | CentOS Stream 10 | 6.12.0-271.el10.x86_64 | 6.12 | yes | unknown | **fail** | 7/36/0/0 |  | 50s | probe, unit/internal_uring, unit/test_unit, integration/large-io, verify-sweep, loop-e2e ... |

## Details for runs that did not pass

### mainline-6.0.19 (6.0.19-060019-generic): no-ublk

### mainline-6.1.189 (6.1.189-0601189-generic): no-ublk

### mainline-6.2 (6.2.0-060200-generic): no-ublk

### mainline-6.3 (6.3.0-060300-generic): no-ublk

### mainline-6.4 (6.4.0-060400-generic): fail
- `probe` fail: {"features":"unknown","srcversion":"784A74C9C421FA0CEFC07D1","error":"ioctl-encoded: no such device\nlegacy: operation not supported"}
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `kernel-log` fail:
  ```
  6 oops/hang line(s); first: [  254.103713] refcount_t: underflow; use-after-free.
  [  254.103976] WARNING: CPU: 1 PID: 2440 at lib/refcount.c:28 refcount_warn_saturate+0xa3/0x150
  [  254.104394] Modules linked in: xfs libcrc32c ublk_drv
  [  254.105152] CPU: 1 PID: 2440 Comm: iou-wrk-1990 Not tainted 6.4.0-060400-generic #202306271339
  [  254.105337] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  254.105479] RIP: 0010:refcount_warn_saturate+0xa3/0x150
  [  254.105618] Code: cc cc 0f b6 1d b9 fe de 01 80 fb 01 0f 87 c8 79 8b 00 83 e3 01 75 dd 48 c7 c7 c8 5e 1a a8 c6 05 9d fe de 01 01 e8 7d 23 92 ff <0f> 0b eb c6 0f b6 1d 90 fe de 01 80 fb 01 0f 87 88 79 8b 00 83 e3
  [  254.105839] RSP: 0018:ff79027b00bd3bf8 EFLAGS: 00000246
  [  254.105894] RAX: 0000000000000000 RBX: 0000000000000000 RCX: 0000000000000000
  [  254.105995] RDX: 0000000000000000 RSI: 0000000000000000 RDI: 0000000000000000
  [  254.106065] RBP: ff79027b00bd3c00 R08: 0000000000000000 R09: 0000000000000000
  [  254.106114] R10: 0000000000000000 R11: 0000000000000000 R12: 0000000000000002
  [  254.106162] R13: ff27ac7e84cdb640 R14: 0000000000000001 R15: ff27ac7efb2c3000
  [  254.106260] FS:  000000
  ```

### mainline-6.5.11 (6.5.11-060511-generic): fail
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2542:ublk-mem 9:kworker/0:1+events
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  26 oops/hang line(s); first: [  308.158889] INFO: task kworker/0:1:9 blocked for more than 30 seconds.
  [  308.159273]       Not tainted 6.5.11-060511-generic #202311151304
  [  308.159424] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  308.159559] task:kworker/0:1     state:D stack:0     pid:9     ppid:2      flags:0x00004000
  [  308.160132] Workqueue: events ublk_stop_work_fn [ublk_drv]
  [  308.161066] Call Trace:
  [  308.161342]  <TASK>
  [  308.161641]  __schedule+0x2cc/0x770
  [  308.161999]  schedule+0x63/0x110
  [  308.162050]  schedule_preempt_disabled+0x15/0x30
  [  308.162116]  __mutex_lock.constprop.0+0x42f/0x740
  [  308.162182]  ? add_timer+0x20/0x40
  [  308.162314]  __mutex_lock_slowpath+0x13/0x20
  [  308.162374]  mutex_lock+0x3c/0x50
  [  308.162436]  ublk_stop_dev+0x1e/0xd0 [ublk_drv]
  [  308.162510]  ublk_stop_work_fn+0x15/0x20 [ublk_drv]
  [  308.162577]  process_one_work+0x220/0x440
  [  308.162635]  worker_thread+0x4d/0x3f0
  [  308.162684]  ? _raw_spin_lock_irqsave+0xe/0x20
  [  308.162791]  ? __pfx_worker_thread+0x10/0x10
  [  308.162866]  kthread+0xef/0x120
  [  308.162915]  ? __pfx_kthread+0x10/0x10
  [  308.162966]  ret_from_fork+0x44/0x70
  [  308.163016]  ? __pfx_kthread+0x10/0x10
  [  308.16
  ```

### mainline-6.6.158 (6.6.158-0606158-generic): fail
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2539:ublk-mem 8:kworker/0:0+events
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  26 oops/hang line(s); first: [  308.361438] INFO: task kworker/0:0:8 blocked for more than 30 seconds.
  [  308.361819]       Not tainted 6.6.158-0606158-generic #202610031241
  [  308.361974] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  308.362121] task:kworker/0:0     state:D stack:0     pid:8     ppid:2      flags:0x00004000
  [  308.362666] Workqueue: events ublk_stop_work_fn [ublk_drv]
  [  308.363481] Call Trace:
  [  308.363759]  <TASK>
  [  308.364001]  __schedule+0x2cb/0x770
  [  308.364266]  schedule+0x63/0x110
  [  308.364312]  schedule_preempt_disabled+0x15/0x30
  [  308.364389]  __mutex_lock.constprop.0+0x42f/0x740
  [  308.364476]  __mutex_lock_slowpath+0x13/0x20
  [  308.364532]  mutex_lock+0x3c/0x50
  [  308.364593]  ublk_stop_dev+0x27/0x100 [ublk_drv]
  [  308.364662]  ublk_stop_work_fn+0x15/0x20 [ublk_drv]
  [  308.364729]  process_one_work+0x181/0x3a0
  [  308.364902]  worker_thread+0x18b/0x330
  [  308.364974]  ? __pfx_worker_thread+0x10/0x10
  [  308.365032]  kthread+0xef/0x120
  [  308.365121]  ? __pfx_kthread+0x10/0x10
  [  308.365175]  ret_from_fork+0x44/0x70
  [  308.365223]  ? __pfx_kthread+0x10/0x10
  [  308.365273]  ret_from_fork_asm+0x1b/0x30
  [  308.365373]  </TASK>
  [  308.365480] INFO: task
  ```

### mainline-6.10.14 (6.10.14-061014-generic): fail
- `verify-sweep` fail: integrity sweep: PASS=23 FAIL=1; FAIL: device did not appear (q=4 d=128)   log| 2026/10/04 14:16:47 [ERROR] failed to create device error=failed to START_DEV: START_DEV failed: operation canceled   integrity sweep: PASS=23 FAIL=1   failed: q4/d128/dir1(no-dev) 
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state: none
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  18 oops/hang line(s); first: [  302.754472] INFO: task ublk-suite:1995 blocked for more than 30 seconds.
  [  302.756141]       Not tainted 6.10.14-061014-generic #202411070043
  [  302.756279] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  302.756408] task:ublk-suite      state:D stack:0     pid:1995  tgid:1970  ppid:142    flags:0x00004000
  [  302.757139] Call Trace:
  [  302.757522]  <TASK>
  [  302.757947]  __schedule+0x277/0x6c0
  [  302.758465]  schedule+0x29/0xd0
  [  302.758603]  schedule_timeout+0x135/0x170
  [  302.758684]  __wait_for_common+0x91/0x190
  [  302.758754]  ? __pfx_schedule_timeout+0x10/0x10
  [  302.758835]  wait_for_completion+0x24/0x40
  [  302.758890]  io_wq_put_and_exit+0xaa/0x270
  [  302.759017]  io_uring_clean_tctx+0x84/0xba
  [  302.759120]  io_uring_cancel_generic+0x277/0x2d0
  [  302.759197]  ? futex_unqueue+0x3d/0x70
  [  302.759295]  ? __pfx_autoremove_wake_function+0x10/0x10
  [  302.759431]  __io_uring_cancel+0x14/0x20
  [  302.759496]  do_exit+0x137/0x4d0
  [  302.759664]  do_group_exit+0x34/0x90
  [  302.759725]  get_signal+0x7a9/0x890
  [  302.759779]  arch_do_signal_or_restart+0x39/0x110
  [  302.759846]  syscall_exit_to_user_mode+0x206/0x270
  [  302.759910]  do_syscall_64+0x8a/0x
  ```

### mainline-6.11.11 (6.11.11-061111-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2555:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  20 oops/hang line(s); first: [  302.769006] INFO: task ublk-suite:2017 blocked for more than 30 seconds.
  [  302.770684]       Not tainted 6.11.11-061111-generic #202412051415
  [  302.770833] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  302.770965] task:ublk-suite      state:D stack:0     pid:2017  tgid:1978  ppid:142    flags:0x00004000
  [  302.771652] Call Trace:
  [  302.771936]  <TASK>
  [  302.772182]  __schedule+0x277/0x6c0
  [  302.772674]  schedule+0x29/0xd0
  [  302.772750]  schedule_timeout+0x135/0x170
  [  302.772809]  __wait_for_common+0x91/0x190
  [  302.772869]  ? __pfx_schedule_timeout+0x10/0x10
  [  302.772943]  wait_for_completion+0x24/0x40
  [  302.772999]  io_wq_put_and_exit+0xaa/0x270
  [  302.773057]  io_uring_clean_tctx+0x84/0xba
  [  302.773115]  io_uring_cancel_generic+0x277/0x2d0
  [  302.773181]  ? __pfx_autoremove_wake_function+0x10/0x10
  [  302.773253]  __io_uring_cancel+0x14/0x20
  [  302.773306]  do_exit+0x137/0x4d0
  [  302.773353]  do_group_exit+0x34/0x90
  [  302.773413]  get_signal+0x7a9/0x890
  [  302.773464]  arch_do_signal_or_restart+0x39/0x110
  [  302.773531]  syscall_exit_to_user_mode+0x1eb/0x250
  [  302.773710]  do_syscall_64+0x8a/0x170
  [  302.773785]  ? irqentry_exit+0x43/0
  ```

### mainline-6.12.112 (6.12.112-0612112-generic): fail
- `lifecycle/churn-leaks` fail: leaked 61 memory mappings over 5 create/close cycles (~12.2 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2541:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  20 oops/hang line(s); first: [  302.775098] INFO: task ublk-suite:1990 blocked for more than 30 seconds.
  [  302.777175]       Not tainted 6.12.112-0612112-generic #202610031248
  [  302.777354] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  302.777591] task:ublk-suite      state:D stack:0     pid:1990  tgid:1977  ppid:145    flags:0x00004004
  [  302.778152] Call Trace:
  [  302.778547]  <TASK>
  [  302.778826]  __schedule+0x2bf/0x640
  [  302.779581]  schedule+0x2b/0xf0
  [  302.779656]  ? try_to_wake_up+0x2ec/0x7a0
  [  302.779719]  schedule_timeout+0x137/0x170
  [  302.779776]  wait_for_completion+0x81/0x140
  [  302.779834]  io_wq_put_and_exit+0xa2/0x260
  [  302.780028]  io_uring_clean_tctx+0x87/0xbb
  [  302.780091]  io_uring_cancel_generic+0x272/0x2c0
  [  302.780171]  ? futex_wait_queue+0x69/0xa0
  [  302.780228]  ? __pfx_autoremove_wake_function+0x10/0x10
  [  302.780300]  __io_uring_cancel+0x1b/0x30
  [  302.780488]  do_exit+0x12e/0x4c0
  [  302.780607]  do_group_exit+0x2d/0xb0
  [  302.780660]  get_signal+0x7a7/0x880
  [  302.780774]  arch_do_signal_or_restart+0x39/0x1f0
  [  302.780888]  syscall_exit_to_user_mode+0x146/0x1d0
  [  302.780956]  do_syscall_64+0x8d/0x1a0
  [  302.781008]  ? kmem_cache_free+0x439/0
  ```

### mainline-6.13.12 (6.13.12-061312-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: create: failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:3190:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [  219.110805] BUG: kernel NULL pointer dereference, address: 0000000000000080
  [  219.111169] #PF: supervisor read access in kernel mode
  [  219.111222] #PF: error_code(0x0000) - not-present page
  [  219.111368] PGD 2a66067 P4D 16cc5067 PUD 4825067 PMD 0 
  [  219.111411] Oops: Oops: 0000 [#1] PREEMPT SMP NOPTI
  [  219.111411] CPU: 1 UID: 0 PID: 1994 Comm: ublk-suite Not tainted 6.13.12-061312-generic #202504200842
  [  219.111411] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  219.111411] RIP: 0010:io_req_uring_cleanup+0x1b/0xb0
  [  219.111411] Code: 90 90 90 90 90 90 90 90 90 90 90 90 90 90 90 0f 1f 44 00 00 55 48 89 e5 41 55 41 54 41 89 f4 53 4c 8b af b8 00 00 00 48 89 fb <49> 8b bd 80 00 00 00 48 85 ff 74 10 e8 f4 91 c1 ff 49 c7 85 80 00
  [  219.111411] RSP: 0018:ff5cdc87017ab9c0 EFLAGS: 00000202
  [  219.111411] RAX: 0000000000000c00 RBX: ff3fd329ccf55e00 RCX: 0000000000000001
  [  219.111411] RDX: 0000000000000000 RSI: 0000000000000001 RDI: ff3fd329ccf55e00
  [  219.111411] RBP: ff5cdc87017ab9d8 R08: 0000000000000000 R09: 0000000000000000
  [  219.111411] R10: 0000000000000000 R11: 0000000000000000 R12: 0000000000000001
  [
  ```

### mainline-6.14.11 (6.14.11-061411-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server did not report readiness
- `suite/hygiene` fail: leaked: ublk-suite-process D-state: none
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [  215.551086] BUG: kernel NULL pointer dereference, address: 0000000000000000
  [  215.551316] #PF: supervisor read access in kernel mode
  [  215.551362] #PF: error_code(0x0000) - not-present page
  [  215.551438] PGD 9046067 P4D bd8c067 PUD 2ace067 PMD 0 
  [  215.551900] Oops: Oops: 0000 [#1] PREEMPT SMP NOPTI
  [  215.552061] CPU: 0 UID: 0 PID: 2059 Comm: ublk-suite Not tainted 6.14.11-061411-generic #202506101206
  [  215.552231] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  215.552438] RIP: 0010:io_req_uring_cleanup+0x1b/0xb0
  [  215.552876] Code: 90 90 90 90 90 90 90 90 90 90 90 90 90 90 90 0f 1f 44 00 00 55 48 89 e5 41 55 41 54 41 89 f4 53 4c 8b af b8 00 00 00 48 89 fb <49> 8b 7d 00 48 85 ff 74 0d e8 c7 92 c1 ff 49 c7 45 00 00 00 00 00
  [  215.553102] RSP: 0018:ff82ee49018d3a28 EFLAGS: 00000202
  [  215.553160] RAX: 0000000000000c00 RBX: ff3923407c120500 RCX: 0000000000000001
  [  215.553215] RDX: 0000000000000000 RSI: 0000000000000001 RDI: ff3923407c120500
  [  215.553266] RBP: ff82ee49018d3a40 R08: 0000000000000000 R09: 0000000000000000
  [  215.553315] R10: 0000000000000000 R11: 0000000000000000 R12: 0000000000000001
  [ 
  ```

### mainline-6.15.11 (6.15.11-061511-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: close /dev/ublkb1: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process D-state: none
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [  216.297330] BUG: kernel NULL pointer dereference, address: 0000000000000000
  [  216.297681] #PF: supervisor read access in kernel mode
  [  216.297760] #PF: error_code(0x0000) - not-present page
  [  216.297904] PGD 8eb0067 P4D 5a86067 PUD 15252067 PMD 0 
  [  216.298380] Oops: Oops: 0000 [#1] SMP NOPTI
  [  216.298772] CPU: 0 UID: 0 PID: 1990 Comm: ublk-suite Not tainted 6.15.11-061511-generic #202508201748 PREEMPT(voluntary) 
  [  216.298921] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  216.299130] RIP: 0010:io_req_uring_cleanup+0x1b/0xc0
  [  216.299517] Code: 90 90 90 90 90 90 90 90 90 90 90 90 90 90 90 0f 1f 44 00 00 55 48 89 e5 41 55 41 54 41 89 f4 53 4c 8b af b8 00 00 00 48 89 fb <49> 8b 7d 00 48 85 ff 74 0d e8 37 e7 c0 ff 49 c7 45 00 00 00 00 00
  [  216.299754] RSP: 0018:ff4f4837c17db978 EFLAGS: 00000202
  [  216.299811] RAX: 0000000000000c00 RBX: ff1adcf908e68200 RCX: 0000000000000001
  [  216.299866] RDX: 0000000000000000 RSI: 0000000000000001 RDI: ff1adcf908e68200
  [  216.299916] RBP: ff4f4837c17db990 R08: 0000000000000000 R09: 0000000000000000
  [  216.299966] R10: 0000000000000000 R11: 0000000000000000 R12: 000000
  ```

### mainline-6.16.12 (6.16.12-061612-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` timeout: no result after 1m0s
- `lifecycle/chaos` fail: close /dev/ublkb3: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2847:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [  214.402735] BUG: kernel NULL pointer dereference, address: 0000000000000000
  [  214.403286] #PF: supervisor read access in kernel mode
  [  214.403584] #PF: error_code(0x0000) - not-present page
  [  214.403952] PGD 7ebbb067 P4D 8900067 PUD b67a067 PMD 0 
  [  214.406616] Oops: Oops: 0000 [#1] SMP NOPTI
  [  214.408005] CPU: 0 UID: 0 PID: 2009 Comm: ublk-suite Not tainted 6.16.12-061612-generic #202510121336 PREEMPT(voluntary) 
  [  214.408451] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  214.409554] RIP: 0010:io_req_uring_cleanup+0x1b/0xc0
  [  214.411268] Code: 90 90 90 90 90 90 90 90 90 90 90 90 90 90 90 0f 1f 44 00 00 55 48 89 e5 41 55 41 54 41 89 f4 53 4c 8b af b8 00 00 00 48 89 fb <49> 8b 7d 00 48 85 ff 74 0d e8 97 d6 bf ff 49 c7 45 00 00 00 00 00
  [  214.411772] RSP: 0018:ff6d7943017539c0 EFLAGS: 00000202
  [  214.411832] RAX: 0000000000000c00 RBX: ff29639fbe9d8000 RCX: 0000000000000001
  [  214.411888] RDX: 0000000000000000 RSI: 0000000000000001 RDI: ff29639fbe9d8000
  [  214.411939] RBP: ff6d7943017539d8 R08: 0000000000000000 R09: 0000000000000000
  [  214.411989] R10: 0000000000000000 R11: 0000000000000000 R12: 000000
  ```

### mainline-6.17.12 (6.17.12-061712-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to open /dev/ublkc0: device or resource busy
- `lifecycle/chaos` fail: close /dev/ublkb3: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s

### mainline-6.18.55 (6.18.55-061855-generic): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to open /dev/ublkc0: device or resource busy
- `lifecycle/chaos` fail: close /dev/ublkb2: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s

### mainline-6.19.14 (6.19.14-061914-generic): fail
- `lifecycle/ctx-cancel-idle` fail: Close did not return within 20s after the context was cancelled
- `lifecycle/churn-leaks` fail: failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/concurrent-create` fail: worker 0 cycle 0: create: failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/close-under-load` fail: failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/server-killed` fail: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/restart-after-stop` fail: failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: create: failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/exit` fail: ublk-suite reported every test but did not exit within 30s: State: D (disk sleep) wchan=io_wq_put_and_exit
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:10:kworker/0:1+events 1974:ublk-suite 2991:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  28 oops/hang line(s); first: [  272.605460] INFO: task iou-wrk-1974:1981 blocked for more than 30 seconds.
  [  272.608854]       Not tainted 6.19.14-061914-generic #202604221411
  [  272.609390] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  272.609808] task:iou-wrk-1974    state:D stack:0     pid:1981  tgid:1974  ppid:138    task_flags:0x484150 flags:0x00080000
  [  272.610907] Call Trace:
  [  272.611338]  <TASK>
  [  272.611925]  __schedule+0x2ad/0x600
  [  272.612476]  schedule+0x27/0x90
  [  272.612689]  schedule_preempt_disabled+0x15/0x30
  [  272.612857]  __mutex_lock.constprop.0+0x4f1/0xa10
  [  272.612971]  __mutex_lock_slowpath+0x13/0x20
  [  272.613080]  mutex_lock+0x3b/0x50
  [  272.613153]  __del_gendisk+0x68/0x340
  [  272.613224]  ? update_load_avg+0x8e/0x420
  [  272.613306]  del_gendisk+0x7c/0xc0
  [  272.613646]  ublk_stop_dev_unlocked.part.0+0x38/0x150 [ublk_drv]
  [  272.614066]  ublk_stop_dev+0x2f/0xb0 [ublk_drv]
  [  272.614188]  ublk_ctrl_uring_cmd+0x517/0x7e0 [ublk_drv]
  [  272.614288]  ? sysvec_apic_timer_interrupt+0x54/0xd0
  [  272.614412]  io_uring_cmd+0xb7/0x190
  [  272.614640]  __io_issue_sqe+0x41/0x1c0
  [  272.614751]  ? finish_task_switch.isra.0+0x9b/0x2d0
  [  272.614891]  io_issue_sqe
  ```

### amazon-2023-6.1 (6.1.186-228.376.amzn2023.x86_64): no-ublk

### amazon-2023-6.12 (6.12.103-129.197.amzn2023.x86_64): no-ublk

### arch-lts (6.18.55-1-lts): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to open /dev/ublkc0: device or resource busy
- `lifecycle/chaos` fail: close /dev/ublkb3: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s

### arch (7.2.8-arch1-2): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` timeout: no result after 1m0s
- `lifecycle/chaos` fail: close /dev/ublkb1: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2796:ublk-mem 41:khugepaged
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [  218.816253] BUG: kernel NULL pointer dereference, address: 0000000000000018
  [  218.816430] #PF: supervisor write access in kernel mode
  [  218.816479] #PF: error_code(0x0002) - not-present page
  [  218.816555] PGD 9527067 P4D 4e3f067 PUD 4abc067 PMD 0 
  [  218.816615] Oops: Oops: 0002 [#1] SMP NOPTI
  [  218.816615] CPU: 1 UID: 0 PID: 28 Comm: kworker/1:0 Not tainted 7.2.8-arch1-2 #1 PREEMPT(full)  186163caea6eb5dc4b2a0252463c7ce1c6927294
  [  218.816615] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [  218.816615] Workqueue: events ublk_partition_scan_work [ublk_drv]
  [  218.816615] RIP: 0010:ublk_queue_rq+0x4d/0xc0 [ublk_drv]
  [  218.816615] Code: f8 ff ff 84 c0 75 31 80 7b 19 00 75 36 48 63 55 20 48 c7 c6 90 56 40 c0 88 44 24 07 48 c1 e2 06 48 8b bc 13 90 00 00 00 31 d2 <48> 89 6f 18 e8 ca ea 05 f1 0f b6 44 24 07 48 83 c4 08 5b 5d c3 cc
  [  218.816615] RSP: 0018:ff4484c1800f3a40 EFLAGS: 00000246
  [  218.816615] RAX: 0000000000000000 RBX: ff11e4fa84dd8000 RCX: 0000000000000000
  [  218.816615] RDX: 0000000000000000 RSI: ffffffffc0405690 RDI: 0000000000000000
  [  218.816615] RBP: ff11e4fa8cc42940 R08: ff11e4faf811f100 R09:
  ```

### debian-12 (6.1.0-53-amd64): no-ublk

### debian-12-backports (6.12.95+deb12-amd64): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2623:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  20 oops/hang line(s); first: [  308.328422] INFO: task ublk-suite:2048 blocked for more than 30 seconds.
  [  308.329929]       Not tainted 6.12.95+deb12-amd64 #1 Debian 6.12.95-1~bpo12+1
  [  308.330093] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  308.330222] task:ublk-suite      state:D stack:0     pid:2048  tgid:2034  ppid:196    flags:0x00004000
  [  308.330752] Call Trace:
  [  308.330917]  <TASK>
  [  308.331132]  __schedule+0x4fb/0xc00
  [  308.332005]  schedule+0x27/0xc0
  [  308.332059]  schedule_timeout+0x15d/0x170
  [  308.332094]  __wait_for_common+0x90/0x1c0
  [  308.332128]  ? __pfx_schedule_timeout+0x10/0x10
  [  308.332169]  io_wq_put_and_exit+0xad/0x240
  [  308.332249]  io_uring_clean_tctx+0x8a/0xc0
  [  308.332285]  io_uring_cancel_generic+0x172/0x320
  [  308.332336]  ? __pfx_autoremove_wake_function+0x10/0x10
  [  308.332418]  do_exit+0x11b/0xaf0
  [  308.332453]  do_group_exit+0x30/0x80
  [  308.332483]  __x64_sys_exit_group+0x18/0x20
  [  308.332514]  x64_sys_call+0x14b1/0x16a0
  [  308.332545]  do_syscall_64+0x87/0x1b0
  [  308.332613]  ? __x64_sys_futex+0x92/0x1d0
  [  308.332648]  ? restore_fpregs_from_fpstate+0x3c/0xa0
  [  308.332688]  ? ktime_get_ts64+0x41/0x110
  [  308.332720]  ? posix_get_
  ```

### debian-13 (6.12.111+deb13-amd64): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to START_DEV: START_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `lifecycle/chaos` fail: server create: server: ERROR failed to add device: ADD_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s
- `suite/hygiene` fail: leaked: ublk-suite-process /dev/ublkb0  reaper-stuck D-state:2629:ublk-mem
- `poweroff` fail: payload finished but the guest did not power off within 45s (kernel shutdown blocked, typically by a wedged ublk device); killed by the host
- `kernel-log` fail:
  ```
  18 oops/hang line(s); first: [  308.325228] INFO: task ublk-suite:2052 blocked for more than 30 seconds.
  [  308.326649]       Not tainted 6.12.111+deb13-amd64 #1 Debian 6.12.111-1
  [  308.326795] "echo 0 > /proc/sys/kernel/hung_task_timeout_secs" disables this message.
  [  308.326923] task:ublk-suite      state:D stack:0     pid:2052  tgid:2037  ppid:197    flags:0x00004000
  [  308.327351] Call Trace:
  [  308.327547]  <TASK>
  [  308.327785]  __schedule+0x505/0xc00
  [  308.328888]  schedule+0x27/0xc0
  [  308.328952]  schedule_timeout+0x12f/0x160
  [  308.329012]  __wait_for_common+0x8e/0x1c0
  [  308.329075]  ? __pfx_schedule_timeout+0x10/0x10
  [  308.329132]  io_wq_put_and_exit+0xaa/0x240
  [  308.329169]  io_uring_clean_tctx+0x83/0xb0
  [  308.329203]  io_uring_cancel_generic+0x289/0x2d0
  [  308.329242]  ? __pfx_autoremove_wake_function+0x10/0x10
  [  308.329347]  do_exit+0x11e/0xad0
  [  308.329415]  ? __pfx_futex_wake_mark+0x10/0x10
  [  308.329464]  do_group_exit+0x30/0x80
  [  308.329495]  get_signal+0x87c/0x880
  [  308.329527]  arch_do_signal_or_restart+0x3f/0x250
  [  308.329567]  syscall_exit_to_user_mode+0x139/0x1b0
  [  308.329626]  do_syscall_64+0x93/0x1b0
  [  308.329677]  ? do_syscall_64+0x93/0x1b0
  [  308.329708]  ? __pfx_hrtime
  ```

### fedora-42 (6.19.14-108.fc42.x86_64): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to open /dev/ublkc0: device or resource busy
- `lifecycle/chaos` fail: close /dev/ublkb1: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s

### fedora-43 (7.2.8-100.fc43.x86_64): fail
- `lifecycle/churn-leaks` fail: leaked 60 memory mappings over 5 create/close cycles (~12.0 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to open /dev/ublkc0: device or resource busy
- `lifecycle/chaos` fail: close /dev/ublkb3: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s

### fedora-44 (7.2.8-200.fc44.x86_64): fail
- `lifecycle/churn-leaks` fail: leaked 61 memory mappings over 5 create/close cycles (~12.2 per cycle)
- `lifecycle/restart-after-stop` fail: Start after Stop (documented as supported): failed to open /dev/ublkc0: device or resource busy
- `lifecycle/chaos` fail: close /dev/ublkb1: failed to delete device: DEL_DEV submit failed: failed to submit control command: timeout waiting for control command completion after 10s

### almalinux-9 (5.14.0-687.53.1.el9_8.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 6 fail, 4 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     real_ring_test.go:75: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:85: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:99: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted 
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup failed: operation not permitted 

### centos-stream-9 (5.14.0-754.el9.x86_64): no-ublk
- `unit/internal_uring` fail: 18 pass, 6 fail, 4 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     real_ring_test.go:75: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:85: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:99: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted 
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup failed: operation not permitted 

### almalinux-10 (6.12.0-211.56.1.el10_2.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"5799E24E6A58591A9F62173","error":"io_uring setup: io_uring_setup failed: operation not permitted"}
- `unit/internal_uring` fail: 18 pass, 6 fail, 4 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     real_ring_test.go:75: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:85: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:99: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted 
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup failed: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted 
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1)   log| 2026/10/04 14:01:56 [ERROR] failed to create io_uring error=io_uring_setup failed: operation not permitted   log| 2026/10/04 14:01:56 [ERROR] failed to create device error=failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted FAIL: device did not appear (q=1 d=1)   log| 2026/10/04 14:01:56 [ERROR] failed to create io_uring error=io_uring_setup failed: operation not permitted   log| 2026/10/04 14:01:56 [ERROR] failed to create device error=failed to create controller: f
- `loop-e2e` fail: ; 2026/10/04 14:02:02 [ERROR] failed to create io_uring error=io_uring_setup failed: operation not permitted 2026/10/04 14:02:02 create device: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted 
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/concurrent-create` fail: worker 1 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/chaos` fail: create: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted

### centos-stream-10 (6.12.0-271.el10.x86_64): fail
- `probe` fail: {"features":"unknown","srcversion":"A72A5ED01780D367B935BB1","error":"io_uring setup: io_uring_setup failed: operation not permitted"}
- `unit/internal_uring` fail: 18 pass, 6 fail, 4 skip; failed: TestRealRingNewCloseWithPipe TestRealRingSubmitCtrlCmdPipeUnsupported TestRealRingSecondRoundTripSameRing TestRealRingOpenCloseNoRegistration TestSubmitIOCmdReturnsFabricatedSuccess TestSubmitIOCmdProductionCallSiteDiscardsResult     real_ring_test.go:75: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:85: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted     real_ring_test.go:99: NewMinimalRing(4, 5): io_uring_setup failed: operation not permitted 
- `unit/test_unit` fail: 6 pass, 1 fail, 0 skip; failed: TestURingInterface     unit_test.go:75: NewRing failed: io_uring_setup failed: operation not permitted 
- `integration/large-io` fail: 0 pass, 1 fail, 0 skip; failed: TestDisposableLargeIOPublicRunnerPaths     largeio_kernel_test.go:84: CreateAndServe with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted     largeio_kernel_test.go:84: Create with existing ublk privilege: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted 
- `verify-sweep` fail: integrity sweep: PASS=0 FAIL=24; FAIL: device did not appear (q=1 d=1)   log| 2026/10/04 14:02:12 [ERROR] failed to create io_uring error=io_uring_setup failed: operation not permitted   log| 2026/10/04 14:02:12 [ERROR] failed to create device error=failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted FAIL: device did not appear (q=1 d=1)   log| 2026/10/04 14:02:12 [ERROR] failed to create io_uring error=io_uring_setup failed: operation not permitted   log| 2026/10/04 14:02:12 [ERROR] failed to create device error=failed to create controller: f
- `loop-e2e` fail: ; 2026/10/04 14:02:18 [ERROR] failed to create io_uring error=io_uring_setup failed: operation not permitted 2026/10/04 14:02:18 create device: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted 
- `io/integrity/q1-d1-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q1-d64-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q2-d32-bs4096` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q4-d128-bs512` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity/q4-d16-bs4096-maxio64k` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/integrity-buffered` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/boundaries` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/flush` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/no-volatile-cache` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/discard` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/discard-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/write-zeroes` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/write-zeroes-large` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/no-discard-advertised` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/error-propagation` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/readonly` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `io/multi-device` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `params/geometry` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `fs/ext4` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `fs/xfs` fail: CreateAndServe: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/create-close` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/fixed-id` fail: open control device: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/create-start-stop-close` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/ctx-cancel-idle` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/churn-leaks` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/concurrent-create` fail: worker 1 cycle 0: create: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/close-under-load` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/server-killed` fail: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/restart-after-stop` fail: failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted
- `lifecycle/chaos` fail: server create: server: ERROR failed to create controller: failed to create io_uring: io_uring_setup failed: operation not permitted

