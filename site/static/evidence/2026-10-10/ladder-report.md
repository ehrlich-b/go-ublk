# Linux ublk comparative benchmark

Repeated measurements of the pinned implementations below; claims apply only to this workload and CPU budget.

Status: **complete**; valid observations 264/264.
Complete evidence and lifecycle receipts: **yes**.
Kernel `6.12.111+deb13-cloud-amd64`; fio `fio-3.39`; CPU AMD Ryzen 9 6900HX with Radeon Graphics; vCPUs 8; disjoint CPU budget `0,1,2,3,4,5,6,7`; fio `0,1,2,3`, server `4,5,6,7`.

Primary samples alone form the means and sample standard deviations. A/A repeats do not inflate N. Latency columns are means of each run's I/O-weighted completion latency quantiles, pooled across its read/write jobs using JSON+ bins. No pooled percentile is inferred from averaged job percentiles.

CPU columns use percent of one core (100% = one fully busy core; values may exceed 100%). Fio usr/sys percentages are summed over workers with runtime weighting. Server usr/sys use all-thread `/proc/PID/stat` tick deltas; `/proc/stat` measures all system CPUs separately and includes fio, kernel workers and other activity. pidstat logs or ps snapshots are supporting evidence; ps lifetime averages are never substituted for interval CPU.

Kernel baselines are context; their kernel workers may run outside the userspace cpuset. Rust's unmodified ramdisk example may have fixed queue/depth values. C loop over tmpfs retains data but has an additional backend I/O path. Plain null drops writes and belongs to a separate semantic class.

