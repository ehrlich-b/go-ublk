# go-ublk kernel matrix

Generated 2026-10-04T18:19:05+00:00 against go-ublk `0aa26d9`. 29 kernels: 14 fail, 15 pass.

Rows by the commit they ran: `0aa26d9` 8, `11f8769` 21.

| Kernel id | Distro | uname -r | Base | ublk_drv | Features | Status | P/F/S/E | Oops | Wall | Failing tests |
|---|---|---|---|---|---|---|---|---|---|---|
| mainline-6.4~write-zeroes-large-capped | Ubuntu mainline build | 6.4.0-060400-generic | 6.4 | yes | - | **pass** | 1/0/0/0 |  | 5s |  |
| mainline-6.4~wzcheck | Ubuntu mainline build | 6.4.0-060400-generic | 6.4 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| mainline-6.4~wzcheck-capped | Ubuntu mainline build | 6.4.0-060400-generic | 6.4 | yes | - | **pass** | 1/0/0/0 |  | 5s |  |
| mainline-6.6.158~wzcheck | Ubuntu mainline build | 6.6.158-0606158-generic | 6.6 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| mainline-6.9.12~wzcheck | Ubuntu mainline build | 6.9.12-060912-generic | 6.9 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| mainline-6.10.14~verify-sweep-1 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **pass** | 1/0/0/0 |  | 107s |  |
| mainline-6.10.14~verify-sweep-2 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **pass** | 1/0/0/0 |  | 107s |  |
| mainline-6.10.14~verify-sweep-3 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **pass** | 1/0/0/0 |  | 108s |  |
| mainline-6.10.14~write-zeroes-large-1 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **fail** | 0/1/0/0 |  | 6s | io/write-zeroes-large |
| mainline-6.10.14~write-zeroes-large-2 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **fail** | 0/1/0/0 |  | 6s | io/write-zeroes-large |
| mainline-6.10.14~write-zeroes-large-3 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **fail** | 0/1/0/0 |  | 6s | io/write-zeroes-large |
| mainline-6.10.14~write-zeroes-large-capped | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **pass** | 1/0/0/0 |  | 5s |  |
| mainline-6.10.14~wzcheck-1 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| mainline-6.10.14~wzcheck-2 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| mainline-6.10.14~wzcheck-3 | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| mainline-6.10.14~wzcheck-capped | Ubuntu mainline build | 6.10.14-061014-generic | 6.10 | yes | - | **pass** | 1/0/0/0 |  | 8s |  |
| mainline-6.11.11~wzcheck | Ubuntu mainline build | 6.11.11-061111-generic | 6.11 | yes | - | **pass** | 1/0/0/0 |  | 8s |  |
| mainline-7.0.14~wzcheck | Ubuntu mainline build | 7.0.14-070014-generic | 7.0 | yes | - | **pass** | 1/0/0/0 |  | 7s |  |
| opensuse-leap-15.6~wzcheck | openSUSE Leap 15.6 | 6.4.0-150600.23.103-default | 6.4 | yes | - | **fail** | 0/1/0/0 |  | 8s | wzcheck |
| ubuntu-22.04-hwe~wzcheck | Ubuntu 22.04 HWE | 6.8.0-138-generic | 6.8 | yes | - | **fail** | 0/1/0/0 |  | 9s | wzcheck |
| ubuntu-24.04-ga~write-zeroes-large-capped | Ubuntu 24.04 GA | 6.8.0-146-generic | 6.8 | yes | - | **pass** | 1/0/0/0 |  | 8s |  |
| ubuntu-24.04-ga~wzcheck | Ubuntu 24.04 GA | 6.8.0-146-generic | 6.8 | yes | - | **fail** | 0/1/0/0 |  | 9s | wzcheck |
| ubuntu-24.04-ga~wzcheck-capped | Ubuntu 24.04 GA | 6.8.0-146-generic | 6.8 | yes | - | **pass** | 1/0/0/0 |  | 8s |  |
| ubuntu-24.04-hwe-6.17.0-40~possible8 | Ubuntu 24.04 HWE 6.17.0-40 (known-bad control) [possible_cpus=8] | 6.17.0-40-generic | 6.17 | yes | 0x7fff | **fail** | 1/2/0/1 | YES | 315s | integration/large-io, integration/large-io/hygiene, kernel-log |
| ubuntu-24.04-hwe-6.17.0-40~smp4 | Ubuntu 24.04 HWE 6.17.0-40 (known-bad control) | 6.17.0-40-generic | 6.17 | yes | 0x7fff | **fail** | 1/1/0/1 | YES | 313s | integration/large-io, kernel-log |
| ubuntu-24.04-hwe-7.0.0-38~ctx-cancel-idle-1 | Ubuntu 24.04 HWE 7.0.0-38 (Slide-tested) | 7.0.0-38-generic | 7.0 | yes | - | **pass** | 1/0/0/0 |  | 6s |  |
| ubuntu-24.04-hwe-7.0.0-38~ctx-cancel-idle-2 | Ubuntu 24.04 HWE 7.0.0-38 (Slide-tested) | 7.0.0-38-generic | 7.0 | yes | - | **pass** | 1/0/0/0 |  | 6s |  |
| ubuntu-24.04-hwe-7.0.0-38~ctx-cancel-idle-3 | Ubuntu 24.04 HWE 7.0.0-38 (Slide-tested) | 7.0.0-38-generic | 7.0 | yes | - | **pass** | 1/0/0/0 |  | 5s |  |
| ubuntu-24.04-hwe-7.0.0-38~wzcheck | Ubuntu 24.04 HWE 7.0.0-38 (Slide-tested) | 7.0.0-38-generic | 7.0 | yes | - | **pass** | 1/0/0/0 |  | 8s |  |

