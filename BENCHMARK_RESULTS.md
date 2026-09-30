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

## Realism check: two AWS VMs in one zone (2026-09-29)

**Setup:** two `c7i-flex.large` (x86, 2 vCPU) in `us-east-2a`, Amazon
Linux 2023, one security group allowing SSH from one IP and all traffic
between its members. kvxfer built on the Mac with `GOOS=linux
GOARCH=amd64` and copied over with scp; traffic uses private IPs. The
receiver ran on A (`172.31.7.13`), the sender on B.

The account is on AWS's free plan, which refused `c8gn.4xlarge`
("not eligible for Free Tier"), even though `--dry-run` said it would
succeed. c7i-flex.large is free-tier eligible but its network is
"up to 12.5 Gbps" with a 0.39 Gbps baseline, so the ENA counter
`bw_out_allowance_exceeded` was read before and after every run to catch
the burst allowance running out.

Launch command:

```sh
aws ec2 run-instances --region us-east-2 \
  --image-id resolve:ssm:/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64 \
  --instance-type c7i-flex.large --count 2 --key-name kvflow \
  --security-group-ids <kvflow sg> --subnet-id <us-east-2a default subnet> \
  --associate-public-ip-address \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=project,Value=kvflow}]' \
                       'ResourceType=volume,Tags=[{Key=project,Value=kvflow}]'
```

**Link:** ping B → A 0.225 min / 0.246 median / 0.498 max ms. MTU 9001.
`tcp_rmem` max 30199872, `tcp_wmem` max 4194304, slow start after idle
on, cubic.

Run order fresh → reuse → reuse-2 → fresh-2, about 1.5 s each. Data:
`results/aws-c7i-flex-same-az/`.

**kvfit, round 1 dropped:**

| run | startup | bandwidth | worst size error |
|---|---|---|---|
| fresh | 0.109 ms | 1.19 GB/s | -34.7% (1 MiB) |
| reuse | 0.000 ms | 1.19 GB/s | +4.0% (1 MiB) |
| reuse-2 | 0.000 ms | 1.19 GB/s | +2.8% (1 MiB) |
| fresh-2 | 0.018 ms | 1.19 GB/s | -41.7% (1 MiB) |

The two exact zeros are `Line` pinning a negative startup, so they were
checked with a plain least-squares fit (round 1 dropped, ± one standard
error): fresh +0.109 ± 0.131, reuse -0.136 ± 0.324, reuse-2 -0.012 ±
0.017, fresh-2 +0.018 ± 0.173 ms.

**Every repeat, ms (rounds 1-5):**

| size | reuse-2 | fresh-2 |
|---|---|---|
| 1 MiB | 1.34, 0.91, 0.81, 0.88, 0.82 | 1.44, 2.08, 1.30, 1.39, 1.40 |
| 4 MiB | 2.82, 3.66, 3.54, 3.49, 3.52 | 2.89, 3.12, 2.91, 3.97, 2.95 |
| 16 MiB | 13.96, 13.92, 14.06, 14.08, 14.10 | 14.08, 13.97, 14.52, 13.74, 13.38 |
| 64 MiB | 56.34, 56.25, 56.36, 56.34, 56.31 | 56.93, 55.79, 57.20, 56.28, 55.60 |
| 256 MiB | 225.29, 225.35, 225.25, 225.32, 225.30 | 227.13, 225.96, 225.41, 226.38, 224.75 |

**Counters on the sender, per run:** `bw_out_allowance_exceeded` stayed
0 through all four runs (~7 GB sent). TCP `RetransSegs` rose by 516
(fresh), 799 (reuse), 917 (reuse-2), 405 (fresh-2).

**What it means:**

- **Reused connections pass the ±10% criterion in both runs** (worst
  +4.0%). This is the first setup where they do.
- **The line is almost all slope.** Bandwidth is 1.19 GB/s (~9.5 Gbps)
  in every run, and 256 MiB repeats agree to 0.1 ms. A 0.25 ms round
  trip is small next to that, and the startup can't be told apart from 0.
- **reuse-2's startup (-0.012 ± 0.017 ms) is below one ping round trip
  (0.25 ms).** Each transfer waits for a 1-byte reply, so at least one
  round trip should show. Not explained. One guess, not checked: ping
  measures an idle link, and a busy one may answer faster.
- **Fresh connections still fail at 1 MiB** (about 1.4 ms vs 0.85 ms
  reused). The extra ~0.6 ms is a few round trips of handshake and slow
  start. It only matters at small sizes; from 4 MiB up, fresh and reused
  cost the same.
- **The burst allowance never ran out**, so this is the burst rate, not
  the 0.39 Gbps baseline. The rate stopped at ~9.5 Gbps, not the listed
  12.5. It is also above what AWS documents for one TCP connection
  between VMs outside a cluster placement group: "bandwidth for
  single-flow traffic is limited to 5 Gbps" ([EC2 network bandwidth
  docs](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-instance-network-bandwidth.html)).
  The next pair of VMs ran at that 5 Gbps (see below), so ~9.5 Gbps was
  probably a lucky placement, not a rate to expect. The docs call burst
  "best effort". Not checked, since these VMs are gone.
