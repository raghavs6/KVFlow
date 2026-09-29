# Decisions

This file records settled engineering decisions and why they were made.

## Scheduler input

The scheduler receives feasible execution candidates with an estimated time to
first token (TTFT). The cost model owns the conversion from raw cluster state to
those estimates; the scheduler only selects the lowest estimate. This keeps the
prediction mechanism replaceable without changing the selection policy.

When estimates tie, the scheduler returns the first candidate. There is no
evidence yet for a secondary objective, so candidate order is the explicit and
deterministic tie-breaker.

## Static cost estimates

The initial cost model produces candidates in the fixed order wait, transfer,
and recompute. Combined with the scheduler's first-candidate tie-breaker, this
means wait wins a three-way tie and transfer wins a tie with recompute.

The model uses these time-to-first-token estimates:

- `wait = QueueA + SuffixTokens / PrefillTokensPerSecA`
- `transfer = max(QueueB, TransferStartup + PrefixTokens * KVBytesPerToken / BandwidthBytesPerSec) + SuffixTokens / PrefillTokensPerSecB`
- `recompute = QueueB + (PrefixTokens + SuffixTokens) / PrefillTokensPerSecB`

Transfer time, including its fixed startup (such as connection setup),
overlaps Worker B's queue because receiving KV data does not use B's GPU. Recompute does use B's GPU, so its prefix work starts only after B's
queue drains and the two times are added. First-token decode time is omitted
because it is the same for every candidate and cannot change their ordering.

## Benchmark parameters

kvbench uses a 1500 ms source queue so that each network phase has a
different best action: transfer when the network is fast (220 ms) and
recompute when it is slow (1070 ms, versus 1520 ms wait and 2020 ms
transfer). With the earlier 400 ms queue, wait beat recompute in every
phase and the benchmark never tested whether recomputing can beat moving
existing KV. A test in `cmd/kvbench` pins both best actions down.

## Adaptation time

Adaptation time is the number of requests after a change in the true best
action until the policy *believes* the new action is best. Runs record the
believed action separately from the executed one, because a forced probe
executes a transfer without the policy changing its mind; counting probes
would report recoveries as adapted when they were not.

Each change point's window ends at the next change point. A belief that only
matches after conditions changed again is stale, not adapted. If the belief
never matches inside the window, the change point is unadapted, and kvbench
prints "never" for the whole cell rather than averaging it away.

Slowdowns (recompute becomes best) and recoveries (transfer becomes best) are
reported separately: ordinary transfers reveal a slowdown, but only probes
can reveal a recovery.

## Randomized flapping

Flapping phases last a random 150-250 requests drawn from a seeded RNG. With
a fixed 200-request period, probing every 101 requests (K=100) landed just
after each recovery and scored 33 ms regret; at fixed periods of 170 and 250
the same policy scored 112-119 ms. Real networks do not change on a schedule,
so the workload should not reward a probe schedule that matches one.

kvbench draws one flapping workload from a fixed seed. Policy rankings were
the same for workload seeds 1-4, but single cells for rarely-probing policies
varied by up to about 50%, so only rankings should be read from one draw.

## Hidden transfer startup in the simulated truth

The simulated truth can charge every transfer a fixed `TransferStartup` that
policies are never told. Truth is the cost model's formula with the true
startup plugged in; beliefs pass their own startup estimate, so a belief can
never pick up the truth's value by copying a scenario. Regret and adaptation
time judge decisions against these true costs, because asking the model for
the best action would hide exactly the error being measured.

Workers report the real transfer duration, startup included, so a cost the
model gets wrong leaks into what the learner sees, as it would in practice.

## Static baseline

Static means a bandwidth measured once at startup and never updated. Queues
and the request are read fresh for each request, the same inputs adaptive
policies get, so the comparison isolates learning. An earlier version froze
the whole scenario, which silently predicted every transfer as a 50k-token
one once prefix sizes varied.

## Line learner

`learner.Line` learns transfer time as `startup + bytes * secondsPerByte` by
fitting a line through observed `(bytes, seconds)` points:

- Exponentially weighted least squares, fading older points by `1 - alpha`,
  so alpha means what it means for the EWMA. It stores weighted means and
  deviations instead of raw sums of squares, which at ~1e18 lose precision.
- A line is fit only when transfer sizes vary by at least 10% of their mean
  (a guess, not a measurement). Below that, sizes 1% apart with 1 ms of
  timing noise would fit a nonsense 300 ms startup.
