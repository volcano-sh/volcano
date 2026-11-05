# Reporting bugs

Before opening a bug report, reproduce the failure on a real Kubernetes cluster
with Volcano deployed. A local kind or minikube cluster counts. Check for an
existing issue first and report one bug per issue.

Use the bug report template and include:

- The user scenario, actual behavior, and expected behavior.
- Exact Volcano and Kubernetes versions, cluster setup, and relevant configuration.
- Minimal workload manifests and commands to reproduce through normal workloads
  or supported user operations. For intermittent failures, include the frequency
  and test duration or attempt count.
- Actual output from that run: logs, events, resource status, or a stack trace.
  For performance problems, include cluster measurements or profiles, workload
  scale, and the measurement procedure.

Unit tests, mocks, standalone benchmarks, static analysis, AI output, and code path
reasoning cannot replace real-cluster reproduction. Root-cause analysis and a fix
are optional.

Reports without a real-cluster reproduction and its evidence are not accepted and
will be closed. They can be reconsidered once evidence is provided. See
[File an Issue](../../CONTRIBUTING.md#file-an-issue) for the full requirements.