| Implementation | Requested Q | Accepted Q/depth | Semantics | Workload | N | IOPS mean ± sd | MiB/s mean ± sd | p50 µs | p99 µs | p99.9 µs | fio usr/sys % | server usr/sys % | system busy % | steal % |
|---|---:|---|---|---|---:|---:|---:|---:|---:|---:|---|---|---:|---:|
| brd | 2 | fixed/fixed | retaining-ram | 128k-read | 3 | 72909.54 ± 1813.25 | 9113.69 ± 226.66 | 93.35 | 130.05 | 160.77 | 5.27/94.69 | n/a (kernel) | 12.84 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 128k-read | 3 | 91183.25 ± 5425.49 | 11397.91 ± 678.19 | 77.65 | 160.09 | 236.89 | 9.33/59.18 | 96.25/101.00 | 33.50 | 0.31 |
| go-ublk-inline | 2 | 2/64 | retaining-ram | 128k-read | 3 | 78383.49 ± 4668.31 | 9797.94 ± 583.54 | 93.70 | 171.01 | 218.79 | 6.15/41.48 | 48.98/49.87 | 18.68 | 0.02 |
| go-ublk-null | 2 | 2/64 | null | 128k-read | 3 | 103221.81 ± 6695.44 | 12902.73 ± 836.93 | 70.91 | 137.56 | 189.44 | 8.77/61.25 | 18.87/107.16 | 24.61 | 0.05 |
| go-ublk-null-inline | 2 | 2/64 | null | 128k-read | 3 | 170845.21 ± 5257.46 | 21355.65 ± 657.18 | 39.85 | 60.33 | 84.82 | 10.36/85.00 | 3.69/96.77 | 24.44 | 0.01 |
| lib-ublk | 2 | 2/64 | retaining-ram | 128k-read | 3 | 69112.90 ± 662.43 | 8639.11 ± 82.80 | 100.86 | 194.90 | 225.62 | 4.88/39.90 | 43.14/54.70 | 18.28 | 0.00 |
| lib-ublk-null | 2 | 2/64 | null | 128k-read | 3 | 92435.17 ± 1273.53 | 11554.40 ± 159.19 | 74.24 | 136.02 | 155.99 | 6.57/53.42 | 27.94/69.89 | 19.91 | 0.00 |
| libublk-rs | 2 | 1/128 | retaining-ram | 128k-read | 3 | 80497.08 ± 3161.98 | 10062.13 ± 395.25 | 86.53 | 153.94 | 192.85 | 5.60/42.94 | 46.01/51.78 | 18.48 | 0.01 |
| libublk-rs-null | 2 | 2/64 | null | 128k-read | 3 | 180301.54 ± 465.52 | 22537.69 ± 58.19 | 37.46 | 56.23 | 73.30 | 10.79/87.68 | 1.97/95.51 | 25.08 | 0.01 |
| null-blk | 2 | 2/64 | retaining-ram | 128k-read | 3 | 49726.64 ± 583.38 | 6215.83 ± 72.92 | 149.85 | 211.29 | 256.00 | 6.53/38.62 | n/a (kernel) | 18.10 | 0.01 |
| null-blk-nomem | 2 | 2/64 | null | 128k-read | 3 | 129849.43 ± 2663.88 | 16231.18 ± 332.98 | 52.99 | 71.17 | 82.43 | 8.78/91.18 | n/a (kernel) | 26.05 | 0.12 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 128k-read | 3 | 48941.74 ± 723.80 | 6117.72 ± 90.47 | 162.82 | 259.07 | 344.75 | 3.87/31.48 | 2.15/95.38 | 17.06 | 0.00 |
| ublksrv-null | 2 | 2/64 | null | 128k-read | 3 | 178402.62 ± 6277.08 | 22300.33 ± 784.64 | 37.63 | 55.38 | 72.79 | 10.00/87.61 | 1.69/95.79 | 25.06 | 0.00 |
| brd | 2 | fixed/fixed | retaining-ram | 128k-write | 3 | 54973.74 ± 1720.49 | 6871.72 ± 215.06 | 125.78 | 185.34 | 216.75 | 6.77/93.20 | n/a (kernel) | 12.60 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 128k-write | 3 | 71838.28 ± 5793.71 | 8979.78 ± 724.21 | 101.55 | 193.54 | 251.90 | 12.63/42.06 | 122.38/94.48 | 34.13 | 0.13 |
| go-ublk-inline | 2 | 2/64 | retaining-ram | 128k-write | 3 | 54817.48 ± 6085.37 | 6852.18 ± 760.67 | 151.21 | 228.35 | 281.94 | 7.42/26.40 | 54.36/41.87 | 16.98 | 0.02 |
| go-ublk-null | 2 | 2/64 | null | 128k-write | 3 | 107631.29 ± 8343.40 | 13453.91 ± 1042.92 | 66.39 | 138.92 | 179.88 | 15.12/55.80 | 25.18/109.14 | 25.35 | 0.06 |
| go-ublk-null-inline | 2 | 2/64 | null | 128k-write | 3 | 168184.14 ± 1575.95 | 21023.02 ± 196.99 | 36.61 | 66.73 | 103.59 | 17.63/73.62 | 4.68/95.59 | 23.91 | 0.01 |
| lib-ublk | 2 | 2/64 | retaining-ram | 128k-write | 3 | 52655.65 ± 716.08 | 6581.96 ± 89.51 | 144.38 | 184.66 | 229.72 | 6.52/28.16 | 51.00/46.62 | 16.81 | 0.00 |
| lib-ublk-null | 2 | 2/64 | null | 128k-write | 3 | 108871.65 ± 1774.90 | 13608.96 ± 221.86 | 66.82 | 89.60 | 121.17 | 12.50/54.88 | 11.25/86.13 | 21.90 | 0.01 |
| libublk-rs | 2 | 1/128 | retaining-ram | 128k-write | 3 | 55971.78 ± 1331.57 | 6996.47 ± 166.45 | 136.87 | 216.06 | 263.85 | 6.79/26.56 | 58.58/39.55 | 16.98 | 0.01 |
| libublk-rs-null | 2 | 2/64 | null | 128k-write | 3 | 162102.66 ± 4715.71 | 20262.83 ± 589.46 | 41.39 | 64.85 | 82.77 | 17.09/73.30 | 2.32/91.78 | 23.33 | 0.01 |
| null-blk | 2 | 2/64 | retaining-ram | 128k-write | 3 | 45118.68 ± 1078.37 | 5639.84 ± 134.80 | 167.59 | 218.79 | 262.49 | 9.16/32.13 | n/a (kernel) | 17.37 | 0.00 |
| null-blk-nomem | 2 | 2/64 | null | 128k-write | 3 | 137127.06 ± 5921.39 | 17140.88 ± 740.17 | 50.60 | 70.14 | 84.14 | 15.89/84.08 | n/a (kernel) | 25.56 | 0.06 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 128k-write | 3 | 38508.28 ± 967.51 | 4813.53 ± 120.94 | 199.00 | 311.98 | 403.46 | 4.97/19.30 | 1.52/94.00 | 14.97 | 0.01 |
| ublksrv-null | 2 | 2/64 | null | 128k-write | 3 | 177472.97 ± 5114.19 | 22184.12 ± 639.27 | 36.95 | 58.62 | 71.85 | 18.42/76.96 | 2.10/95.44 | 24.17 | 0.01 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randread | 3 | 748901.96 ± 7305.96 | 2925.40 ± 28.54 | 82.09 | 100.18 | 116.57 | 46.29/153.65 | n/a (kernel) | 24.98 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 568548.05 ± 10151.12 | 2220.89 ± 39.65 | 107.01 | 179.20 | 284.67 | 50.13/139.07 | 95.09/103.85 | 48.94 | 0.09 |
| go-ublk-inline | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 601819.01 ± 10890.82 | 2350.86 ± 42.54 | 104.62 | 149.85 | 190.12 | 41.43/129.70 | 37.34/61.56 | 34.10 | 0.07 |
| go-ublk-null | 2 | 2/64 | null | 4k-randread | 3 | 627270.72 ± 8713.52 | 2450.28 ± 34.04 | 98.47 | 158.04 | 214.70 | 51.34/145.64 | 56.43/100.12 | 44.50 | 0.05 |
| go-ublk-null-inline | 2 | 2/64 | null | 4k-randread | 3 | 750904.23 ± 8992.04 | 2933.22 ± 35.13 | 81.75 | 101.55 | 119.98 | 47.03/152.94 | 19.08/80.14 | 37.47 | 0.01 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 283428.33 ± 4403.77 | 1107.14 ± 17.20 | 185.34 | 456.70 | 4205.23 | 20.50/83.32 | 36.93/60.66 | 25.73 | 0.01 |
| lib-ublk-null | 2 | 2/64 | null | 4k-randread | 3 | 301149.68 ± 2987.66 | 1176.37 ± 11.67 | 172.37 | 432.13 | 4101.46 | 22.73/89.08 | 33.86/63.70 | 26.35 | 0.03 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randread | 3 | 656377.06 ± 14608.31 | 2563.97 ± 57.06 | 94.72 | 134.49 | 167.59 | 44.32/138.11 | 34.10/63.60 | 35.10 | 0.04 |
| libublk-rs-null | 2 | 2/64 | null | 4k-randread | 3 | 751428.28 ± 11504.50 | 2935.27 ± 44.94 | 81.75 | 97.79 | 117.59 | 45.39/154.57 | 8.43/89.09 | 37.02 | 0.00 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 518206.85 ± 6522.69 | 2024.25 ± 25.48 | 118.95 | 144.38 | 171.01 | 30.95/169.01 | n/a (kernel) | 25.34 | 0.01 |
| null-blk-nomem | 2 | 2/64 | null | 4k-randread | 3 | 697574.16 ± 13383.90 | 2724.90 ± 52.28 | 88.23 | 102.57 | 124.93 | 42.35/157.60 | n/a (kernel) | 25.41 | 0.00 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randread | 3 | 367384.13 ± 4231.12 | 1435.09 ± 16.53 | 157.35 | 253.95 | 305.15 | 25.60/81.44 | 3.67/93.93 | 25.07 | 0.01 |
| ublksrv-null | 2 | 2/64 | null | 4k-randread | 3 | 756632.54 ± 15010.16 | 2955.60 ± 58.63 | 81.75 | 96.77 | 115.88 | 48.40/151.55 | 7.09/90.41 | 37.04 | 0.00 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randread-qd1 | 3 | 380625.80 ± 2047.33 | 1486.82 ± 8.00 | 0.11 | 0.13 | 0.21 | 22.58/77.39 | n/a (kernel) | 13.28 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 23096.15 ± 530.41 | 90.22 ± 2.07 | 36.78 | 62.81 | 108.03 | 3.74/15.66 | 19.61/45.03 | 9.80 | 0.07 |
| go-ublk-inline | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 48609.34 ± 581.88 | 189.88 ± 2.27 | 14.53 | 24.96 | 48.90 | 7.61/31.08 | 9.43/27.89 | 9.34 | 0.08 |
| go-ublk-null | 2 | 2/64 | null | 4k-randread-qd1 | 3 | 23814.19 ± 262.36 | 93.02 ± 1.02 | 35.41 | 58.28 | 98.82 | 4.08/15.69 | 17.50/46.53 | 10.06 | 0.03 |
| go-ublk-null-inline | 2 | 2/64 | null | 4k-randread-qd1 | 3 | 50519.50 ± 336.26 | 197.34 ± 1.31 | 13.89 | 23.59 | 41.56 | 7.82/31.98 | 5.68/30.19 | 9.26 | 0.07 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 38604.82 ± 551.34 | 150.80 ± 2.15 | 20.35 | 29.31 | 43.43 | 5.35/24.34 | 18.59/42.51 | 11.54 | 0.02 |
| lib-ublk-null | 2 | 2/64 | null | 4k-randread-qd1 | 3 | 38408.49 ± 1947.56 | 150.03 ± 7.61 | 19.75 | 36.86 | 70.66 | 5.50/24.94 | 18.12/42.87 | 11.13 | 0.57 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randread-qd1 | 3 | 50846.33 ± 753.06 | 198.62 ± 2.94 | 14.61 | 22.49 | 31.40 | 7.22/29.34 | 26.41/51.75 | 14.04 | 0.01 |
| libublk-rs-null | 2 | 2/64 | null | 4k-randread-qd1 | 3 | 50698.83 ± 1319.15 | 198.04 ± 5.15 | 13.89 | 21.80 | 34.43 | 7.98/31.80 | 2.88/30.56 | 9.37 | 0.02 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 264838.67 ± 4933.86 | 1034.53 ± 19.27 | 0.11 | 0.13 | 0.23 | 15.42/84.56 | n/a (kernel) | 12.56 | 0.00 |
| null-blk-nomem | 2 | 2/64 | null | 4k-randread-qd1 | 3 | 352763.56 ± 2388.32 | 1377.98 ± 9.33 | 0.11 | 0.12 | 0.23 | 21.36/78.61 | n/a (kernel) | 12.49 | 0.00 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randread-qd1 | 3 | 38252.16 ± 344.57 | 149.42 ± 1.35 | 20.44 | 30.42 | 44.63 | 5.60/23.87 | 3.42/50.62 | 10.24 | 0.02 |
| ublksrv-null | 2 | 2/64 | null | 4k-randread-qd1 | 3 | 53618.85 ± 257.53 | 209.45 ± 1.01 | 13.16 | 21.12 | 35.24 | 7.32/33.33 | 2.73/28.81 | 8.66 | 0.05 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randrw-70r30w | 3 | 722591.23 ± 4682.49 | 2822.62 ± 18.29 | 84.82 | 104.96 | 121.17 | 46.73/153.23 | n/a (kernel) | 25.08 | 0.01 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 560703.84 ± 29145.18 | 2190.25 ± 113.85 | 109.74 | 177.83 | 233.13 | 51.63/139.59 | 93.57/104.11 | 48.85 | 0.09 |
| go-ublk-inline | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 598136.86 ± 5249.52 | 2336.47 ± 20.51 | 105.30 | 153.26 | 194.22 | 43.51/130.51 | 37.70/61.29 | 34.21 | 0.09 |
| go-ublk-null | 2 | 2/64 | null | 4k-randrw-70r30w | 3 | 605853.07 ± 16502.91 | 2366.61 ± 64.46 | 101.55 | 163.50 | 224.94 | 51.43/144.87 | 53.97/100.92 | 44.24 | 0.07 |
| go-ublk-null-inline | 2 | 2/64 | null | 4k-randrw-70r30w | 3 | 730038.46 ± 7934.69 | 2851.71 ± 30.99 | 83.46 | 105.30 | 124.76 | 48.55/151.38 | 18.14/81.27 | 37.63 | 0.01 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 262404.24 ± 8236.53 | 1025.02 ± 32.17 | 214.02 | 529.07 | 4838.74 | 20.64/81.68 | 34.14/63.40 | 25.25 | 0.01 |
| lib-ublk-null | 2 | 2/64 | null | 4k-randrw-70r30w | 3 | 278881.15 ± 4980.70 | 1089.38 ± 19.46 | 200.36 | 509.95 | 5144.58 | 21.94/87.59 | 31.46/66.13 | 26.11 | 0.02 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randrw-70r30w | 3 | 629258.92 ± 25202.58 | 2458.04 ± 98.45 | 100.52 | 134.83 | 160.09 | 46.36/135.58 | 33.80/64.01 | 35.04 | 0.02 |
| libublk-rs-null | 2 | 2/64 | null | 4k-randrw-70r30w | 3 | 725685.49 ± 7200.81 | 2834.71 ± 28.13 | 84.82 | 101.89 | 121.69 | 46.84/153.14 | 11.14/86.34 | 37.02 | 0.00 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 514477.61 ± 8809.09 | 2009.68 ± 34.41 | 119.98 | 143.70 | 170.33 | 33.07/166.88 | n/a (kernel) | 24.85 | 0.00 |
| null-blk-nomem | 2 | 2/64 | null | 4k-randrw-70r30w | 3 | 673483.59 ± 22708.32 | 2630.80 ± 88.70 | 90.62 | 121.51 | 152.41 | 43.29/156.64 | n/a (kernel) | 25.28 | 0.14 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randrw-70r30w | 3 | 362288.20 ± 8982.17 | 1415.19 ± 35.09 | 164.18 | 267.95 | 311.98 | 27.34/80.78 | 4.12/93.44 | 25.05 | 0.01 |
| ublksrv-null | 2 | 2/64 | null | 4k-randrw-70r30w | 3 | 731771.42 ± 11726.04 | 2858.48 ± 45.80 | 83.80 | 99.50 | 115.88 | 50.02/149.95 | 12.67/84.83 | 37.16 | 0.00 |
| brd | 2 | fixed/fixed | retaining-ram | 4k-randwrite | 3 | 689646.76 ± 1343.13 | 2693.93 ± 5.25 | 88.58 | 111.45 | 126.46 | 43.89/156.05 | n/a (kernel) | 25.28 | 0.00 |
| go-ublk | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 558978.76 ± 11026.63 | 2183.51 ± 43.07 | 110.08 | 180.57 | 265.22 | 50.94/141.50 | 97.82/103.59 | 49.65 | 0.09 |
| go-ublk-inline | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 591022.87 ± 5963.79 | 2308.68 ± 23.30 | 106.67 | 153.94 | 203.78 | 41.57/131.20 | 38.25/60.63 | 34.12 | 0.07 |
| go-ublk-null | 2 | 2/64 | null | 4k-randwrite | 3 | 606231.64 ± 4443.31 | 2368.09 ± 17.36 | 101.55 | 167.59 | 231.77 | 50.24/147.18 | 59.53/100.11 | 44.92 | 0.08 |
| go-ublk-null-inline | 2 | 2/64 | null | 4k-randwrite | 3 | 706909.94 ± 8184.98 | 2761.37 ± 31.97 | 86.53 | 109.06 | 130.73 | 48.37/151.56 | 19.38/79.92 | 37.53 | 0.01 |
| lib-ublk | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 251613.09 ± 2242.63 | 982.86 ± 8.76 | 233.13 | 618.50 | 4161.54 | 19.95/82.97 | 26.67/70.91 | 25.59 | 0.02 |
| lib-ublk-null | 2 | 2/64 | null | 4k-randwrite | 3 | 275901.24 ± 2044.34 | 1077.74 ± 7.99 | 211.29 | 539.31 | 3642.71 | 23.04/91.89 | 18.86/78.72 | 27.17 | 0.01 |
| libublk-rs | 2 | 1/128 | retaining-ram | 4k-randwrite | 3 | 670473.17 ± 25736.17 | 2619.04 ± 100.53 | 88.58 | 131.41 | 164.18 | 47.44/144.53 | 36.22/61.48 | 36.39 | 0.05 |
| libublk-rs-null | 2 | 2/64 | null | 4k-randwrite | 3 | 706629.18 ± 6212.02 | 2760.27 ± 24.27 | 86.87 | 103.59 | 121.69 | 45.60/154.37 | 12.23/85.32 | 36.97 | 0.00 |
| null-blk | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 506170.70 ± 1946.46 | 1977.23 ± 7.60 | 121.34 | 146.43 | 170.33 | 32.97/166.98 | n/a (kernel) | 25.13 | 0.00 |
| null-blk-nomem | 2 | 2/64 | null | 4k-randwrite | 3 | 665755.53 ± 12143.12 | 2600.61 ± 47.43 | 91.99 | 112.13 | 128.34 | 43.41/156.52 | n/a (kernel) | 24.96 | 0.00 |
| ublksrv-loop | 2 | 2/64 | retaining-ram | 4k-randwrite | 3 | 367190.94 ± 3729.75 | 1434.34 ± 14.57 | 176.47 | 251.56 | 298.33 | 26.15/85.24 | 4.79/92.76 | 26.33 | 0.01 |
| ublksrv-null | 2 | 2/64 | null | 4k-randwrite | 3 | 712006.11 ± 4605.80 | 2781.27 ± 17.99 | 86.19 | 102.91 | 121.69 | 48.09/151.88 | 12.21/85.16 | 37.16 | 0.00 |

