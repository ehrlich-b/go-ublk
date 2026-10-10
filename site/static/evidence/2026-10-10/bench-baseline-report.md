# Linux ublk comparative benchmark

Repeated measurements of the pinned implementations below; claims apply only to this workload and CPU budget.

Status: **complete**; valid observations 138/138.
Complete evidence and lifecycle receipts: **yes**.
Kernel `6.12.111+deb13-cloud-amd64`; fio `fio-3.39`; CPU AMD Ryzen 9 6900HX with Radeon Graphics; vCPUs 8; shared server + fio CPU budget `0,1,2,3`.

Primary samples alone form the means and sample standard deviations. A/A repeats do not inflate N. Latency columns are means of each run's I/O-weighted completion latency quantiles, pooled across its read/write jobs using JSON+ bins. No pooled percentile is inferred from averaged job percentiles.

CPU columns use percent of one core (100% = one fully busy core; values may exceed 100%). Fio usr/sys percentages are summed over workers with runtime weighting. Server usr/sys use all-thread `/proc/PID/stat` tick deltas; `/proc/stat` measures all system CPUs separately and includes fio, kernel workers and other activity. pidstat logs or ps snapshots are supporting evidence; ps lifetime averages are never substituted for interval CPU.

Kernel baselines are context; their kernel workers may run outside the userspace cpuset. Rust's unmodified ramdisk example may have fixed queue/depth values. C loop over tmpfs retains data but has an additional backend I/O path. Plain null drops writes and belongs to a separate semantic class.

