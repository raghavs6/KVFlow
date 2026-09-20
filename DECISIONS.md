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