## Overhead ladder

L0: no-data kernel floor; L1: kernel RAM; L2: userspace null; L3: userspace RAM. QD1 latency always comes from the separate 4k-randread-qd1 observation at this queue setting. IOPS and CPU-ns/I/O come from the named workload. CPU-ns/I/O = (fio usr+sys + server usr+sys + attributed residual kernel CPU) / completed read+write I/Os. Role-cgroup system time includes task system time, which is subtracted before adding the residual. Negative residuals from tick/runtime rounding are clamped to zero and retained in raw receipts. Excluded: kernel workers outside the benchmark cgroups/cpuset, unattributed interrupts, other host/guest work, steal, build, warm-up, preconditioning and the separate profiling pass. Legacy CPU receipts without cgroup accounting show n/a.

Deltas are signed upper minus lower; a negative IOPS delta means less throughput. Missing rungs, mismatched accepted geometry/budgets or different round sets give n/a. A/A floors are the control's reference noise percentages for each metric, not proof of a cross-rung win. lib-ublk-null zero-fills reads in the supplied C example, so its transport delta includes that data work. ublksrv L3 also includes its loop/tmpfs backend I/O path. Winner rules below remain unchanged.

### 4k-randread, requested queues 2, depth 64

| Rung | Family / mode | Implementation | Accepted Q/depth | Semantics | N | qd1 p50 µs | qd1 p99 µs | IOPS | CPU-ns/IO |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| L0 | kernel | null-blk-nomem | 2/64 | null | 3 | 0.11 | 0.12 | 697574.16 | 2872.83 |
| L1 | kernel | brd | fixed/fixed | retaining-ram | 3 | 0.11 | 0.13 | 748901.96 | 2673.59 |
| L1 | kernel | null-blk | 2/64 | retaining-ram | 3 | 0.11 | 0.13 | 518206.85 | 3869.35 |
| L2 | go-ublk / goroutine | go-ublk-null | 2/64 | null | 3 | 35.41 | 58.28 | 627270.72 | 5701.59 |
| L3 | go-ublk / goroutine | go-ublk | 2/64 | retaining-ram | 3 | 36.78 | 62.81 | 568548.05 | 6920.21 |
| L2 | go-ublk / inline | go-ublk-null-inline | 2/64 | null | 3 | 13.89 | 23.59 | 750904.23 | 4027.94 |
| L3 | go-ublk / inline | go-ublk-inline | 2/64 | retaining-ram | 3 | 14.53 | 24.96 | 601819.01 | 4532.49 |
| L2 | lib-ublk | lib-ublk-null | 2/64 | null | 3 | 19.75 | 36.86 | 301149.68 | 7045.35 |
| L3 | lib-ublk | lib-ublk | 2/64 | retaining-ram | 3 | 20.35 | 29.31 | 283428.33 | 7200.78 |
| L2 | libublk-rs | libublk-rs-null | 2/64 | null | 3 | 13.89 | 21.80 | 751428.28 | 4006.86 |
| L3 | libublk-rs | libublk-rs | 1/128 | retaining-ram | 3 | 14.61 | 22.49 | 656377.06 | 4309.44 |
| L2 | ublksrv | ublksrv-null | 2/64 | null | 3 | 13.16 | 21.12 | 756632.54 | 3981.74 |
| L3 | ublksrv | ublksrv-loop | 2/64 | retaining-ram | 3 | 20.44 | 30.42 | 367384.13 | 5680.39 |

