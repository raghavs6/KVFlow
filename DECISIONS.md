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