## Details for runs that did not pass

### mainline-6.4~wzcheck (6.4.0-060400-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.1s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### mainline-6.6.158~wzcheck (6.6.158-0606158-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.1s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### mainline-6.9.12~wzcheck (6.9.12-060912-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.2s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### mainline-6.10.14~write-zeroes-large-1 (6.10.14-061014-generic): fail
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120

### mainline-6.10.14~write-zeroes-large-2 (6.10.14-061014-generic): fail
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120

### mainline-6.10.14~write-zeroes-large-3 (6.10.14-061014-generic): fail
- `io/write-zeroes-large` fail: write-zeroes: backend saw 1073741824 bytes in 1 ranges, want 5368709120

### mainline-6.10.14~wzcheck-1 (6.10.14-061014-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.1s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### mainline-6.10.14~wzcheck-2 (6.10.14-061014-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.1s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### mainline-6.10.14~wzcheck-3 (6.10.14-061014-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.2s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### opensuse-leap-15.6~wzcheck (6.4.0-150600.23.103-default): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.1s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### ubuntu-22.04-hwe~wzcheck (6.8.0-138-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.1s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### ubuntu-24.04-ga~wzcheck (6.8.0-146-generic): fail
- `wzcheck` fail: write_zeroes_max_bytes=2199023255040 max_hw_sectors_kb=1024; BLKZEROOUT 5GiB took 0.2s, blkdiscard exit 0 ; write I/Os +1, write sectors +2097152; patterns still present at: 1074790400 3221225472 5368709120