| Delta | Lower → upper | qd1 p50 Δ µs | qd1 p99 Δ µs | IOPS Δ | CPU-ns/IO Δ | A/A floor % (p50 / p99 / IOPS / CPU) |
|---|---|---:|---:|---:|---:|---|
| kernel data cost | null-blk-nomem → null-blk | +0.00 | +0.00 | -179367.31 | +996.52 | 1.593 / 5.255 / 4.895 / 5.109 |
| kernel data cost | null-blk-nomem → brd | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 4.895 / 5.109 |
| transport tax | null-blk-nomem → go-ublk-null | +35.30 | +58.16 | -70303.44 | +2828.76 | 1.593 / 5.255 / 4.895 / 5.109 |
| data cost | go-ublk-null → go-ublk | +1.37 | +4.52 | -58722.67 | +1218.62 | 1.593 / 5.255 / 4.895 / 5.109 |
| transport tax | null-blk-nomem → go-ublk-null-inline | +13.78 | +23.47 | +53330.07 | +1155.11 | 1.593 / 5.255 / 4.895 / 5.109 |
| data cost | go-ublk-null-inline → go-ublk-inline | +0.64 | +1.37 | -149085.22 | +504.55 | 1.593 / 5.255 / 4.895 / 5.109 |
| transport tax | null-blk-nomem → lib-ublk-null | +19.64 | +36.74 | -396424.48 | +4172.52 | 1.593 / 5.255 / 4.895 / 5.109 |
| data cost | lib-ublk-null → lib-ublk | +0.60 | -7.55 | -17721.35 | +155.43 | 1.593 / 5.255 / 4.895 / 5.109 |
| transport tax | null-blk-nomem → libublk-rs-null | +13.78 | +21.68 | +53854.12 | +1134.03 | 1.593 / 5.255 / 4.895 / 5.109 |
| data cost | libublk-rs-null → libublk-rs | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 4.895 / 5.109 |
| transport tax | null-blk-nomem → ublksrv-null | +13.05 | +21.00 | +59058.38 | +1108.91 | 1.593 / 5.255 / 4.895 / 5.109 |
| data cost | ublksrv-null → ublksrv-loop | +7.27 | +9.30 | -389248.41 | +1698.66 | 1.593 / 5.255 / 4.895 / 5.109 |

### 4k-randwrite, requested queues 2, depth 64

| Rung | Family / mode | Implementation | Accepted Q/depth | Semantics | N | qd1 p50 µs | qd1 p99 µs | IOPS | CPU-ns/IO |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| L0 | kernel | null-blk-nomem | 2/64 | null | 3 | 0.11 | 0.12 | 665755.53 | 3008.38 |
| L1 | kernel | brd | fixed/fixed | retaining-ram | 3 | 0.11 | 0.13 | 689646.76 | 2903.98 |
| L1 | kernel | null-blk | 2/64 | retaining-ram | 3 | 0.11 | 0.13 | 506170.70 | 3960.83 |
| L2 | go-ublk / goroutine | go-ublk-null | 2/64 | null | 3 | 35.41 | 58.28 | 606231.64 | 5959.48 |
| L3 | go-ublk / goroutine | go-ublk | 2/64 | retaining-ram | 3 | 36.78 | 62.81 | 558978.76 | 7142.78 |
| L2 | go-ublk / inline | go-ublk-null-inline | 2/64 | null | 3 | 13.89 | 23.59 | 706909.94 | 4279.23 |
| L3 | go-ublk / inline | go-ublk-inline | 2/64 | retaining-ram | 3 | 14.53 | 24.96 | 591022.87 | 4640.91 |
| L2 | lib-ublk | lib-ublk-null | 2/64 | null | 3 | 19.75 | 36.86 | 275901.24 | 7828.04 |
| L3 | lib-ublk | lib-ublk | 2/64 | retaining-ram | 3 | 20.35 | 29.31 | 251613.09 | 8092.99 |
| L2 | libublk-rs | libublk-rs-null | 2/64 | null | 3 | 13.89 | 21.80 | 706629.18 | 4257.47 |
| L3 | libublk-rs | libublk-rs | 1/128 | retaining-ram | 3 | 14.61 | 22.49 | 670473.17 | 4362.64 |
| L2 | ublksrv | ublksrv-null | 2/64 | null | 3 | 13.16 | 21.12 | 712006.11 | 4223.56 |
| L3 | ublksrv | ublksrv-loop | 2/64 | retaining-ram | 3 | 20.44 | 30.42 | 367190.94 | 5792.74 |

| Delta | Lower → upper | qd1 p50 Δ µs | qd1 p99 Δ µs | IOPS Δ | CPU-ns/IO Δ | A/A floor % (p50 / p99 / IOPS / CPU) |
|---|---|---:|---:|---:|---:|---|
| kernel data cost | null-blk-nomem → null-blk | +0.00 | +0.00 | -159584.83 | +952.46 | 1.593 / 5.255 / 5.178 / 5.669 |
| kernel data cost | null-blk-nomem → brd | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 5.178 / 5.669 |
| transport tax | null-blk-nomem → go-ublk-null | +35.30 | +58.16 | -59523.89 | +2951.10 | 1.593 / 5.255 / 5.178 / 5.669 |
| data cost | go-ublk-null → go-ublk | +1.37 | +4.52 | -47252.88 | +1183.30 | 1.593 / 5.255 / 5.178 / 5.669 |
| transport tax | null-blk-nomem → go-ublk-null-inline | +13.78 | +23.47 | +41154.41 | +1270.85 | 1.593 / 5.255 / 5.178 / 5.669 |
| data cost | go-ublk-null-inline → go-ublk-inline | +0.64 | +1.37 | -115887.07 | +361.68 | 1.593 / 5.255 / 5.178 / 5.669 |
| transport tax | null-blk-nomem → lib-ublk-null | +19.64 | +36.74 | -389854.29 | +4819.67 | 1.593 / 5.255 / 5.178 / 5.669 |
| data cost | lib-ublk-null → lib-ublk | +0.60 | -7.55 | -24288.14 | +264.94 | 1.593 / 5.255 / 5.178 / 5.669 |
| transport tax | null-blk-nomem → libublk-rs-null | +13.78 | +21.68 | +40873.65 | +1249.09 | 1.593 / 5.255 / 5.178 / 5.669 |
| data cost | libublk-rs-null → libublk-rs | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 5.178 / 5.669 |
| transport tax | null-blk-nomem → ublksrv-null | +13.05 | +21.00 | +46250.58 | +1215.19 | 1.593 / 5.255 / 5.178 / 5.669 |
| data cost | ublksrv-null → ublksrv-loop | +7.27 | +9.30 | -344815.17 | +1569.18 | 1.593 / 5.255 / 5.178 / 5.669 |

### 4k-randrw-70r30w, requested queues 2, depth 64

| Rung | Family / mode | Implementation | Accepted Q/depth | Semantics | N | qd1 p50 µs | qd1 p99 µs | IOPS | CPU-ns/IO |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| L0 | kernel | null-blk-nomem | 2/64 | null | 3 | 0.11 | 0.12 | 673483.59 | 2976.25 |
| L1 | kernel | brd | fixed/fixed | retaining-ram | 3 | 0.11 | 0.13 | 722591.23 | 2770.78 |
| L1 | kernel | null-blk | 2/64 | retaining-ram | 3 | 0.11 | 0.13 | 514477.61 | 3895.04 |
| L2 | go-ublk / goroutine | go-ublk-null | 2/64 | null | 3 | 35.41 | 58.28 | 605853.07 | 5864.79 |
| L3 | go-ublk / goroutine | go-ublk | 2/64 | retaining-ram | 3 | 36.78 | 62.81 | 560703.84 | 7041.87 |
| L2 | go-ublk / inline | go-ublk-null-inline | 2/64 | null | 3 | 13.89 | 23.59 | 730038.46 | 4145.17 |
| L3 | go-ublk / inline | go-ublk-inline | 2/64 | retaining-ram | 3 | 14.53 | 24.96 | 598136.86 | 4609.34 |
| L2 | lib-ublk | lib-ublk-null | 2/64 | null | 3 | 19.75 | 36.86 | 278881.15 | 7531.09 |
| L3 | lib-ublk | lib-ublk | 2/64 | retaining-ram | 3 | 20.35 | 29.31 | 262404.24 | 7728.19 |
| L2 | libublk-rs | libublk-rs-null | 2/64 | null | 3 | 13.89 | 21.80 | 725685.49 | 4146.05 |
| L3 | libublk-rs | libublk-rs | 1/128 | retaining-ram | 3 | 14.61 | 22.49 | 629258.92 | 4489.11 |
| L2 | ublksrv | ublksrv-null | 2/64 | null | 3 | 13.16 | 21.12 | 731771.42 | 4111.79 |
| L3 | ublksrv | ublksrv-loop | 2/64 | retaining-ram | 3 | 20.44 | 30.42 | 362288.20 | 5790.70 |

