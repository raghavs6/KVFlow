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
