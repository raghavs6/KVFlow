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

## Localhost can measure bandwidth, not startup

On a Mac over localhost (about 12 GB/s, which is memory-copy speed, not a
network), the line fit the large sizes within about 1%. It missed the
pass criterion at small sizes: fresh connections were off by 11% at
4 MiB, reused ones by 17% at 1 MiB. Those misses are about 0.02-0.05 ms.

The reason is noise, not shape. Repeats of a 256 MiB transfer spread over
2-4 ms, while the startup being estimated is about 0.1 ms. Least squares
lets the big transfers set the line, and a 2 ms wobble at the far end
swings the intercept by more than the whole startup. A plain fit put the
startup at 0.12 ± 0.12 ms for fresh connections and -0.01 ± 0.24 ms for
reused ones, so neither is distinguishable from zero. It is like finding
a car's weight by weighing a loaded truck with and without it, on a scale
that wobbles by more than the car weighs.

Warm-up matters more than startup here. Keeping round 1 moved the fresh
connection startup from 0.12 to 0.66 ms and made the 1 MiB prediction
101% too slow. The first transfers paid one-time costs that the fit
then spread over every transfer.

So localhost only checks the plumbing and the slope. Whether real
transfers have a startup the learner can find needs a link where startup
is large relative to the noise, like the shaped link planned in Docker.

## Test a measurer on an answer you already know

Localhost couldn't tell whether kvfit finds startups, because the real
startup was smaller than the noise. So we made one: `tc netem` holds
every packet the receiver sends for 10 ms. It is like testing a scale
with a weight you already know.

Even the known weight needed weighing. netem added 1-3 ms on top of what
it was asked for, so ping measured ~12.6 ms, not 10. Checking the setup
first kept kvfit from being blamed for netem's extra.

With reused connections kvfit found 11.5-11.8 ms. The learner works
when the startup stands out from the noise.

## A new TCP connection starts slow

A fresh connection doesn't know how fast the link is, so TCP starts by
sending a little (about 14 KB), waits for "got it", then doubles. This
is called slow start. On a link where each round trip takes 12 ms,
getting 1 MiB through takes about 7 doublings plus the handshake and
the reply, around 10 round trips, or 110-160 ms. A reused connection
has already sped up and sends 1 MiB in about one round trip, 13 ms.

It is like a new employee: the manager checks a small task, then hands
over one twice as big, and so on. An employee who's been there a while
just gets the whole job.

Slow start is why fresh connections don't fit `startup + bytes /
bandwidth`. Time grows in steps with the number of doublings, not
evenly with bytes. The fitted "startup" of 180-220 ms was the line
swallowing slow start, not a real fixed cost. Bursts while speeding up
also lost packets (225 resent, versus 0 on reused connections), which
made repeats jump 2-7x.

The lesson: a model's shape can be right for one way of using a system
and wrong for another. The line is right for warm connections, so KV
transfers should reuse them.

## In one data center, bandwidth is almost everything

Between two VMs in the same AWS zone, a round trip takes 0.25 ms, but
moving 256 MiB takes 225 ms. The fixed startup is so small next to the
per-byte cost that the fit can't tell it apart from 0. It is like a
taxi with a 1-cent pickup fee: the meter is the whole fare.

That changes where the cost model's accuracy matters. In one zone,
getting bandwidth right is what counts. Startup matters only for fresh
connections and small transfers: a fresh 1 MiB transfer took 1.4 ms
against 0.85 ms reused, while from 4 MiB up the two cost the same.

## A dry run doesn't check everything

`aws ec2 run-instances --dry-run` said a c8gn.4xlarge launch "would have
succeeded". The real launch was refused because the free plan only
allows free-tier types. The dry run checks permissions, not every rule.
It is like a door that says your badge works but doesn't know the room
is reserved. A dry run is a hint; only the real call is proof.

## Read the counter instead of guessing from timings

The VMs could burst to 12.5 Gbps and then drop to 0.39 Gbps. Instead of
guessing from slow runs, the network driver keeps a counter,
`bw_out_allowance_exceeded`, that goes up whenever the limit is hit. It
stayed at 0, so we know these runs are burst-rate data. When a system
reports a cause directly, read that before inferring it from effects.

## Check a suspicious zero

`Line` pins a negative startup to 0, so the reused runs printed exactly
0.000 ms. A plain fit gave -0.136 ± 0.324 and -0.012 ± 0.017 ms. Both
agree there is no measurable startup, but the second is below one ping
round trip, which it shouldn't be. The pinned 0 hid that puzzle. A
round number from a clamped calculation should be checked against the
unclamped one.

## Predict first, then learn

A fit that has seen every point, like kvfit, answers "does a line
describe this data?". A policy never has that view. It must guess the
next transfer from the past only. kvreplay tests that honestly: for
each transfer, the learner predicts first and sees the answer second.

It is like grading a weather forecaster only on days that hadn't
happened yet when they made the forecast. If they could see tomorrow
first, every forecast would look perfect. A test that swapped the two
steps made the first prediction exactly right (5 s instead of the 0.1 s
the starting belief gives), which is how the test proves the order.

## A real slowdown, and how fast the learners noticed

When the AWS burst allowance ran out, bandwidth fell 12.8x in one step.
Every learner's next prediction was 90% too fast. Then:

- alpha 0.5 was back within 10% after 4-6 transfers.
- alpha 0.1 took 21.

Alpha is how far each new measurement moves the belief. At 0.5 the
belief moves halfway to what it just saw, so a 12.8x surprise shrinks
fast. At 0.1 it moves a tenth of the way each time. It is the same
lesson kvbench taught, now on a network nobody scripted.

## A good number can be luck, so check the documentation

The first AWS pair moved 9.5 Gbps over one connection. The second moved
4.97 Gbps. The tempting read is that the second pair was slow. The AWS
docs say the opposite: one TCP connection between VMs outside a cluster
placement group is limited to 5 Gbps. The second pair was normal and
the first was lucky.

Like a store that promises delivery in 5 days: one package arriving in
2 days doesn't mean every package will. Plan around the promise, not the
best day. This is the same lesson as "metrics can look good for the
wrong reason": when one result is surprisingly good, find out what was
actually promised before trusting it.