| Delta | Lower → upper | qd1 p50 Δ µs | qd1 p99 Δ µs | IOPS Δ | CPU-ns/IO Δ | A/A floor % (p50 / p99 / IOPS / CPU) |
|---|---|---:|---:|---:|---:|---|
| kernel data cost | null-blk-nomem → null-blk | +0.00 | +0.00 | -159005.98 | +918.79 | 1.593 / 5.255 / 4.878 / 5.052 |
| kernel data cost | null-blk-nomem → brd | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 4.878 / 5.052 |
| transport tax | null-blk-nomem → go-ublk-null | +35.30 | +58.16 | -67630.52 | +2888.54 | 1.593 / 5.255 / 4.878 / 5.052 |
| data cost | go-ublk-null → go-ublk | +1.37 | +4.52 | -45149.23 | +1177.08 | 1.593 / 5.255 / 4.878 / 5.052 |
| transport tax | null-blk-nomem → go-ublk-null-inline | +13.78 | +23.47 | +56554.87 | +1168.93 | 1.593 / 5.255 / 4.878 / 5.052 |
| data cost | go-ublk-null-inline → go-ublk-inline | +0.64 | +1.37 | -131901.61 | +464.17 | 1.593 / 5.255 / 4.878 / 5.052 |
| transport tax | null-blk-nomem → lib-ublk-null | +19.64 | +36.74 | -394602.44 | +4554.84 | 1.593 / 5.255 / 4.878 / 5.052 |
| data cost | lib-ublk-null → lib-ublk | +0.60 | -7.55 | -16476.91 | +197.10 | 1.593 / 5.255 / 4.878 / 5.052 |
| transport tax | null-blk-nomem → libublk-rs-null | +13.78 | +21.68 | +52201.90 | +1169.81 | 1.593 / 5.255 / 4.878 / 5.052 |
| data cost | libublk-rs-null → libublk-rs | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 4.878 / 5.052 |
| transport tax | null-blk-nomem → ublksrv-null | +13.05 | +21.00 | +58287.83 | +1135.54 | 1.593 / 5.255 / 4.878 / 5.052 |
| data cost | ublksrv-null → ublksrv-loop | +7.27 | +9.30 | -369483.21 | +1678.92 | 1.593 / 5.255 / 4.878 / 5.052 |

### 4k-randread-qd1, requested queues 2, depth 64

| Rung | Family / mode | Implementation | Accepted Q/depth | Semantics | N | qd1 p50 µs | qd1 p99 µs | IOPS | CPU-ns/IO |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| L0 | kernel | null-blk-nomem | 2/64 | null | 3 | 0.11 | 0.12 | 352763.56 | 2843.51 |
| L1 | kernel | brd | fixed/fixed | retaining-ram | 3 | 0.11 | 0.13 | 380625.80 | 2633.17 |
| L1 | kernel | null-blk | 2/64 | retaining-ram | 3 | 0.11 | 0.13 | 264838.67 | 3788.45 |
| L2 | go-ublk / goroutine | go-ublk-null | 2/64 | null | 3 | 35.41 | 58.28 | 23814.19 | 35891.69 |
| L3 | go-ublk / goroutine | go-ublk | 2/64 | retaining-ram | 3 | 36.78 | 62.81 | 23096.15 | 37467.76 |
| L2 | go-ublk / inline | go-ublk-null-inline | 2/64 | null | 3 | 13.89 | 23.59 | 50519.50 | 15150.71 |
| L3 | go-ublk / inline | go-ublk-inline | 2/64 | retaining-ram | 3 | 14.53 | 24.96 | 48609.34 | 15880.90 |
| L2 | lib-ublk | lib-ublk-null | 2/64 | null | 3 | 19.75 | 36.86 | 38408.49 | 24293.14 |
| L3 | lib-ublk | lib-ublk | 2/64 | retaining-ram | 3 | 20.35 | 29.31 | 38604.82 | 23969.99 |
| L2 | libublk-rs | libublk-rs-null | 2/64 | null | 3 | 13.89 | 21.80 | 50698.83 | 14613.31 |
| L3 | libublk-rs | libublk-rs | 1/128 | retaining-ram | 3 | 14.61 | 22.49 | 50846.33 | 23009.03 |
| L2 | ublksrv | ublksrv-null | 2/64 | null | 3 | 13.16 | 21.12 | 53618.85 | 13611.16 |
| L3 | ublksrv | ublksrv-loop | 2/64 | retaining-ram | 3 | 20.44 | 30.42 | 38252.16 | 22202.17 |

| Delta | Lower → upper | qd1 p50 Δ µs | qd1 p99 Δ µs | IOPS Δ | CPU-ns/IO Δ | A/A floor % (p50 / p99 / IOPS / CPU) |
|---|---|---:|---:|---:|---:|---|
| kernel data cost | null-blk-nomem → null-blk | +0.00 | +0.00 | -87924.89 | +944.94 | 1.593 / 5.255 / 1.706 / 1.946 |
| kernel data cost | null-blk-nomem → brd | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 1.706 / 1.946 |
| transport tax | null-blk-nomem → go-ublk-null | +35.30 | +58.16 | -328949.37 | +33048.17 | 1.593 / 5.255 / 1.706 / 1.946 |
| data cost | go-ublk-null → go-ublk | +1.37 | +4.52 | -718.04 | +1576.08 | 1.593 / 5.255 / 1.706 / 1.946 |
| transport tax | null-blk-nomem → go-ublk-null-inline | +13.78 | +23.47 | -302244.06 | +12307.19 | 1.593 / 5.255 / 1.706 / 1.946 |
| data cost | go-ublk-null-inline → go-ublk-inline | +0.64 | +1.37 | -1910.16 | +730.19 | 1.593 / 5.255 / 1.706 / 1.946 |
| transport tax | null-blk-nomem → lib-ublk-null | +19.64 | +36.74 | -314355.07 | +21449.63 | 1.593 / 5.255 / 1.706 / 1.946 |
| data cost | lib-ublk-null → lib-ublk | +0.60 | -7.55 | +196.33 | -323.15 | 1.593 / 5.255 / 1.706 / 1.946 |
| transport tax | null-blk-nomem → libublk-rs-null | +13.78 | +21.68 | -302064.72 | +11769.79 | 1.593 / 5.255 / 1.706 / 1.946 |
| data cost | libublk-rs-null → libublk-rs | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 1.706 / 1.946 |
| transport tax | null-blk-nomem → ublksrv-null | +13.05 | +21.00 | -299144.71 | +10767.64 | 1.593 / 5.255 / 1.706 / 1.946 |
| data cost | ublksrv-null → ublksrv-loop | +7.27 | +9.30 | -15366.69 | +8591.01 | 1.593 / 5.255 / 1.706 / 1.946 |

### 128k-read, requested queues 2, depth 64

| Rung | Family / mode | Implementation | Accepted Q/depth | Semantics | N | qd1 p50 µs | qd1 p99 µs | IOPS | CPU-ns/IO |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| L0 | kernel | null-blk-nomem | 2/64 | null | 3 | 0.11 | 0.12 | 129849.43 | 15801.72 |
| L1 | kernel | brd | fixed/fixed | retaining-ram | 3 | 0.11 | 0.13 | 72909.54 | 13793.34 |
| L1 | kernel | null-blk | 2/64 | retaining-ram | 3 | 0.11 | 0.13 | 49726.64 | 29263.48 |
| L2 | go-ublk / goroutine | go-ublk-null | 2/64 | null | 3 | 35.41 | 58.28 | 103221.81 | 19318.11 |
| L3 | go-ublk / goroutine | go-ublk | 2/64 | retaining-ram | 3 | 36.78 | 62.81 | 91183.25 | 29801.27 |
| L2 | go-ublk / inline | go-ublk-null-inline | 2/64 | null | 3 | 13.89 | 23.59 | 170845.21 | 11669.26 |
| L3 | go-ublk / inline | go-ublk-inline | 2/64 | retaining-ram | 3 | 14.53 | 24.96 | 78383.49 | 19054.49 |
| L2 | lib-ublk | lib-ublk-null | 2/64 | null | 3 | 19.75 | 36.86 | 92435.17 | 17368.38 |
| L3 | lib-ublk | lib-ublk | 2/64 | retaining-ram | 3 | 20.35 | 29.31 | 69112.90 | 20963.75 |
| L2 | libublk-rs | libublk-rs-null | 2/64 | null | 3 | 13.89 | 21.80 | 180301.54 | 11082.54 |
| L3 | libublk-rs | libublk-rs | 1/128 | retaining-ram | 3 | 14.61 | 22.49 | 80497.08 | 18495.84 |
| L2 | ublksrv | ublksrv-null | 2/64 | null | 3 | 13.16 | 21.12 | 178402.62 | 11166.77 |
| L3 | ublksrv | ublksrv-loop | 2/64 | retaining-ram | 3 | 20.44 | 30.42 | 48941.74 | 27921.29 |

