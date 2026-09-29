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

## Adding a known delay with tc netem (2026-09-28)

**Setup:** same image, now with `iproute2` and `iputils-ping`. The receiver
runs with `--cap-add NET_ADMIN` (without it, `tc` is refused), and the
delay goes on its outgoing packets:

```sh
docker exec recv tc qdisc add dev eth0 root netem delay 10ms
```

Only the receiver's packets are held, so every round trip is one delay
longer. Ping runs from a separate container on `kvnet`.

**Ping round trip (20 pings each, first ping dropped):**

| netem delay | min | median | max |
|---|---|---|---|
| none | 0.05 ms | ~0.11 ms (mean) | 0.15 ms |
| 1 ms | 1.15 ms | 2.00 ms | 2.53 ms |
| 5 ms | 5.87 ms | 6.75 ms | 7.51 ms |
| 10 ms | 11.00 ms | 12.60 ms | 13.90 ms |
| 20 ms | 20.80 ms | 22.80 ms | 25.90 ms |

**TCP settings in the container:** `tcp_rmem` max 6291456 (6 MiB),
`tcp_wmem` max 4194304 (4 MiB), `tcp_slow_start_after_idle` 1, congestion
control `cubic`, MTU 1500.

**What it means:**

- The delay is real: the fastest round trip went from 0.05 ms to 11 ms.
- netem adds roughly 1-3 ms on top of what it is asked for, and that extra
  doesn't grow in step with the delay. The cause wasn't checked (a slow
  timer in Docker's VM is one guess). So the known answer to compare kvfit
  against is the measured round trip, ~12.6 ms median at `delay 10ms`,
  not the 10 ms that was typed.
- The first ping from a new container took about two delays (21.5 ms).
  Probably the address lookup reply is delayed too. kvxfer's round 1
  would pay this, which is one more reason to drop it.
- With a ~12.6 ms round trip and a 6 MiB receive window, one connection
  can't go faster than about 6 MiB / 12.6 ms ≈ 0.5 GB/s, far below the
  ~10 GB/s baseline.

## Known-answer test: kvxfer over a 10 ms netem link (2026-09-28)

**Setup:** as above, `netem delay 10ms` on the receiver. Ping measured
11.4-13.6 ms average round trip before and after the runs. Run order
fresh → reuse → reuse-2 → fresh-2. Data: `results/docker-netem-10ms/`.

**Expected:** reused connections pay about 1 round trip of startup
(~12.6 ms) and fit a line; fresh connections pay the handshake plus slow
start and may not fit a line; bandwidth at most ~0.5 GB/s.

**kvfit, round 1 dropped:**

| run | startup | bandwidth | worst size error |
|---|---|---|---|
| reuse | 11.822 ms | 0.33 GB/s | +11.8% (1 MiB) |
| reuse-2 | 11.463 ms | 0.33 GB/s | +15.1% (1 MiB) |
| fresh | 220.382 ms | 0.35 GB/s | +58.9% (1 MiB) |
| fresh-2 | 181.803 ms | 0.29 GB/s | +42.1% (1 MiB) |

Reused connections, every size other than 1 MiB: within ±3.4%.

**Every repeat, ms (rounds 1-5):**

| size | reuse | fresh | fresh-2 |
|---|---|---|---|
| 1 MiB | 101.8, 13.6, 13.8, 13.4, 12.8 | 111.7, 145.6, 122.0, 159.2, 135.6 | 149.0, 120.1, 142.7, 133.2, 125.9 |
| 4 MiB | 35.5, 24.9, 25.9, 24.3, 26.0 | 150.8, 164.2, 291.0, 147.6, 190.1 | 236.2, 145.7, 392.4, 180.6, 443.7 |
| 16 MiB | 60.2, 61.6, 63.1, 66.7, 60.6 | 382.6, 201.2, 529.7, 822.8, 215.4 | 196.9, 260.4, 207.9, 234.2, 230.6 |
| 64 MiB | 211.3, 224.1, 216.8, 216.4, 201.5 | 1418.1, 351.1, 349.4, 338.7, 363.3 | 399.1, 347.6, 359.4, 417.7, 364.0 |
| 256 MiB | 817.7, 826.5, 819.7, 817.4, 823.9 | 1006.5, 1003.6, 995.1, 981.1, 1005.7 | 957.7, 937.3, 1271.3, 1287.3, 993.7 |

**Packet loss checks** (one extra run per mode, counters read inside the
sender before it exited):

- fresh: 225 retransmitted segments, all fast retransmits, 0 timeouts.
- reuse: 0 retransmissions.
- netem on the receiver dropped 0 packets, and the receiver's own TCP
  counters showed no loss. Where the fresh-connection packets are lost
  was not tracked down.

**What it means:**

- **Reused connections pass the known-answer test for startup.** kvfit
  found 11.8 and 11.5 ms against a measured 11-13.6 ms round trip, and
  the two runs agree to 0.4 ms. The learner can find a real startup when
  it is large relative to the noise.
- **They miss ±10% only at 1 MiB** (predicted 15 ms, measured ~13 ms).
  A likely reason, not checked: 1 MiB fits in the window a warmed-up
  connection already has open, so it pays about one round trip and no
  per-byte cost. Below the window size, time is flat, not a line.
- **Fresh connections do not fit a line.** Even without loss spikes,
  1 MiB takes 110-160 ms, about 9-12 round trips, which fits slow start
  (starting near 14 KB and doubling per round trip needs about 7 round
  trips for 1 MiB, plus handshake and reply). On top of that, fast
  retransmits make repeats jump by 2-7x. The fitted "startup" of
  180-220 ms is the line absorbing slow start, not a real fixed cost.
- **Bandwidth is 0.33 GB/s, below the ~0.5 GB/s window limit.** Why it
  stops at 0.33 was not checked.
- **For KVFlow:** the startup + bytes line is a good model for a warm,
  reused connection, and a bad one for a connection opened per transfer.
  Transfers should reuse connections, or the model needs another shape.
