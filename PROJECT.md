KVFlow

Overview

KVFlow is an adaptive control plane for distributed LLM inference that decides the lowest-cost way to obtain KV-cache state needed by an inference worker.

When multiple machines serve the same LLM, a request may need KV-cache state that already exists somewhere else in the cluster.

The system may have several choices:

1. Route the request to the worker that already has the KV cache.
2. Transfer the KV cache to another worker.
3. Restore KV from a local lower storage tier such as CPU memory or NVMe.
4. Recompute the missing KV state on the destination GPU.

The fastest option depends on current system conditions.

KVFlow estimates these costs at runtime and chooses the lowest-latency path.

⸻

The Problem

Consider two inference workers:

Worker A
- Has the required 50k-token KV prefix
- GPU is heavily loaded
- Long request queue
Worker B
- Does not have the KV prefix
- GPU is mostly idle
- No request queue

A new request needs that same 50k-token prefix.

There are several possible strategies:

                     Request
                        |
             +----------+----------+
             |          |          |
             v          v          v
          Wait A    Transfer KV  Recompute
                    A -> B       on B

None is always best.

If the network is fast, transferring the KV cache may be cheapest.

If the network is congested, recomputation may be faster.

If Worker A becomes available quickly, executing directly on A may be best.

The decision depends on runtime conditions.

⸻

Core Idea

KVFlow continuously estimates the cost of different ways of satisfying a request.

Example:

Run on Worker A       ~420 ms
Transfer A -> B       ~140 ms
Recompute on B        ~260 ms
                         ^
                       choose

After executing the decision, KVFlow observes what actually happened.

Predicted transfer: 140 ms
Actual transfer:    175 ms

That measurement is fed back into the cost model.

The core loop is:

Predict
   |
   v
Decide
   |
   v
Execute
   |
   v
Measure
   |
   v
Update Model
   |
   +------> Predict

KVFlow therefore adapts when network, compute, or worker conditions change.

⸻

Key Insight

Existing KV data is not necessarily cheaper to move than to recompute.

Transfer KV:  300 ms
Recompute:    120 ms

In this case, recomputation is preferable even though another worker already performed the computation.

KVFlow attempts to answer:

Given the current state of the cluster, what is the lowest-cost way to obtain the KV state required for this request?

⸻

Architecture

Initial architecture:

                         Request
                            |
                            v
                         Router
                            |
                            v
                     +-------------+
                     |   KVFlow    |
                     |             |
                     | Cost Model  |
                     | Scheduler   |
                     +------+------+
                            |
                  chosen execution plan
                            |
              +-------------+-------------+
              |                           |
              v                           v
          Worker A                    Worker B

KVFlow belongs primarily to the control plane.

It decides what should happen.

Workers belong to the data plane.

They perform expensive operations such as KV transfer, restoration, recomputation, and inference.

⸻

KVFlow Components

Cluster State

Maintains an approximate local view of the inference cluster.

Eventually this may include information such as:

* Worker load
* Request queue depth
* KV-cache locations
* KV-cache size
* Storage tier
* Worker health
* Recent transfer performance
* Recent recomputation performance

KVFlow should not synchronously query every worker before every decision.

Workers instead report state and measurements so KVFlow can make decisions from a local cluster view.

This means the cluster view may occasionally be stale, which is an intentional distributed-systems tradeoff.

Cost Model

Predicts the expected latency of possible operations.

Examples:

transfer(A, B, 2 GB)      -> ~175 ms
recompute(B, 50k tokens)  -> ~240 ms
wait(A)                   -> ~410 ms

The initial model can use simple online estimators such as EWMA.

The model should eventually adapt to changes such as:

* Network congestion
* GPU contention
* Worker load
* Prefix size
* Transfer size
* CPU pressure
* NVMe contention
* Different model characteristics

Scheduler

Uses cost estimates to choose an execution plan.

Example:

transfer:    175 ms
recompute:   240 ms
wait:        410 ms
decision:
TRANSFER A -> B

The scheduler should remain separate from the cost model.

The model predicts.

The scheduler decides.

Workers

Workers execute KVFlow’s decisions.

Initially these will be simulated workers.

Later they may wrap real inference workers such as vLLM instances.

Workers report actual operation measurements back to KVFlow so the cost model can improve.

⸻

Adaptive Cost Modeling

KVFlow should not assume that network or compute performance remains constant.

For example:

A -> B transfer
09:00   10 GB/s
09:05    8 GB/s
09:10    3 GB/s

A startup benchmark would quickly become inaccurate.

Instead, KVFlow observes actual operations.

Example:

Transfer #1: 1 GB -> 105 ms
Transfer #2: 1 GB -> 110 ms
Transfer #3: 1 GB -> 98 ms

The initial implementation can use an exponentially weighted moving average:

new_estimate =
    (1 - alpha) * old_estimate
    + alpha * observation

This allows the system to adapt to persistent changes without overreacting to individual noisy measurements.

More sophisticated models should only be introduced if benchmarks demonstrate that they improve decisions.

⸻

Initial Simulator

The first version will not require GPUs.

KVFlow will run against simulated workers:

                    KVFlow
                   /      \
                  /        \
             Worker A    Worker B

Workers simulate operations such as:

recompute 50k tokens -> ~250 ms
transfer 2 GB A -> B -> ~150 ms

The simulator will allow controlled changes to system conditions.

Example:

Requests 1-1000
network = fast

followed by:

Requests 1001-2000
network = congested

A static policy may continue transferring KV.

KVFlow should observe increasing transfer latency and eventually prefer recomputation when appropriate.

⸻

Baselines

KVFlow should be evaluated against simple policies.

Static Threshold

Use fixed rules determined before runtime.

Example:

if prefix_tokens > threshold:
    transfer
else:
    recompute

Offline Cost Model

Benchmark the system at startup and use those measurements throughout execution.

Adaptive KVFlow

Continuously update cost estimates using observed runtime behavior.

The purpose of the project is to determine when and how much online adaptation improves decisions.

⸻

Evaluation

Important metrics include:

Time to First Token (TTFT)

Time between request arrival and generation of the first output token.

Decision Accuracy

How often KVFlow selects the lowest-cost available option.

Regret

Difference between the cost of KVFlow’s decision and the best available decision.

Example:

KVFlow decision: 80 ms
Optimal choice:  65 ms
regret:          15 ms

Adaptation Time

How quickly KVFlow responds when conditions change.

For example:

fast network
     |
     v
network congestion introduced
     |
     v
how long until KVFlow changes policy?

Additional metrics may include throughput, network utilization, cache hit rate, and prediction error.

⸻

Development Plan

Phase 1 — Simulator

Build:

* Worker model
* Cluster state
* Simple cost model
* Scheduler
* Request generator
* Configurable transfer/recomputation costs

Goal:

Demonstrate an end-to-end decision:

Request
   |
   v
KVFlow
   |
   +-- transfer estimate
   +-- recompute estimate
   +-- wait estimate
   |
   v
choose execution plan

Phase 2 — Adaptive Cost Model

Add runtime observations and EWMA-based estimates.

Introduce controlled changes such as network congestion.

Compare adaptive decisions against static policies.

Phase 3 — Real Networking

Replace simulated transfers with real worker-to-worker communication.

Use gRPC for control-plane communication.

Introduce configurable network conditions and contention.

Phase 4 — Real Inference Measurements

Integrate Python/vLLM workers.

Measure real:

* Prefill/recomputation latency
* KV transfer latency
* Worker load
* TTFT

Phase 5 — Multi-Tier KV

Extend decisions beyond P2P transfer versus recomputation:

GPU
 |
CPU
 |
NVMe
+ remote peers

KVFlow can then choose among:

local GPU
local CPU restore
local NVMe restore
P2P transfer
recomputation

This phase should only be pursued after the simpler system has produced meaningful benchmark results.

⸻

Tech Stack

Core

* Go
* gRPC
* Protocol Buffers

Inference / Benchmark Integration

* Python
* PyTorch
* vLLM

Infrastructure

* Docker
* Linux traffic control (tc)

Observability

Potential later additions:

* Prometheus
* Grafana

Observability tooling should only be added when it provides useful benchmark information.

⸻

Non-Goals

KVFlow is not intended to:

* Build a new LLM inference engine.
* Replace vLLM.
* Implement transformer attention.
* Invent KV caching.
* Build a distributed storage system solely for KV blocks.
* Add infrastructure technologies purely for complexity.
* Guarantee perfectly synchronized cluster state.

The project’s focus is the decision problem around KV data movement and recomputation.

⸻

First Milestone

The first meaningful milestone is:

Given two simulated workers and a request requiring an existing KV prefix, KVFlow can estimate the cost of waiting, transferring, and recomputing and select an execution plan.

No GPU integration is required.

No vLLM integration is required.

No advanced prediction model is required.

The first objective is to make the decision loop correct, understandable, measurable, and testable.

⸻

Guiding Principle

KVFlow should remain centered around one question:

What is the cheapest way to obtain the KV state this request needs under current system conditions?

Every major feature should help answer that question more accurately or execute the resulting decision more effectively.