| Delta | Lower → upper | qd1 p50 Δ µs | qd1 p99 Δ µs | IOPS Δ | CPU-ns/IO Δ | A/A floor % (p50 / p99 / IOPS / CPU) |
|---|---|---:|---:|---:|---:|---|
| kernel data cost | null-blk-nomem → null-blk | +0.00 | +0.00 | -80122.79 | +13461.77 | 1.593 / 5.255 / 6.232 / 8.503 |
| kernel data cost | null-blk-nomem → brd | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 6.232 / 8.503 |
| transport tax | null-blk-nomem → go-ublk-null | +35.30 | +58.16 | -26627.62 | +3516.39 | 1.593 / 5.255 / 6.232 / 8.503 |
| data cost | go-ublk-null → go-ublk | +1.37 | +4.52 | -12038.55 | +10483.16 | 1.593 / 5.255 / 6.232 / 8.503 |
| transport tax | null-blk-nomem → go-ublk-null-inline | +13.78 | +23.47 | +40995.78 | -4132.45 | 1.593 / 5.255 / 6.232 / 8.503 |
| data cost | go-ublk-null-inline → go-ublk-inline | +0.64 | +1.37 | -92461.72 | +7385.23 | 1.593 / 5.255 / 6.232 / 8.503 |
| transport tax | null-blk-nomem → lib-ublk-null | +19.64 | +36.74 | -37414.26 | +1566.67 | 1.593 / 5.255 / 6.232 / 8.503 |
| data cost | lib-ublk-null → lib-ublk | +0.60 | -7.55 | -23322.27 | +3595.37 | 1.593 / 5.255 / 6.232 / 8.503 |
| transport tax | null-blk-nomem → libublk-rs-null | +13.78 | +21.68 | +50452.10 | -4719.18 | 1.593 / 5.255 / 6.232 / 8.503 |
| data cost | libublk-rs-null → libublk-rs | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 6.232 / 8.503 |
| transport tax | null-blk-nomem → ublksrv-null | +13.05 | +21.00 | +48553.19 | -4634.94 | 1.593 / 5.255 / 6.232 / 8.503 |
| data cost | ublksrv-null → ublksrv-loop | +7.27 | +9.30 | -129460.88 | +16754.51 | 1.593 / 5.255 / 6.232 / 8.503 |

### 128k-write, requested queues 2, depth 64

| Rung | Family / mode | Implementation | Accepted Q/depth | Semantics | N | qd1 p50 µs | qd1 p99 µs | IOPS | CPU-ns/IO |
|---|---|---|---|---|---:|---:|---:|---:|---:|
| L0 | kernel | null-blk-nomem | 2/64 | null | 3 | 0.11 | 0.12 | 137127.06 | 14520.01 |
| L1 | kernel | brd | fixed/fixed | retaining-ram | 3 | 0.11 | 0.13 | 54973.74 | 18310.74 |
| L1 | kernel | null-blk | 2/64 | retaining-ram | 3 | 0.11 | 0.13 | 45118.68 | 31486.75 |
| L2 | go-ublk / goroutine | go-ublk-null | 2/64 | null | 3 | 35.41 | 58.28 | 107631.29 | 19423.87 |
| L3 | go-ublk / goroutine | go-ublk | 2/64 | retaining-ram | 3 | 36.78 | 62.81 | 71838.28 | 38712.36 |
| L2 | go-ublk / inline | go-ublk-null-inline | 2/64 | null | 3 | 13.89 | 23.59 | 168184.14 | 11590.94 |
| L3 | go-ublk / inline | go-ublk-inline | 2/64 | retaining-ram | 3 | 14.53 | 24.96 | 54817.48 | 24260.37 |
| L2 | lib-ublk | lib-ublk-null | 2/64 | null | 3 | 19.75 | 36.86 | 108871.65 | 15475.19 |
| L3 | lib-ublk | lib-ublk | 2/64 | retaining-ram | 3 | 20.35 | 29.31 | 52655.65 | 25534.57 |
| L2 | libublk-rs | libublk-rs-null | 2/64 | null | 3 | 13.89 | 21.80 | 162102.66 | 11596.76 |
| L3 | libublk-rs | libublk-rs | 1/128 | retaining-ram | 3 | 14.61 | 22.49 | 55971.78 | 23835.14 |
| L2 | ublksrv | ublksrv-null | 2/64 | null | 3 | 13.16 | 21.12 | 177472.97 | 11090.52 |
| L3 | ublksrv | ublksrv-loop | 2/64 | retaining-ram | 3 | 20.44 | 30.42 | 38508.28 | 32107.40 |

| Delta | Lower → upper | qd1 p50 Δ µs | qd1 p99 Δ µs | IOPS Δ | CPU-ns/IO Δ | A/A floor % (p50 / p99 / IOPS / CPU) |
|---|---|---:|---:|---:|---:|---|
| kernel data cost | null-blk-nomem → null-blk | +0.00 | +0.00 | -92008.38 | +16966.74 | 1.593 / 5.255 / 14.368 / 6.219 |
| kernel data cost | null-blk-nomem → brd | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 14.368 / 6.219 |
| transport tax | null-blk-nomem → go-ublk-null | +35.30 | +58.16 | -29495.77 | +4903.86 | 1.593 / 5.255 / 14.368 / 6.219 |
| data cost | go-ublk-null → go-ublk | +1.37 | +4.52 | -35793.01 | +19288.49 | 1.593 / 5.255 / 14.368 / 6.219 |
| transport tax | null-blk-nomem → go-ublk-null-inline | +13.78 | +23.47 | +31057.09 | -2929.08 | 1.593 / 5.255 / 14.368 / 6.219 |
| data cost | go-ublk-null-inline → go-ublk-inline | +0.64 | +1.37 | -113366.66 | +12669.43 | 1.593 / 5.255 / 14.368 / 6.219 |
| transport tax | null-blk-nomem → lib-ublk-null | +19.64 | +36.74 | -28255.41 | +955.18 | 1.593 / 5.255 / 14.368 / 6.219 |
| data cost | lib-ublk-null → lib-ublk | +0.60 | -7.55 | -56216.01 | +10059.38 | 1.593 / 5.255 / 14.368 / 6.219 |
| transport tax | null-blk-nomem → libublk-rs-null | +13.78 | +21.68 | +24975.60 | -2923.25 | 1.593 / 5.255 / 14.368 / 6.219 |
| data cost | libublk-rs-null → libublk-rs | n/a | n/a | n/a | n/a | 1.593 / 5.255 / 14.368 / 6.219 |
| transport tax | null-blk-nomem → ublksrv-null | +13.05 | +21.00 | +40345.91 | -3429.49 | 1.593 / 5.255 / 14.368 / 6.219 |
| data cost | ublksrv-null → ublksrv-loop | +7.27 | +9.30 | -138964.69 | +21016.88 | 1.593 / 5.255 / 14.368 / 6.219 |

## A/A noise and throughput ranking

Noise = max(initial A/A symmetric percentage gap, RMS of at least three interleaved A/A gaps), independently for each queue setting and workload. A gap uses the mean of the two rates as its denominator. A winner requires a strictly greater gap than 2× this floor, at least three rounds, all planned observations, matched accepted geometry and data semantics, checked source pins, an enforced cpuset, and a quiet-window attestation. This threshold is a noise guard, not a statistical significance test. QD1 is also ranked by throughput; tails and CPU are shown above.