| Implementation | Requested Q | Accepted Q/depth | Semantics | Workload | N | IOPS mean ± sd | MiB/s mean ± sd | p50 µs | p99 µs | p99.9 µs | fio usr/sys % | server usr/sys % | system busy % | steal % |
|---|---:|---|---|---|---:|---:|---:|---:|---:|---:|---|---|---:|---:|
| brd | 2 | fixed/fixed | retaining-ram | 128k-read | 3 | 75793.51 ± 1328.23 | 9474.19 ± 166.03 | 90.28 | 122.37 | 148.48 | 4.71/95.26 | n/a (kernel) | 12.73 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 128k-read | 3 | 92719.02 ± 3945.22 | 11589.88 ± 493.15 | 72.53 | 177.15 | 235.18 | 8.82/56.20 | 89.11/91.85 | 30.52 | 0.06 |
| lib-ublk | 2 | 2/64 | retaining-ram | 128k-read | 3 | 70884.59 ± 2357.24 | 8860.57 ± 294.66 | 96.77 | 186.03 | 216.06 | 5.14/41.08 | 42.16/55.58 | 18.67 | 0.00 |
| libublk-rs | 2 | 1/128 | retaining-ram | 128k-read | 3 | 82620.16 ± 2142.33 | 10327.52 ± 267.79 | 85.16 | 149.16 | 181.93 | 5.82/43.01 | 45.53/52.43 | 19.92 | 0.00 |
| null-blk | 2 | 2/64 | retaining-ram | 128k-read | 3 | 51349.93 ± 139.79 | 6418.74 ± 17.47 | 147.11 | 191.49 | 225.62 | 6.38/39.47 | n/a (kernel) | 18.36 | 0.00 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 128k-read | 3 | 48647.60 ± 1092.71 | 6080.95 ± 136.59 | 162.82 | 262.83 | 362.50 | 4.14/31.40 | 1.87/95.87 | 17.00 | 0.01 |
| brd | 2 | fixed/fixed | retaining-ram | 128k-write | 3 | 56641.22 ± 830.57 | 7080.15 ± 103.82 | 122.03 | 165.55 | 196.27 | 6.52/93.46 | n/a (kernel) | 12.57 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 128k-write | 3 | 64380.33 ± 2991.32 | 8047.54 ± 373.92 | 109.06 | 246.78 | 313.34 | 10.98/38.24 | 97.26/80.60 | 28.35 | 0.05 |
| lib-ublk | 2 | 2/64 | retaining-ram | 128k-write | 3 | 53963.25 ± 1539.08 | 6745.41 ± 192.39 | 140.29 | 184.66 | 224.26 | 7.14/29.62 | 50.49/47.43 | 17.96 | 0.01 |
| libublk-rs | 2 | 1/128 | retaining-ram | 128k-write | 3 | 56764.26 ± 2467.10 | 7095.53 ± 308.39 | 133.97 | 205.82 | 251.56 | 6.58/27.01 | 56.75/40.98 | 17.49 | 0.01 |
| null-blk | 2 | 2/64 | retaining-ram | 128k-write | 3 | 46492.06 ± 136.66 | 5811.51 ± 17.08 | 163.50 | 213.33 | 249.17 | 9.05/32.83 | n/a (kernel) | 17.70 | 0.01 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 128k-write | 3 | 38241.92 ± 4230.57 | 4780.24 ± 528.82 | 199.00 | 339.29 | 439.64 | 5.22/18.98 | 1.48/94.26 | 15.60 | 0.01 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randread | 3 | 766865.82 ± 4968.26 | 2995.57 ± 19.41 | 80.04 | 100.52 | 119.30 | 46.19/153.76 | n/a (kernel) | 25.00 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 559235.16 ± 63628.85 | 2184.51 ± 248.55 | 94.38 | 391.17 | 2819.41 | 39.89/127.80 | 75.43/87.71 | 40.63 | 1.11 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 288525.05 ± 3654.98 | 1127.05 ± 14.28 | 181.93 | 436.22 | 4385.45 | 20.99/83.53 | 35.90/61.78 | 26.52 | 0.01 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randread | 3 | 649358.42 ± 51942.16 | 2536.56 ± 202.90 | 95.74 | 138.41 | 168.28 | 46.55/136.87 | 33.97/63.87 | 34.96 | 0.09 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 544246.95 ± 20552.98 | 2125.96 ± 80.29 | 112.47 | 142.51 | 161.45 | 34.00/165.97 | n/a (kernel) | 25.02 | 0.01 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 371241.43 ± 10898.70 | 1450.16 ± 42.57 | 156.67 | 246.78 | 283.31 | 24.84/81.24 | 3.30/94.37 | 25.50 | 0.01 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randread-qd1 | 3 | 390597.38 ± 3118.39 | 1525.77 ± 12.18 | 0.11 | 0.13 | 0.21 | 23.00/76.97 | n/a (kernel) | 12.60 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 22985.96 ± 97.44 | 89.79 ± 0.38 | 36.10 | 56.75 | 101.21 | 4.88/16.16 | 19.40/45.03 | 10.80 | 0.02 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 38219.41 ± 277.64 | 149.29 ± 1.08 | 20.44 | 29.48 | 43.09 | 5.63/24.14 | 19.72/41.68 | 12.58 | 0.02 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randread-qd1 | 3 | 49752.79 ± 774.90 | 194.35 ± 3.03 | 14.53 | 23.00 | 32.30 | 7.10/29.85 | 26.15/49.97 | 15.37 | 0.01 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 276263.46 ± 3374.41 | 1079.15 ± 13.18 | 0.11 | 0.12 | 0.22 | 16.40/83.57 | n/a (kernel) | 12.80 | 0.01 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 38293.42 ± 408.02 | 149.58 ± 1.59 | 20.35 | 30.76 | 44.80 | 5.80/23.53 | 3.45/50.82 | 10.34 | 0.02 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randrw-70r30w | 3 | 744896.21 ± 4605.36 | 2909.75 ± 17.99 | 82.77 | 100.52 | 115.88 | 48.07/151.90 | n/a (kernel) | 25.05 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 562469.21 ± 66902.00 | 2197.15 ± 261.34 | 94.04 | 253.95 | 2517.67 | 41.96/125.96 | 73.73/88.08 | 40.81 | 0.05 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 272672.96 ± 10247.33 | 1065.13 ± 40.03 | 199.68 | 531.11 | 5100.89 | 21.04/81.99 | 34.05/63.68 | 26.16 | 0.01 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randrw-70r30w | 3 | 661882.10 ± 14165.26 | 2585.48 ± 55.33 | 94.38 | 124.76 | 156.67 | 46.21/140.20 | 34.14/63.79 | 35.63 | 0.04 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 528157.22 ± 1453.66 | 2063.11 ± 5.68 | 116.57 | 136.19 | 160.77 | 33.74/166.23 | n/a (kernel) | 24.77 | 0.00 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 361391.55 ± 1901.42 | 1411.69 ± 7.43 | 165.55 | 269.65 | 307.88 | 24.88/80.85 | 4.24/93.52 | 25.77 | 0.01 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randwrite | 3 | 709066.43 ± 5878.78 | 2769.79 ± 22.96 | 86.19 | 105.30 | 118.61 | 42.69/157.27 | n/a (kernel) | 25.14 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 564917.56 ± 59862.36 | 2206.71 ± 233.84 | 96.43 | 239.62 | 2065.75 | 41.74/131.01 | 75.37/89.34 | 41.95 | 0.08 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 254681.93 ± 1852.10 | 994.85 ± 7.23 | 230.40 | 615.77 | 4106.92 | 19.25/83.43 | 25.63/72.10 | 25.98 | 0.01 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randwrite | 3 | 672654.42 ± 18111.73 | 2627.56 ± 70.75 | 88.92 | 129.71 | 158.04 | 47.10/143.56 | 34.86/62.98 | 35.91 | 0.02 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 510865.23 ± 2451.67 | 1995.57 ± 9.58 | 120.66 | 143.70 | 164.86 | 31.95/168.01 | n/a (kernel) | 24.90 | 0.00 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 371706.58 ± 6841.73 | 1451.98 ± 26.73 | 174.42 | 246.10 | 283.31 | 25.19/86.41 | 4.65/93.07 | 26.86 | 0.01 |

