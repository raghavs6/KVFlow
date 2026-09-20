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