| Requested Q | Workload | A/A pairs + pilot | Noise floor % | Ranked matched userspace peers | Gap % | Result |
|---:|---|---:|---:|---|---:|---|
| 2 | 4k-randread | 0 + pilot | n/a | libublk-rs (656377.1) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randread | 0 + pilot | n/a | ublksrv-null (756632.5) > libublk-rs-null (751428.3) > go-ublk-null-inline (750904.2) > go-ublk-null (627270.7) > lib-ublk-null (301149.7) | 0.690 | No winner: evidence gates incomplete |
| 2 | 4k-randread | 3 + pilot | 4.895 | go-ublk-inline (601819.0) > go-ublk (568548.0) > ublksrv-loop (367384.1) > lib-ublk (283428.3) | 5.686 | No winner: evidence gates incomplete |
| 2 | 4k-randwrite | 0 + pilot | n/a | libublk-rs (670473.2) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randwrite | 0 + pilot | n/a | ublksrv-null (712006.1) > go-ublk-null-inline (706909.9) > libublk-rs-null (706629.2) > go-ublk-null (606231.6) > lib-ublk-null (275901.2) | 0.718 | No winner: evidence gates incomplete |
| 2 | 4k-randwrite | 3 + pilot | 5.178 | go-ublk-inline (591022.9) > go-ublk (558978.8) > ublksrv-loop (367190.9) > lib-ublk (251613.1) | 5.573 | No winner: evidence gates incomplete |
| 2 | 4k-randrw-70r30w | 0 + pilot | n/a | libublk-rs (629258.9) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randrw-70r30w | 0 + pilot | n/a | ublksrv-null (731771.4) > go-ublk-null-inline (730038.5) > libublk-rs-null (725685.5) > go-ublk-null (605853.1) > lib-ublk-null (278881.2) | 0.237 | No winner: evidence gates incomplete |
| 2 | 4k-randrw-70r30w | 3 + pilot | 4.878 | go-ublk-inline (598136.9) > go-ublk (560703.8) > ublksrv-loop (362288.2) > lib-ublk (262404.2) | 6.460 | No winner: evidence gates incomplete |
| 2 | 4k-randread-qd1 | 0 + pilot | n/a | libublk-rs (50846.3) | n/a | No winner: insufficient matched peers |
| 2 | 4k-randread-qd1 | 0 + pilot | n/a | ublksrv-null (53618.8) > libublk-rs-null (50698.8) > go-ublk-null-inline (50519.5) > lib-ublk-null (38408.5) > go-ublk-null (23814.2) | 5.598 | No winner: evidence gates incomplete |
| 2 | 4k-randread-qd1 | 3 + pilot | 1.706 | go-ublk-inline (48609.3) > lib-ublk (38604.8) > ublksrv-loop (38252.2) > go-ublk (23096.1) | 22.942 | No winner: evidence gates incomplete |
| 2 | 128k-read | 0 + pilot | n/a | libublk-rs (10550913003.7) | n/a | No winner: insufficient matched peers |
| 2 | 128k-read | 0 + pilot | n/a | libublk-rs-null (23632482854.7) > ublksrv-null (23383587894.0) > go-ublk-null-inline (22393023406.7) > go-ublk-null (13529488743.3) > lib-ublk-null (12115662731.0) | 1.059 | No winner: evidence gates incomplete |
| 2 | 128k-read | 3 + pilot | 6.232 | go-ublk (11951571526.7) > go-ublk-inline (10273880217.0) > lib-ublk (9058766498.0) > ublksrv-loop (6414891380.0) | 15.097 | No winner: evidence gates incomplete |
| 2 | 128k-write | 0 + pilot | n/a | libublk-rs (7336333103.0) | n/a | No winner: insufficient matched peers |
| 2 | 128k-write | 0 + pilot | n/a | ublksrv-null (23261736922.7) > go-ublk-null-inline (22044232027.3) > libublk-rs-null (21247119827.7) > lib-ublk-null (14270025301.7) > go-ublk-null (14107448606.0) | 5.375 | No winner: evidence gates incomplete |
| 2 | 128k-write | 3 + pilot | 14.368 | go-ublk (9415986701.3) > go-ublk-inline (7185036585.7) > lib-ublk (6901680724.3) > ublksrv-loop (5047356912.3) | 26.877 | No winner: evidence gates incomplete |

## Provenance