## A/A noise and throughput ranking

Noise = max(initial A/A symmetric percentage gap, RMS of at least three interleaved A/A gaps), independently for each queue setting and workload. A gap uses the mean of the two rates as its denominator. A winner requires a strictly greater gap than 2× this floor, at least three rounds, all planned observations, matched accepted geometry and data semantics, checked source pins, an enforced cpuset, and the coordinator's quiet-window attestation. This threshold is a noise guard, not a statistical significance test. QD1 is also ranked by throughput; tails and CPU are shown above.

| Requested Q | Workload | A/A pairs + pilot | Noise floor % | Ranked matched userspace peers | Gap % | Result |
|---:|---|---:|---:|---|---:|---|
| 2 | 4k-randread | 0 + pilot | n/a | libublk-rs (649358.4) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randread | 3 + pilot | 6.006 | go-ublk (559235.2) > ublksrv-loop (371241.4) > lib-ublk (288525.1) | 40.408 | No winner: evidence gates incomplete |
| 2 | 4k-randwrite | 0 + pilot | n/a | libublk-rs (672654.4) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randwrite | 3 + pilot | 5.957 | go-ublk (564917.6) > ublksrv-loop (371706.6) > lib-ublk (254681.9) | 41.257 | No winner: evidence gates incomplete |
| 2 | 4k-randrw-70r30w | 0 + pilot | n/a | libublk-rs (661882.1) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randrw-70r30w | 3 + pilot | 3.213 | go-ublk (562469.2) > ublksrv-loop (361391.6) > lib-ublk (272673.0) | 43.530 | No winner: evidence gates incomplete |
| 2 | 4k-randread-qd1 | 0 + pilot | n/a | libublk-rs (49752.8) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randread-qd1 | 3 + pilot | 0.763 | ublksrv-loop (38293.4) > lib-ublk (38219.4) > go-ublk (22986.0) | 0.193 | No winner: evidence gates incomplete |
| 2 | 128k-read | 0 + pilot | n/a | libublk-rs (10829189434.7) | n/a | No winner: insufficient matched peers |
| 2 | 128k-read | 3 + pilot | 10.295 | go-ublk (12152867222.7) > lib-ublk (9290984386.3) > ublksrv-loop (6376338393.3) | 26.692 | No winner: evidence gates incomplete |
| 2 | 128k-write | 0 + pilot | n/a | libublk-rs (7440205107.0) | n/a | No winner: insufficient matched peers |
| 2 | 128k-write | 3 + pilot | 19.164 | go-ublk (8438458639.7) > lib-ublk (7073070696.3) > ublksrv-loop (5012444571.7) | 17.605 | No winner: evidence gates incomplete |

