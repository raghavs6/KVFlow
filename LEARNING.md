# Learning Notes

## Cost model and scheduler boundary

The cost model answers, "How long should each feasible action take?" The
scheduler answers, "Which estimate is smallest?"

For example, the model may produce wait = 420 ms, transfer = 140 ms, and
recompute = 260 ms. The scheduler does not need to know how queue depth,
bandwidth, or prefix length produced those numbers. It selects transfer because
140 ms is the smallest predicted TTFT.

This boundary matters because later experiments can replace a static estimate
with an adaptive one while keeping the decision rule unchanged.

## Overlapping work versus serialized work

`max(queue time, transfer time)` describes two jobs that can happen at the same
time. Imagine a 300 ms kitchen timer and a 200 ms laundry timer started
together: after 300 ms, both are done. Likewise, Worker B can drain its GPU
queue while KV bytes arrive over the network. The request waits for whichever
one finishes last, not for their sum.

Recomputation is different. It needs Worker B's GPU, just like the queued work.
The prefix cannot be recomputed until that queue drains, so the model adds the
times: `queue time + recompute time`.

The estimator is stateless: each call turns one snapshot of queue, token,
compute, and network measurements into three candidates without remembering or
changing anything. It is like a calculator, not a history book. A later
adaptive layer can learn better input rates while this estimator remains a
small, deterministic conversion step.

## How often to probe

Once the adaptive policy stops transferring, it stops measuring the network.
A probe is a transfer forced every K requests just to measure again. Two
costs pull K in opposite directions:

- Each probe on a network that is still slow costs the full gap between
  transfer and the best action. On the stable-slow workload that is about
  950 / (K+1) ms per request, paid forever.
- Without probes, a policy that switched away from transfer never learns the
  network recovered. On slowdown+recovery, K=0 pays about 850 ms per request
  for the whole recovery phase.

It is like checking whether a closed shop has reopened. Check every day and
you waste many trips; never check and you miss that it opened. In the current
benchmark K=20 with alpha 0.5 balances these best.

## Metrics can look good for the wrong reason

Adaptation time must be read next to regret. Static shows 0 for recoveries
because it always believes transfer is best: it was never wrong in that
direction, not quick to adapt. K=0 looks fast at later slowdowns on flapping
for the same reason: it is stuck believing recompute.

A benchmark can also reward coincidence. When flapping switched every 200
requests, probing every 101 requests happened to land right after each
recovery and looked like the best policy. Changing only the period exposed
it. When one number is surprisingly good, change something that should not
matter and see if the result survives.

## Why noise sometimes speeds up recovery

With alpha 0.5, the belief usually needs two fast probes to swing back to
transfer. Measurement noise makes the slow-phase belief wobble; when it
happens to sit a little lower, one fast probe is enough. That is why the
measured recovery times were below the noise-free predictions.

## A wrong model shape can hide behind a fixed workload

Real transfers pay a fixed startup plus a per-byte cost. The EWMA learns only
a per-byte cost, but when every transfer is the same size it still predicts
perfectly: it folds the startup into a slightly lower "effective bandwidth".
The mistake only shows once sizes vary. A 200 ms startup spread over a small
80 MB transfer makes bytes look about 13x more expensive, so the EWMA scared
itself off large transfers that were actually best. On mixed sizes it had
283-324 ms regret, five times worse than never learning (59 ms).

It is like estimating taxi fares by price per mile when every ride has a flat
pickup fee. Learn only from short rides and long rides look absurdly
expensive. No single per-mile number can fit both.

The fix was to learn the right shape: a line with an intercept (startup) and
a slope (seconds per byte). With probing it reached 2-14 ms regret on the
same workload. Learning harder does not fix a model that cannot represent
the truth; changing its shape does.

## A learner needs to see the variety it must explain

The line learner can only separate startup from bandwidth after it has
transferred two different sizes. If its first transfer is small, it decides
bytes are expensive, recomputes every large prefix, and never gets the second
point. Without probing it stays stuck (324 ms regret). A probe supplies the
missing point. The data a policy collects depends on its own decisions, so a
bad early belief can starve it of the evidence that would correct it.

## Gradual slowdowns are cheap, gradual recoveries are not

When bandwidth drifts slowly downward, every policy notices within about 5
requests. Ordinary transfers keep measuring the link, and the switch happens
where transfer and recompute cost about the same, so being a little late
costs almost nothing.

Drifting back up is harder than a sudden recovery. After a jump, one probe
sees a fully fast network and moves the belief a lot. During drift, a probe
just past the crossover sees a network only slightly faster than believed,
and alpha 0.1 moves the belief 10% of that small gap. Meanwhile the network
keeps improving. With alpha 0.1 and K=100 the belief never catches up before
the run ends (92 ms regret, about the same as never probing), even though the
same policy recovered from a sudden jump in 410 requests.

Across every changing workload, K=20 with alpha 0.5 has had the lowest
regret. Slowdowns of any shape are easy; speedups are only visible through
probes, and gradual ones need frequent probes or a larger alpha.