### ubuntu-24.04-hwe-6.17.0-40~possible8 (6.17.0-40-generic): fail
- `integration/large-io` timeout: 0 pass, 0 fail, 0 skip; killed after 300s; last:  /usr/local/go/src/testing/testing.go:1934 +0xea created by testing.(*T).Run in goroutine 20  /usr/local/go/src/testing/testing.go:1997 +0x465 
- `integration/large-io/hygiene` fail: leaked: none D-state:169:integration.tes
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [    3.123517] BUG: kernel NULL pointer dereference, address: 0000000000000000
  [    3.123906] #PF: supervisor read access in kernel mode
  [    3.123990] #PF: error_code(0x0000) - not-present page
  [    3.124076] PGD 2031067 P4D 211a067 PUD 2110067 PMD 0 
  [    3.124286] Oops: Oops: 0000 [#1] SMP NOPTI
  [    3.124508] CPU: 1 UID: 0 PID: 174 Comm: iou-wrk-169 Not tainted 6.17.0-40-generic #40~24.04.1-Ubuntu PREEMPT(voluntary) 
  [    3.124793] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [    3.125010] RIP: 0010:ublk_init_queues+0x4e/0x1f0 [ublk_drv]
  [    3.126142] Code: 48 c7 c0 40 bc 7c 9e 31 db 48 89 45 d0 45 0f b7 6f 0a 8b 35 c4 cb e7 dd 31 c0 45 8d 65 01 41 c1 e4 06 eb 12 49 8b 4f 50 89 c2 <3b> 1c 91 0f 84 25 01 00 00 83 c0 01 89 c2 48 c7 c7 60 b5 63 9d e8
  [    3.126302] RSP: 0018:ff581391c0363bd0 EFLAGS: 00000293
  [    3.126361] RAX: 0000000000000000 RBX: 0000000000000000 RCX: 0000000000000000
  [    3.126412] RDX: 0000000000000000 RSI: 0000000000000002 RDI: 0000000000000000
  [    3.126490] RBP: ff581391c0363c08 R08: 0000000000000000 R09: 0000000000000000
  [    3.126605] R10: 0000000000000000 R11: 0000000000000000 R12:
  ```

### ubuntu-24.04-hwe-6.17.0-40~smp4 (6.17.0-40-generic): fail
- `integration/large-io` timeout: 0 pass, 0 fail, 0 skip; killed after 300s; last:  /usr/local/go/src/testing/testing.go:1934 +0xea created by testing.(*T).Run in goroutine 35  /usr/local/go/src/testing/testing.go:1997 +0x465 
- `kernel-log` fail:
  ```
  4 oops/hang line(s); first: [    5.569189] BUG: kernel NULL pointer dereference, address: 0000000000000000
  [    5.569608] #PF: supervisor read access in kernel mode
  [    5.569707] #PF: error_code(0x0000) - not-present page
  [    5.569859] PGD 3e7a7067 P4D 3e4e7067 PUD 3e7af067 PMD 0 
  [    5.570330] Oops: Oops: 0000 [#1] SMP NOPTI
  [    5.570665] CPU: 1 UID: 0 PID: 190 Comm: iou-wrk-187 Not tainted 6.17.0-40-generic #40~24.04.1-Ubuntu PREEMPT(voluntary) 
  [    5.571182] Hardware name: QEMU Ubuntu 24.04 PC v2 (i440FX + PIIX, arch_caps fix, 1996), BIOS 1.16.3-debian-1.16.3-2 04/01/2014
  [    5.571654] RIP: 0010:ublk_init_queues+0x4e/0x1f0 [ublk_drv]
  [    5.572356] Code: 48 c7 c0 40 bc fc 91 31 db 48 89 45 d0 45 0f b7 6f 0a 8b 35 c4 cb 47 d1 31 c0 45 8d 65 01 41 c1 e4 06 eb 12 49 8b 4f 50 89 c2 <3b> 1c 91 0f 84 25 01 00 00 83 c0 01 89 c2 48 c7 c7 60 b5 e3 90 e8
  [    5.572649] RSP: 0018:ff6e7d1e003afbd0 EFLAGS: 00000297
  [    5.572759] RAX: 0000000000000000 RBX: 0000000000000000 RCX: 0000000000000000
  [    5.572873] RDX: 0000000000000000 RSI: 0000000000000004 RDI: 0000000000000000
  [    5.573159] RBP: ff6e7d1e003afc08 R08: 0000000000000000 R09: 0000000000000000
  [    5.573267] R10: 0000000000000000 R11: 0000000000000000 R
  ```