- **Retransmits happen here even on reused connections**, unlike Docker,
  yet the times stayed steady. Where they come from wasn't checked.
- **For KVFlow:** in one zone, transfer time is well modeled as bytes /
  bandwidth on a reused connection. Startup matters for fresh connections
  and small transfers.

## A real bandwidth drop: burst allowance running out (2026-09-29)

**Setup:** two fresh `c7i-flex.large` in `us-east-2a`, same launch as above
plus `--instance-initiated-shutdown-behavior terminate` and a boot script
running `shutdown -h +100`, so the VMs delete themselves after 100
minutes even if teardown is forgotten. One reused-connection run:

```sh
kvxfer send -addr <A private IP>:9000 -reuse -duration 75m
```

run detached with `setsid nohup`. Every 5 s a loop logged the time and
`bw_out_allowance_exceeded` on the sender and `bw_in_allowance_exceeded`
on the receiver. The run was stopped at 1263 s, about 10 minutes after
the drop, so the last round is partial. VMs ran ~26 minutes (~$0.07).

Data: `results/aws-c7i-flex-burst/` (`burst.csv`, `counter_out.log`,
`counter_in.log`, `send_start`). kvreplay output is not committed; it is
reproduced with `go run ./cmd/kvreplay < burst.csv`.

**Lining transfers up with the counter:** kvxfer sends back to back, so
the running sum of `seconds` is the time since start. It totals 1261.0 s
against 1263 s of wall time.

**What happened:**

| | before | after |
|---|---|---|
| rounds | 1-1100 | 1102-1185 |
| bandwidth | 0.621 GB/s, every round | 0.0486 GB/s (0.39 Gbps) |
| `bw_out_allowance_exceeded` | 0 | first nonzero at 638 s, then ~27k per 5 s |

- The drop is a sudden step at transfer 5505 (the 256 MiB transfer of
  round 1101), about 12.8x slower. The running sum puts that transfer at
  633-638 s, where the counter first moved.
- After the drop the rate is exactly the listed 0.39 Gbps baseline and
  very steady: 256 MiB took 5546.5-5547.2 ms across rounds 1110-1185.
- The first ~10.6 minutes ran at 0.62 GB/s, which is 4.97 Gbps. That
  matches AWS's documented 5 Gbps limit for a single TCP connection
  between VMs that are not in the same cluster placement group. The
  previous pair got 1.19 GB/s (9.5 Gbps) over the same kind of
  connection, above that limit; this pair is the documented behavior and
  that one the exception. The docs say a cluster placement group allows
  up to 10 Gbps per connection. Neither a placement group nor several
  connections at once was tested.

