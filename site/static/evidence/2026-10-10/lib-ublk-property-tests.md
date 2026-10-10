# lib-ublk seeded property campaign

Recorded 2026-10-10. Public extract of the committed campaign ledger.

Each case initializes Zig 0.15.2 `std.Random.DefaultPrng` from its own seed. Seed ranges are inclusive. The tested source revision is recorded in each row; source release is pending.

| Property | Commit | Seed range | Cases | Seconds | Failures |
|---|---|---|---:|---:|---:|
| decoding | 5f4a920e1a8a0da6b5ee32e91c33715fe78305a4 | 8458442256972513280 - 8458442256972713279 | 200000 | 5.699909 | 0 |
| completions | 5f4a920e1a8a0da6b5ee32e91c33715fe78305a4 | 8458442256972513280 - 8458442256972713279 | 200000 | 5.278619 | 0 |
| lifecycle | 5f4a920e1a8a0da6b5ee32e91c33715fe78305a4 | 8458442256972513280 - 8458442256972713279 | 200000 | 18.661425 | 0 |
| records | 5f4a920e1a8a0da6b5ee32e91c33715fe78305a4 | 8458442256972513280 - 8458442256972713279 | 200000 | 10.264245 | 0 |

Each property also passed a separate maximum-u64-seed replay. This extract covers the committed 200,000-case campaign per property. Earlier development runs are excluded. No new library defect was found in this campaign. Coverage-guided fuzzing is outside this qualification.