```json
{
  "binary_sha256": {
    "go-ublk": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "go-ublk-inline": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "go-ublk-null": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "go-ublk-null-inline": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "lib-ublk": "b2c681c66c8912a59ad29aa5f25d1ede09972de6a7fed754cbf6d30e36386458",
    "lib-ublk-null": "3378a78b9c3d9d60766ae190014abffc5eef822a28b7b4a06ea1832b35e6b728",
    "libublk-rs": "f1f1f6f6ea9460f1148724407a7b07269624df89b7951dd397562972d1de896a",
    "libublk-rs-null": "3cd14ea4d87c52d137fe8fe51004efdea1c9dcbc1785f4c05195c2359d2ff843",
    "ublk": "7d9b34c864ce4b2a81849a529669dea1572867fe0850c4c4edbec7648d8999f6",
    "ublksrv-loop": "24d78f067bcd6b0dd239df9d580e1b64b3362baba49a5e55efd2dd32a1f3e14c",
    "ublksrv-null": "88838713c4f40e8fcf2616eb677c6c5288b2ee8bd5735c12002d0680b324908e"
  },
  "brd": {
    "family": "kernel",
    "rung": "L1",
    "semantics": "retaining-ram"
  },
  "cargo": "cargo 1.85.1 (d73d2caf9 2024-12-31)",
  "cpu_budget": "0,1,2,3,4,5,6,7",
  "cpu_budget_enforced": true,
  "cpu_isolation": "disjoint",
  "cpu_model": "AMD Ryzen 9 6900HX with Radeon Graphics",
  "fio": "fio-3.39",
  "fio_cpus": "0,1,2,3",
  "gcc": "gcc (Debian 14.2.0-19) 14.2.0",
  "go-ublk": {
    "artifact": "ublk-mem",
    "family": "go-ublk",
    "mode": "goroutine",
    "rung": "L3",
    "semantics": "retaining-ram",
    "sha256": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "source_sha": "f148e8e3e0f6896e0855ea968dea27b28fbc5e63",
    "target": "go-ublk"
  },
  "go-ublk-inline": {
    "artifact": "ublk-mem",
    "family": "go-ublk",
    "mode": "inline",
    "rung": "L3",
    "semantics": "retaining-ram",
    "sha256": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "source_sha": "f148e8e3e0f6896e0855ea968dea27b28fbc5e63",
    "target": "go-ublk-inline"
  },
  "go-ublk-null": {
    "artifact": "ublk-mem",
    "family": "go-ublk",
    "mode": "goroutine",
    "rung": "L2",
    "semantics": "null",
    "sha256": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "source_sha": "f148e8e3e0f6896e0855ea968dea27b28fbc5e63",
    "target": "go-ublk-null"
  },
  "go-ublk-null-inline": {
    "artifact": "ublk-mem",
    "family": "go-ublk",
    "mode": "inline",
    "rung": "L2",
    "semantics": "null",
    "sha256": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
    "source_sha": "f148e8e3e0f6896e0855ea968dea27b28fbc5e63",
    "target": "go-ublk-null-inline"
  },
  "harness_files_sha256": {
    "bench.sh": "a6440334dad393d6aa018cb4fc772eac539d9cfed250cee1153822cdbe9a7af4",
    "lib/__init__.py": "71c8ec8b4bb85daed8a28f758cd5dc29bcf6f95f84e10f3d5d26092758f32824",
    "lib/build.py": "a24bf0702cc23d1e83fa0b64851893fa334d7c744659951d463c96e6b5ae1ca6",
    "lib/common.py": "92a1b3aa0289df5cef4a85afafeafb3d84e0d800be138c0b6628ae448b71ff4a",
    "lib/cpu.py": "1c83669cbdffbc3f940f8d744acb6a5855bd24950d6877edc46e6c412a546d4b",
    "lib/harness.py": "225675194daf2816140753f20a20a6efc1dd30835a391eddd35392df214f4f15",
    "lib/profile.py": "4580fdaa6de6e26f3a659ea36d5802cd5080e4aafbe4e793ecb9942a8ac054e8",
    "lib/report.py": "371e6e1bcb8bc3949f4f9a5926fb7f8bde23f8890bddceafc472969d6e1809db",
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
    },
    "null-blk-nomem": {
      "kernel": "6.12.111+deb13-cloud-amd64",
      "module_sha256": "6a8dc3083354be4d34926a9dd6778ff74dd397048e8ea04cde8ef24d04e4747c",
      "source_sha": "not exposed by Debian kernel; kernel build and module SHA256 recorded"
    }
  },
  "kernel_build": "#1 SMP PREEMPT_DYNAMIC Debian 6.12.111-1 (2026-09-28)",
  "lib-ublk": {
    "artifact": "lib-ublk-gnu.tar.gz",
    "family": "lib-ublk",
    "rung": "L3",
    "semantics": "retaining-ram",
    "sha256": "3bd4be5cac52402ad281ed277568ed763ae5c495aef8ce706108dd2ff05cdcd6",
    "source_sha": "e0743b3eb034a8343f7762ea25f44f26193e67ce"
  },
  "lib-ublk-null": {
    "artifact": "lib-ublk-gnu.tar.gz",
    "family": "lib-ublk",
    "pure_transport": false,
    "rung": "L2",
    "semantics": "null",
    "sha256": "3bd4be5cac52402ad281ed277568ed763ae5c495aef8ce706108dd2ff05cdcd6",
    "source_sha": "e0743b3eb034a8343f7762ea25f44f26193e67ce",
    "target": "C null; drops writes and zero-fills read buffers"
  },
  "libublk-rs": {
    "add_args": [
      "add",
      "{id}",
      "512"
    ],
    "cargo_lock_sha256": "20259460bf9a25cb972a02925f0ad9dab133d1afd09c0d7eb833437e8d9bd640",
    "example": "ramdisk",
    "family": "libublk-rs",
    "fixed_depth": 128,
    "fixed_queues": 1,
    "lock_origin": "resolved once on VM; replay with saved Cargo.lock",
    "resolved_sha": "479f3097e128d595877185781987d218fe78c047",
    "rung": "L3",
    "semantics": "retaining-ram",
    "sha": "479f3097e128d595877185781987d218fe78c047",
    "source_sha256": "44cd4d7cca9df739a2ab56e8ecda625368e7171e9b60776f5978770afaba4126",
    "submodules": "",
    "tag": "v0.4.6",
    "url": "https://github.com/ublk-org/libublk-rs.git"
  },
  "libublk-rs-null": {
    "add_args": [
      "add",
      "-n",
      "{id}",
      "-q",
      "{q}",
      "-d",
      "{d}",
      "--foreground"
    ],
    "cargo_lock_sha256": "20259460bf9a25cb972a02925f0ad9dab133d1afd09c0d7eb833437e8d9bd640",
    "example": "null",
    "family": "libublk-rs",
    "fixed_capacity": true,
    "lock_origin": "resolved once on VM; replay with saved Cargo.lock",
    "resolved_sha": "479f3097e128d595877185781987d218fe78c047",
    "rung": "L2",
    "semantics": "null",
    "sha": "479f3097e128d595877185781987d218fe78c047",
    "source_sha256": "a50ba3c0c2e34548a48bdaab68c6b962c427f3eba6ad6c96674e6a254443b54e",
    "submodules": "",
    "tag": "v0.4.6",
    "url": "https://github.com/ublk-org/libublk-rs.git"
  },
  "liburing": "2.9",
  "lscpu": "{\n   \"lscpu\": [\n      {\n         \"field\": \"Architecture:\",\n         \"data\": \"x86_64\"\n      },{\n         \"field\": \"CPU op-mode(s):\",\n         \"data\": \"32-bit, 64-bit\"\n      },{\n         \"field\": \"Address sizes:\",\n         \"data\": \"48 bits physical, 48 bits virtual\"\n      },{\n         \"field\": \"Byte Order:\",\n         \"data\": \"Little Endian\"\n      },{\n         \"field\": \"CPU(s):\",\n         \"data\": \"8\"\n      },{\n         \"field\": \"On-line CPU(s) list:\",\n         \"data\": \"0-7\"\n      },{\n         \"field\": \"Vendor ID:\",\n         \"data\": \"AuthenticAMD\"\n      },{\n         \"field\": \"Model name:\",\n         \"data\": \"AMD Ryzen 9 6900HX with Radeon Graphics\"\n      },{\n         \"field\": \"CPU family:\",\n         \"data\": \"25\"\n      },{\n         \"field\": \"Model:\",\n         \"data\": \"68\"\n      },{\n         \"field\": \"Thread(s) per core:\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"Core(s) per socket:\",\n         \"data\": \"8\"\n      },{\n         \"field\": \"Socket(s):\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"Stepping:\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"BogoMIPS:\",\n         \"data\": \"6587.62\"\n      },{\n         \"field\": \"Flags:\",\n         \"data\": \"fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm rep_good nopl xtopology cpuid extd_apicid tsc_known_freq pni pclmulqdq ssse3 fma cx16 sse4_1 sse4_2 x2apic movbe popcnt tsc_deadline_timer aes xsave avx f16c rdrand hypervisor lahf_lm cmp_legacy svm cr8_legacy abm sse4a misalignsse 3dnowprefetch osvw perfctr_core ssbd ibrs ibpb stibp vmmcall fsgsbase tsc_adjust bmi1 avx2 smep bmi2 erms invpcid rdseed adx smap clflushopt clwb sha_ni xsaveopt xsavec xgetbv1 xsaves user_shstk clzero xsaveerptr wbnoinvd arat npt lbrv nrip_save tsc_scale vmcb_clean flushbyasid pausefilter pfthreshold v_vmsave_vmload vgif umip pku ospke vaes vpclmulqdq rdpid overflow_recov succor fsrm\"\n      },{\n         \"field\": \"Virtualization:\",\n         \"data\": \"AMD-V\"\n      },{\n         \"field\": \"Hypervisor vendor:\",\n         \"data\": \"KVM\"\n      },{\n         \"field\": \"Virtualization type:\",\n         \"data\": \"full\"\n      },{\n         \"field\": \"L1d cache:\",\n         \"data\": \"512 KiB (8 instances)\"\n      },{\n         \"field\": \"L1i cache:\",\n         \"data\": \"512 KiB (8 instances)\"\n      },{\n         \"field\": \"L2 cache:\",\n         \"data\": \"4 MiB (8 instances)\"\n      },{\n         \"field\": \"L3 cache:\",\n         \"data\": \"128 MiB (8 instances)\"\n      },{\n         \"field\": \"NUMA node(s):\",\n         \"data\": \"1\"\n      },{\n         \"field\": \"NUMA node0 CPU(s):\",\n         \"data\": \"0-7\"\n      },{\n         \"field\": \"Vulnerability Gather data sampling:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Indirect target selection:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Itlb multihit:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability L1tf:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Mds:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Meltdown:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Mmio stale data:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Reg file data sampling:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Retbleed:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Spec rstack overflow:\",\n         \"data\": \"Mitigation; Safe RET\"\n      },{\n         \"field\": \"Vulnerability Spec store bypass:\",\n         \"data\": \"Mitigation; Speculative Store Bypass disabled via prctl\"\n      },{\n         \"field\": \"Vulnerability Spectre v1:\",\n         \"data\": \"Mitigation; usercopy/swapgs barriers and __user pointer sanitization\"\n      },{\n         \"field\": \"Vulnerability Spectre v2:\",\n         \"data\": \"Mitigation; Retpolines; IBPB conditional; IBRS_FW; STIBP disabled; RSB filling; PBRSB-eIBRS Not affected; BHI Not affected\"\n      },{\n         \"field\": \"Vulnerability Srbds:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Tsa:\",\n         \"data\": \"Mitigation; Clear CPU buffers\"\n      },{\n         \"field\": \"Vulnerability Tsx async abort:\",\n         \"data\": \"Not affected\"\n      },{\n         \"field\": \"Vulnerability Vmscape:\",\n         \"data\": \"Not affected\"\n      }\n   ]\n}\n",
  "machine": "x86_64",
  "null-blk": {
    "family": "kernel",
    "rung": "L1",
    "semantics": "retaining-ram"
  },
  "null-blk-nomem": {
    "family": "kernel",
    "rung": "L0",
    "semantics": "null"
  },
  "packages": "cargo\t1.85.1+dfsg1-1+deb13u1\nfio\t3.39-1\ng++\t4:14.2.0-1\ngcc\t4:14.2.0-1\nlibc6:amd64\t2.41-12+deb13u4\nliburing-dev:amd64\t2.9-1\nrustc\t1.85.1+dfsg1-1+deb13u1\n",
  "rustc": "rustc 1.85.1 (4eb161250 2025-03-15) (built from a source tarball)",
  "server_cpus": "4,5,6,7",
  "server_environment": {
    "GODEBUG": null,
    "GOMAXPROCS": "4",
    "LD_PRELOAD": null,
    "MALLOC_ARENA_MAX": null
  },
  "supplied_inputs": {
    "go-ublk": {
      "artifact": "ublk-mem",
      "sha256": "faa2fce0084d0be5291fef712e8c1076c19000e44e3badeba0ee9e6005dd9897",
      "source_sha": "f148e8e3e0f6896e0855ea968dea27b28fbc5e63"
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
  "ublksrv-loop": {
    "cflags": "-O2 -g0",
    "family": "ublksrv",
    "resolved_sha": "abbfea2b59184b26e7212ce3d4c47702450510df",
    "rung": "L3",
    "semantics": "retaining-ram",
    "sha": "abbfea2b59184b26e7212ce3d4c47702450510df",
    "submodules": "",
    "tag": "v1.8",
    "target": "loop over private tmpfs (buffered backend)",
    "url": "https://github.com/ublk-org/ublksrv.git"
  },
  "ublksrv-null": {
    "cflags": "-O2 -g0",
    "family": "ublksrv",
    "fixed_capacity": true,
    "resolved_sha": "abbfea2b59184b26e7212ce3d4c47702450510df",
    "rung": "L2",
    "semantics": "null",
    "sha": "abbfea2b59184b26e7212ce3d4c47702450510df",
    "size_args": [],
    "source_sha256": "45318250b7dbd7878ea7ff0e2094fb08a6ae915d6eb4c50e00eea738ffa63107",
    "submodules": "",
    "tag": "v1.8",
    "target": "null",
    "url": "https://github.com/ublk-org/ublksrv.git"
  },
  "vcpus": 8,
  "virtualization": "kvm"
}
```

Run context supplied with the completed measurements: the KVM guest was pinned to 4 dedicated physical cores. Fio used vCPUs 0-3, and the server used vCPUs 4-7.

Public extract: numerical results, methodology, source revisions, and binary hashes are preserved. Machine names, local paths, and internal package-validation notes have been omitted. Raw fio files and lifecycle logs are outside this extract.