**Replay (kvreplay, kvbench's alphas and 10 GB/s starting belief):**

| learner | mean abs. error, transfers 6-5500 | transfers after the drop until error stays within ±10% |
|---|---|---|
| line α=0.1 | 1.3% | 21 |
| line α=0.5 | 1.6% | 6 |
| ewma α=0.1 | 1.7% | 21 |
| ewma α=0.5 | 1.9% | 4 |

Every learner predicted the first slow transfer 90% too fast (it took
~10x its prediction). Error after the drop, every 2nd transfer:

| learner | +0 | +2 | +4 | +6 | +10 | +16 | +22 |
|---|---|---|---|---|---|---|---|
| line α=0.1 | -90% | -71% | -68% | -42% | -38% | -14% | -9% |
| line α=0.5 | -90% | -34% | -26% | -1% | -1% | 1% | -0% |
| ewma α=0.1 | -90% | -77% | -62% | -50% | -33% | -17% | -9% |
| ewma α=0.5 | -90% | -30% | -7% | -2% | -0% | 1% | -0% |

**What it means:**

- **On a real, sudden slowdown, alpha 0.5 adapts in 4-6 transfers and
  alpha 0.1 in 21.** This matches kvbench, where alpha 0.5 had the lowest
  regret on every changing workload.
- **Line and EWMA behave almost the same here**, because this link has no
  measurable startup. Only bandwidth changed, and both learn bandwidth.
- **This tests the learner, not the policy.** Every transfer was
  observed. A real policy would stop transferring once recompute looked
  cheaper and would only learn through probes, as kvbench models.
- Only a slowdown was captured. Recovery, the harder case, would need a
  pause long enough for the allowance to refill.

## Recovery: the network getting faster again (2026-09-30)

**Setup:** two fresh `c7i-flex.large` in `us-east-2a`, launched as in the
burst run (self-terminating after 100 minutes, terminated by hand after
~44). Receiver on A (`172.31.7.31`), sender on B. Three phases against
one receiver, each send a new reused connection:

1. Phase A: `kvxfer send -reuse -duration 15m`, to spend the allowance
   and leave the learners settled on "slow".
2. Break: no traffic for 717 s.
3. Phase C: `kvxfer send -reuse -duration 10m`.

Counters were logged every 5 s on both VMs for the whole run. VMs ran
~44 minutes (~$0.12).

Data: `results/aws-c7i-flex-recovery/` (`phaseA.csv`, `phaseC.csv`,
`send_start_a`, `send_start_c`, `counter_in.log`, `counter_out.log`).
Replayed as one stream, the second header dropped:

```sh
(cat phaseA.csv; tail -n +2 phaseC.csv) | go run ./cmd/kvreplay
```

Transfers 1-5785 are Phase A, 5786-6875 Phase C.

**What happened:**

| | Phase A | break | Phase C |
|---|---|---|---|
| fast | 0-325 s at 1.19 GB/s | | 0-44 s at 1.19 GB/s |
| slow | 325-904 s at 0.048 GB/s | | 44-602 s at 0.048 GB/s |
| `bw_in_allowance_exceeded` (A) | first moved at 325 s | flat at 3133110 for all 143 samples | moved again at 44 s |

- This pair ran at 1.19 GB/s (9.5 Gbps), the rate the first pair got and
  above AWS's documented 5 Gbps for one connection. So the "lucky" rate
  happened on 2 of 3 pairs. Planning at 5 Gbps still stands, since it is
  what AWS promises.
- The receiver's `bw_in_allowance_exceeded` counted the throttle; the
  sender's `bw_out_allowance_exceeded` stayed 0. In the burst run it was
  the other way round. Log both sides.
- At twice the rate, the allowance ran out at 325 s instead of 638 s.
  Data sent before the throttle was about the same: ~387 GB here,
  ~396 GB in the burst run. Two runs, so only a hint of a fixed budget.
- The 717 s break bought ~44 s of fast sending (~140 rounds, ~700
  transfers), then the throttle came back.
- Three changes in the replay: slowdown at transfer 5395, speed-up at
  5786 (the first transfer of Phase C), slowdown at 6499. Each slowdown
  is 24.6x (1.19 to 0.0484 GB/s), and so is the speed-up.

**Replay: the two slowdowns** (transfers after the change until error
stays within ±10%):

| learner | slowdown at 5395 | slowdown at 6499 |
|---|---|---|
| line α=0.1 | 22 | 22 |
| line α=0.5 | 6 | 5 |
| ewma α=0.1 | 22 | 23 |
| ewma α=0.5 | 4 | 5 |

The same as the burst run (21 and 4-6), on a 24.6x drop instead of 12.8x.

**Replay: the speed-up.** Every learner predicted the first fast transfer
16x too slow (+1512%). Error after the speed-up, skipping 1 MiB rows
(each +k is the next 4-256 MiB transfer):

| learner | +0 | +4 | +6 | +8 | +10 | +16 | +22 | +30 | +40 | +50 | +60 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| line α=0.1 | +3074% | +2043% | +1367% | +1349% | +843% | +483% | +286% | +99% | +34% | +14% | +5% |
| line α=0.5 | +3059% | +484% | +70% | +50% | +5% | -0% | +0% | -0% | -0% | +3% | +0% |
| ewma α=0.1 | +2779% | +1551% | +1231% | +1018% | +760% | +434% | +231% | +88% | +29% | +12% | +3% |
| ewma α=0.5 | +1587% | +148% | +26% | +8% | +4% | -3% | -5% | -4% | -4% | +2% | -2% |

- **Alpha 0.5 is back within ±10% in ~8-10 transfers; alpha 0.1 in
  ~50-60.** Both are slower than after a slowdown of the same size.
- "Stays within ±10%" doesn't work as a count here. At 1.19 GB/s a 1 MiB
  transfer takes under 1 ms, and 394 of 1078 of them missed ±10% for
  line α=0.5 in the steady fast part of Phase A, when nothing was
  changing. Counting all sizes gave 681 for every learner, which is that
  noise. Without 1 MiB, the last miss was at +9 for line α=0.5 and +54
  for line α=0.1, which is the end of its recovery. The EWMAs still had
  isolated misses long after (last at +141 and +561): from +60 on, 1 and
  8 of 522 transfers, no more often than in the steady fast part of
  Phase A (1.7% and 3.2%). So the table is read instead.

**Why a speed-up takes longer, checked with a noise-free EWMA:** error is
measured against the new time. After a slowdown the old belief is too
small, never more than 100% off, and ~90% of the gap has to close. After
a 24.6x speed-up it starts 2360% off, and the gap has to shrink ~236x.
Each observation closes the same fraction α of the gap, so the second
takes more steps. A single-rate EWMA with no noise needs 23 vs 53
transfers at α=0.1 and 5 vs 9 at α=0.5, matching the replay.

**What it means:**

- **Alpha 0.5 recovers in ~10 transfers on a real speed-up.** With the
  two drops, it is now the faster learner in every real change measured.
- **For a policy, recovery is worse than these numbers.** Every transfer
  here was observed. A policy that stopped transferring during the slow
  period would only see the speed-up through occasional probes, so each
  of these transfers would be one probe. kvbench models this; this data
  doesn't.
- Only one break length (717 s) was tried, so how the refill grows with
  idle time is not measured.
