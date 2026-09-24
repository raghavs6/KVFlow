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
- `transfer = max(QueueB, PrefixTokens * KVBytesPerToken / BandwidthBytesPerSec) + SuffixTokens / PrefillTokensPerSecB`
- `recompute = QueueB + (PrefixTokens + SuffixTokens) / PrefillTokensPerSecB`

Transfer time overlaps Worker B's queue because receiving KV data does not use
B's GPU. Recompute does use B's GPU, so its prefix work starts only after B's
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
