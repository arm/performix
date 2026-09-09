<!--
SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
SPDX-License-Identifier: Apache-2.0
-->

# Test Case 67: Dependent Traversal Through Oversized Nodes

## Problem Summary
- A serialized traversal through a large shuffled chain of oversized nodes has
  material effective load latency from LLC and DRAM accesses.

## ID
- `test_case_67`

## Public Intent (safe summary)
- Follow a deterministic chain through a large node table. Each oversized node
  contains the value and index needed by the next iteration, while the mapping
  uses verified huge pages to suppress translation pressure.

## What's Wrong In Current Implementation
- `lookup_nodes` cannot determine the next node until the current
  node has been loaded, preventing the processor from overlapping future
  accesses.
- Only the linkage and score fields are hot, but each node also contains
  substantial cold storage. The shuffled traversal therefore has a large
  effective cache footprint and poor cache-line utilisation.

## What The LLM Should Suggest
- Connect the material effective load latency and LLC/DRAM contribution to
  `lookup_nodes`.
- Connect the dependent traversal visible in `lookup_nodes` to its limited
  ability to overlap future accesses.
- Propose an evidence-supported way to reduce the effective memory latency or
  serialized dependency. Packing, separating or reordering hot data is
  acceptable when qualified by the available evidence.
- Do not present huge pages as the primary fix when the sampled TLB evidence is
  insignificant.
- Treat the potential improvement value as an estimate, not measured
  recoverable cycles, elapsed time or speedup.

## Expected Profiling Characteristics
- `lookup_nodes` has enough SPE samples and sampled loads to support
  an application-level diagnosis. The conclusion is not based on a sparse or
  incidental row.
- Its average effective load latency is substantially above the run's ideal L1
  latency, and LLC and DRAM make a meaningful contribution to the all-load
  average.
- Its potential improvement estimate is material relative to other
  application functions, but remains a ranking estimate rather than measured
  performance impact.
- The function has no material TLB-walk evidence relative to its sampled TLB
  accesses.

## Scoring Guidance
- Pass:
  - Identifies the material serialized memory-latency problem, supports it with
    load-latency and memory-level evidence, attributes it to
    `lookup_nodes`, and proposes an appropriately qualified latency or
    traversal improvement.
- Fail:
  - Diagnoses this primarily as a TLB problem or recommends huge pages despite
    insignificant sampled TLB-walk evidence.
  - Gives only generic cache advice without connecting the evidence to the
    dependent traversal.
  - Converts the potential score or sampled measurements into an unsupported
    elapsed-time impact, exact stall total or speedup.
