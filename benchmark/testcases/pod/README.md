# Bare Pod Scheduling Benchmark

Benchmarks the Volcano scheduler using bare pods. This can test both the main batch scheduler and the agent-scheduler for bursty and highly latency-sensitive workloads. By default, tests are configured to use the `agent-scheduler`.

## Directory Structure

```text
benchmark/testcases/pod/
├── pod_test.go          # Main benchmark test logic for bare pod scenarios
└── cases/
    ├── case-template.yaml  # envsubst template (used by make test-pod-env)
    └── pod-template.yaml   # Go template for Pod YAML (used by BuildPod)
```

## Usage

Run the following commands from the `benchmark/` root directory.

### 1. Run Basic Pod Scheduling Tests

For the most basic benchmark scenarios, you can specify parameters directly as environment variables. This bypasses the need to create static configuration files:

```bash
make test-pod-env PODS=500 SCHEDULER_NAME=agent-scheduler
```

* `PODS`: Number of bare pods to create (default `500`).
* `SCHEDULER_NAME`: The scheduler to benchmark. Can be `agent-scheduler`, `default-scheduler`, or `volcano` (default `agent-scheduler`).

This command injects these core parameters into `cases/case-template.yaml` using `envsubst` to run the test.

### 2. Run Custom Profiles

If you want to run a customized test profile, you can add your own YAML configuration files under the `cases/` directory.
Take `cases/case-template.yaml` as an example, you can run it using the generic configuration target:
```bash
make test-config SCENARIO=pod CONFIG=cases/case-template.yaml
```