## Provenance

```json
{
  "binary_sha256": {
    "go-ublk": "a46257ec54a5d643392a103a894f7b1bc67af78a87b580f52a9836493d051a47",
    "lib-ublk": "b2c681c66c8912a59ad29aa5f25d1ede09972de6a7fed754cbf6d30e36386458",
    "libublk-rs": "cc7a8259e3a09123514fbab14ef4d4c31d9aa310ca8dc2fc55921389397a1749",
    "ublk": "7d9b34c864ce4b2a81849a529669dea1572867fe0850c4c4edbec7648d8999f6",
    "ublksrv-loop": "24d78f067bcd6b0dd239df9d580e1b64b3362baba49a5e55efd2dd32a1f3e14c"
  },
  "cargo": "cargo 1.85.1 (d73d2caf9 2024-12-31)",
  "cpu_budget": "0,1,2,3",
  "cpu_budget_enforced": true,
  "cpu_model": "AMD Ryzen 9 6900HX with Radeon Graphics",
  "fio": "fio-3.39",
  "gcc": "gcc (Debian 14.2.0-19) 14.2.0",
  "go-ublk": {
    "artifact": "ublk-mem",
    "sha256": "a46257ec54a5d643392a103a894f7b1bc67af78a87b580f52a9836493d051a47",
    "source_sha": "1c800535eef65723d1580b064ee5b8ef50ea2fd4"
  },
  "harness_files_sha256": {
    "bench.sh": "a6440334dad393d6aa018cb4fc772eac539d9cfed250cee1153822cdbe9a7af4",
    "lib/__init__.py": "71c8ec8b4bb85daed8a28f758cd5dc29bcf6f95f84e10f3d5d26092758f32824",
    "lib/build.py": "a368ef56db8a6e0189d04adbe91d5b911d5bb2beb6c2a21fb02e2459555007f5",
    "lib/common.py": "9f23daf81fd25b349e40a484fba7a4a521ae4aae7765a047e497c3123f9070c0",
    "lib/cpu.py": "1992fcfc55dea3a27f82221164e4a8e41d8d0a3d5840eafe7fd4d592ed66706c",
    "lib/harness.py": "c2bbdc1fda7d5d6712799d2b655190d1c5bdda978dd101b699e90a3c74e46eb6",
    "lib/report.py": "dd9bb1fc05fd952836056d2c7ee559e7a5466396f16b0f780a9489ea961e8e77",
    "pins.json": "6c6a013d74f45e24d4fe257b7638b0551f711a8b75edfa95c54defa0e8746254",
    "report.py": "2aedb4ada7bb5783d1c664d9c5edff742d087c22af19908d0274fec8db70e801"
  },
  "harness_sha": "0dc7359cbdb81aa70302e5378668ce27851e83dd",
  "kernel": "6.12.111+deb13-cloud-amd64",
  "kernel_baselines": {
    "brd": {
      "kernel": "6.12.111+deb13-cloud-amd64",
      "module_sha256": "715ab3ad6954fd7a62e54e4a638be9429c80979285c6379d3ca3e38e36fe4e06",
      "source_sha": "not exposed by Debian kernel; kernel build and module SHA256 recorded"
    },
    "null-blk": {
      "kernel": "6.12.111+deb13-cloud-amd64",
      "module_sha256": "6a8dc3083354be4d34926a9dd6778ff74dd397048e8ea04cde8ef24d04e4747c",
      "source_sha": "not exposed by Debian kernel; kernel build and module SHA256 recorded"
    }
  },
  "kernel_build": "#1 SMP PREEMPT_DYNAMIC Debian 6.12.111-1 (2026-09-28)",
  "lib-ublk": {
    "artifact": "lib-ublk-gnu.tar.gz",
    "sha256": "3bd4be5cac52402ad281ed277568ed763ae5c495aef8ce706108dd2ff05cdcd6",
    "source_sha": "e0743b3eb034a8343f7762ea25f44f26193e67ce"
  },
  "libublk-rs": {
    "cargo_lock_sha256": "20259460bf9a25cb972a02925f0ad9dab133d1afd09c0d7eb833437e8d9bd640",
    "example": "ramdisk",
    "fixed_depth": 128,
    "fixed_queues": 1,
    "lock_origin": "resolved once on VM; replay with saved Cargo.lock",
    "resolved_sha": "479f3097e128d595877185781987d218fe78c047",
    "sha": "479f3097e128d595877185781987d218fe78c047",
    "submodules": "",
    "tag": "v0.4.6",
    "url": "https://github.com/ublk-org/libublk-rs.git"
  },
  "liburing": "2.9",
  "lscpu": "{\n   \"lscpu\": [\n      {\n         \"field\": \"Architecture:\",\n         \"data\": \"x86_64\"\n      },{\n         \"field\": \"CPU op-mode(s):\",\n         \"data\": \"32-bit, 64-bit\"\n      },{\n         \"field\": \"Address sizes:\",\n         \"data\": \"48 bits physical, 48 bits virtual\"\n      },{\n         \"field\": \"Byte Order:\",\n         \"data\": \"Little Endian\"\n      },{\n         \"field\": \"CPU(s):\",\n         \"data\": \"8\"\n      },{\n         \"field\": \"On-line CPU(s) list:\",\n         \"data\": \"0-7\"\n      },{\n         \"field\": \"Vendor ID:\",\n         \"data\": \"AuthenticAMD\"\n      },{\n         \"field\": \"Model name:\",\n         \"data\": \"AMD Ryzen 9 6900HX with Radeon Graphics\"\n      },{\n         \"field\": \"CPU family:\",\n         \"data\": \"25\"\n      },{\n         \"field\": \"Model:\",\n         \"data\": \"68\"\n      },{\n         \"field\": \"Thread(s) per core:\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"Core(s) per socket:\",\n         \"data\": \"8\"\n      },{\n         \"field\": \"Socket(s):\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"Stepping:\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"BogoMIPS:\",\n         \"data\": \"6587.62\"\n      },{\n         \"field\": \"Flags:\",\n         \"data\": \"fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm rep_good nopl xtopology cpuid extd_apicid tsc_known_freq pni pclmulqdq ssse3 fma cx16 sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm cmp_legacy svm cr8_legacy abm sse4a misalignsse 3dnowprefetch osvw perfctr_core ssbd ibrs ibpb stibp vmmcall fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid rdseed adx smap clflushopt clwb sha_ni xsaveopt xsavec xgetbv1 xsaves user_shstk clzero xsaveerptr wbnoinvd arat npt lbrv nrip_save tsc_scale vmcb_clean flushbyasid pausefilter pfthreshold v_vmsave_vmload vgif umip pku ospke vaes vpclmulqdq rdpid overflow_recov succor fsrm\"\n      },{\n         \"field\": \"Virtualization:\",\n         \"data\": \"AMD-V\"\n      },{\n         \"field\": \"Hypervisor vendor:\",\n         \"data\": \"KVM\"\n      },{\n         \"field\": \"Virtualization type:\",\n         \"data\": \"full\"\n      },{\n         \"field\": \"L1d cache:\",\n         \"data\": \"512 KiB (8 instances)\"\n      },{\n         \"field\": \"L1i cache:\",\n         \"data\": \"512 KiB (8 instances)\"\n      },{\n         \"field\": \"L2 cache:\",\n         \"data\": \"4 MiB (8 instances)\"\n      },{\n         \"field\": \"L3 cache:\",\n         \"data\": \"128 MiB (8 instances)\"\n      },{\n         \"field\": \"NUMA node(s):\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"NUMA node0 CPU(s):\",\n         \"data\": \"0-7\"\n      },{\n         \"field\": \"Vulnerability Gather data sampling:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Indirect target selection:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Itlb multihit:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability L1tf:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Mds:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Meltdown:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Mmio stale data:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Reg file data sampling:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Retbleed:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Spec rstack overflow:\",\n         \"data\": \"Mitigation; Safe RET\"\n      },{\n         \"field\": \"Vulnerability Spec store bypass:\",\n         \"data\": \"Mitigation; Speculative Store Bypass disabled via prctl\"\n      },{\n         \"field\": \"Vulnerability Spectre v1:\",\n         \"data\": \"Mitigation; usercopy/swapgs barriers and __user pointer sanitization\"\n      },{\n         \"field\": \"Vulnerability Spectre v2:\",\n         \"data\": \"Mitigation; Retpolines; IBPB conditional; IBRS_FW; STIBP disabled; RSB filling; PBRSB-eIBRS Not affected; BHI Not affected\"\n      },{\n         \"field\": \"Vulnerability Srbds:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Tsa:\",\n         \"data\": \"Mitigation; Clear CPU buffers\"\n      },{\n         \"field\": \"Vulnerability Tsx async abort:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Vmscape:\",\n         \"data\": \"Not affected\"\n      }\n   ]\n}\n",
  "machine": "x86_64",
  "packages": "cargo\t1.85.1+dfsg1-1+deb13u1\nfio\t3.39-1\ng++\t4:14.2.0-1\ngcc\t4:14.2.0-1\nlibc6:amd64\t2.41-12+deb13u4\nliburing-dev:amd64\t2.9-1\nrustc\t1.85.1+dfsg1-1+deb13u1\n",
  "rustc": "rustc 1.85.1 (4eb161250 2025-03-15) (built from a source tarball)",
  "server_environment": {
    "GODEBUG": null,
    "GOMAXPROCS": "4",
    "LD_PRELOAD": null,
    "MALLOC_ARENA_MAX": null
  },
  "supplied_inputs": {
    "go-ublk": {
      "artifact": "ublk-mem",
      "sha256": "a46257ec54a5d643392a103a894f7b1bc67af78a87b580f52a9836493d051a47",
      "source_sha": "1c800535eef65723d1580b064ee5b8ef50ea2fd4"
    },
    "lib-ublk": {
      "artifact": "lib-ublk-gnu.tar.gz",
      "sha256": "3bd4be5cac52402ad281ed277568ed763ae5c495aef8ce706108dd2ff05cdcd6",
      "source_sha": "e0743b3eb034a8343f7762ea25f44f26193e67ce"
    }
  },
  "ublksrv": {
    "cflags": "-O2 -g0",
    "resolved_sha": "abbfea2b59184b26e7212ce3d4c47702450510df",
    "sha": "abbfea2b59184b26e7212ce3d4c47702450510df",
    "submodules": "",
    "tag": "v1.8",
    "target": "loop over private tmpfs (buffered backend)",
    "url": "https://github.com/ublk-org/ublksrv.git"
  },
  "vcpus": 8,
  "virtualization": "kvm"
}
```

Public extract: numerical results, methodology, source revisions, and binary hashes are preserved. Machine names, local paths, and internal package-validation notes have been omitted. Raw fio files and lifecycle logs are outside this extract.
