KVFlow

KVFlow is an adaptive control plane for KV-cache data movement in distributed LLM inference.

When multiple workers serve the same model, a request may need KV state that already exists elsewhere in the cluster. Depending on current system conditions, it may be faster to transfer that KV state, recompute it locally, or execute the request on the worker that already has it.

KVFlow makes this decision using an online cost model that adapts to changing network and compute conditions.

How It Works

                     Request
                        |
                        v
                     KVFlow
                        |
              Estimate runtime cost
                        |
          +-------------+-------------+
          |             |             |
          v             v             v
       Wait/Reuse    Transfer KV    Recompute
       on Worker A     A -> B       on Worker B
          |             |             |
          +-------------+-------------+
                        |
                        v
                Choose fastest path
                        |
                        v
                 Observe result
                        |
                        v
                Update cost model

Instead of relying only on static thresholds or startup benchmarks, KVFlow learns from observed transfer and recomputation latency while the system is running.

For example, if network congestion makes peer-to-peer KV transfer slower than GPU recomputation, KVFlow can adapt and begin preferring recomputation.

Goals

* Model the cost of KV transfer, recomputation, and worker queueing
* Adapt decisions as network and compute conditions change
* Compare adaptive routing against static and offline-calibrated policies
* Measure TTFT, decision accuracy, prediction error, and scheduling regret
* Eventually integrate with real LLM inference workers such as vLLM

Architecture

KVFlow separates the control plane from the data plane.

The control plane maintains cluster state, predicts operation costs, and selects an execution plan. Workers execute KV transfers and inference operations and report observed performance back to KVFlow.

The initial implementation uses simulated workers so policies can be evaluated under controlled network and compute conditions before integrating real GPUs.

Tech Stack

Go · gRPC · Protocol Buffers · Python · vLLM · Docker

Status

🚧 Early development

Current focus: building the distributed worker simulator, cost model, and baseline scheduling policies.
