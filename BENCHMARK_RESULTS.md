# Benchmark Results

Measured numbers, how they were produced, and what they mean. Raw data
lives under `results/`.

## Docker baseline: two containers, no added delay (2026-09-28)

**Setup:** Docker 29.1.3 on a Mac (linux/arm64 VM). `kvxfer recv -addr :9000`
in one container and `kvxfer send` in another, both on a user-defined
bridge network `kvnet`. Image built from `Dockerfile`. Both containers run
in the same Linux VM, so this is still a memory copy, not a network.

**Run order:** fresh → reuse, then reuse-2 → fresh-2. Data:
`results/docker-baseline/`.

**kvfit, round 1 dropped:**

| run | startup | bandwidth | worst size error |
|---|---|---|---|
| fresh | 0.581 ms | 10.85 GB/s | +13.7% (4 MiB) |
| reuse | 0.793 ms | 9.82 GB/s | +268.2% (1 MiB) |
| reuse-2 | 0.090 ms | 12.60 GB/s | +20.0% (1 MiB) |
| fresh-2 | 0.302 ms | 9.83 GB/s | -28.0% (1 MiB) |

**Spread of repeats (all 5 rounds):**

| run | 64 MiB | 256 MiB |
|---|---|---|
| fresh | 5.8-9.2 ms | 24.1-26.7 ms |
| reuse | 5.8-14.2 ms | 23.0-43.6 ms |
| reuse-2 | 5.3-6.2 ms | 20.6-23.0 ms |
| fresh-2 | 5.6-11.0 ms | 22.1-37.7 ms |

**What it means:**

- The plumbing works: 25 rows per run in both modes, and kvfit fits them.
- The noise depends on *when* a run happened, not its mode. The second
  run of each pair was the noisy one, whichever mode it was. One run per
  mode would have wrongly blamed connection reuse.
- The fitted startup ranges from 0.09 to 0.79 ms across runs, so there is
  no measurable startup here, same as on localhost. It is noise.
- For the shaped-link test, an added delay has to be well above ~1 ms to
  stand out from these startups. At 256 MiB, repeats can differ by up to
  ~20 ms, so the large sizes will keep making the intercept wobble.
- Nothing passes the ±10% criterion, and nothing was expected to: there
  is no startup here to find.
