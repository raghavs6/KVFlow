# Updates

A short log of what changed, newest first. One entry per working day or
major change.

## 2026-09-30

Built the controller, which decides each request with the learner's belief, runs real transfers through `kvworker`, and learns from what the worker measured. It probes when it hasn't transferred lately, records failures without stopping, keeps requests on a fixed schedule, and writes a CSV. `kvcontrol` runs it from the command line.

## 2026-09-29

Built `kvworker`, a gRPC worker that runs a transfer when the controller asks, times the send itself, and returns the seconds. Locked it down without TLS: it listens on localhost by default and sends only to peers on its allowlist. Ran the AWS recovery test: after a 24.6x speed-up, alpha 0.5 caught up in about 10 transfers and alpha 0.1 in about 55, slower than after a slowdown because of how the EWMA closes a gap.

## 2026-09-28

Took `kvxfer` from localhost to Docker (with a 10 ms netem delay as a known-answer test) and then to two AWS VMs in one zone, and the line model held for reused connections. Added `kvreplay` to feed real transfer times to the learners, and recorded a real bandwidth drop on AWS when the burst allowance ran out. Alpha 0.5 adapted in 4-6 transfers.

## 2026-09-27

Built `kvxfer`, a sender/receiver pair that times real TCP transfers, and `kvfit`, which fits those times to a line (startup + bytes / bandwidth) using the same learner the policies use.

## 2026-09-23

Added adaptation time and randomized flapping to `kvbench`, and gave the simulated truth a transfer startup cost that a bandwidth-only model can't see. Built the online line learner (startup + bandwidth) and a drift workload, and compared it against the EWMA.

## 2026-09-22

Split simulated truth from KVFlow's belief, scored decisions by regret, and added the EWMA bandwidth learner with probing and seeded noise. Added the stable, slowdown-recovery and flapping workloads and `kvbench` to compare policies on them.

## 2026-09-21

Built static cost estimates for wait, transfer and recompute, and drove the scheduler from simulated cluster state.

## 2026-09-18 to 2026-09-19

Started the project: wrote PROJECT.md, the README and the workflow notes, and separated cost prediction from scheduling.