- Without enough spread it keeps its startup estimate and fits only the
  slope, which makes it behave like the EWMA until it has evidence and lets
  it remember a learned startup once only one size is being transferred.
- A negative startup is pinned to 0; a falling line falls back rather than
  learn a negative bandwidth.

The EWMA is kept as a second `Learner` so kvbench compares the two directly.

## Drift workload

Drift moves seconds per byte, not bandwidth, in equal steps from fast to slow
by mid-run and back, so transfer time changes by the same amount every
request. It uses one prefix size and no startup so drift is the only
variable.

## Measuring real transfers (kvxfer)

kvxfer sends plain TCP, not gRPC. gRPC is for control messages; bulk KV
bytes would need chunking under its 4 MB message limit, and we would
partly measure gRPC instead of the network.

Each transfer is an 8-byte length header, the bytes, and a 1-byte reply
from the receiver after it has read everything. The header lets transfers
share a connection; the reply means the timer stops when data arrives,
not when the OS accepts the writes into its buffer.

`-reuse` keeps one connection for every transfer, dialed before timing.
Without it, every transfer dials inside the timed span and so pays the
handshake and TCP ramp-up. Comparing the two shows whether a startup cost
is real.

Sizes (1-256 MiB) are interleaved within numbered rounds, so a slow moment
on the machine hits every size a little. Every run is printed, including
warm-up, and the `round` column lets analysis drop round 1 explicitly.

## Fitting real transfers (kvfit)

kvfit fits with `learner.Line` at alpha 1e-6, which weights all points
about equally and so is ordinary least squares. This tests the learner the
policies use instead of a second copy of the math. The cost: `Line` pins a
negative startup to 0, so a fit printing exactly 0 startup should be
checked against a plain fit.

Errors are reported per size as a percentage of that size's mean. Least
squares minimizes misses in seconds, so the largest transfers dominate
and one overall score would hide misses on small ones.

The pass criterion, set before the first real run: every size within ±10%
with round 1 dropped, in both connection modes. 10% is about how much
repeats of one size already varied.

## Known-answer test (Docker + netem)

Localhost can't separate a ~0.1 ms startup from noise, so kvxfer runs
between two containers with a delay we set. The delay is `tc netem` on
the receiver's outgoing packets: netem sits under TCP, so TCP reacts to
it as to a slow link, and the receiver stays running so it can be
shaped with `docker exec`. Only the receiver gets `NET_ADMIN`.

The known answer is the round trip ping measures, not the delay typed
into tc. netem adds 1-3 ms of its own (10 ms measures ~12.6 ms).

Runs alternate order (fresh, reuse, reuse, fresh), because the baseline
showed noise follows when a run happened, not its mode.

## The line model is for reused connections

Over the 10 ms link, reused connections fit `startup + bytes *
secondsPerByte` with startup ≈ one round trip (11.5-11.8 ms vs ~12.6
ms). Fresh connections don't fit a line: slow start costs ~10 round
trips even at 1 MiB, and fast retransmits make repeats jump 2-7x. So
the transfer model is only claimed for warm, reused connections, and
real transfers should reuse connections. If a design needs a connection
per transfer, the model needs another shape first. Results:
`BENCHMARK_RESULTS.md`.

## Realism check on AWS

Two VMs in one availability zone, because KVFlow is about workers in one
cluster. Two regions would make startup easy to see, but would measure
cross-country links instead.

The account's free plan only launches free-tier types, so the VMs are
`c7i-flex.large` rather than a fixed-bandwidth type. Its network can
burst and then drop to a 0.39 Gbps baseline, so every run reads the ENA
counter `bw_out_allowance_exceeded` before and after. A run is only
trusted as burst-rate data if that counter didn't move.

kvxfer is built on the Mac for linux/amd64 and copied with scp, so the
VMs need no Docker or Go. Traffic uses private IPs, which are free
within a zone. Everything is tagged `project=kvflow`, and teardown is
checked by searching that tag.

## The line model holds in one zone

On the AWS link, reused connections fit the line within ±4% in both
runs, passing the ±10% criterion. Bandwidth (1.19 GB/s) is the whole
story: startup can't be told apart from 0 by a plain fit. This backs
the decision to model reused connections only. Fresh connections match
reused ones from 4 MiB up and miss at 1 MiB by ~0.6 ms. Results:
`BENCHMARK_RESULTS.md`.
