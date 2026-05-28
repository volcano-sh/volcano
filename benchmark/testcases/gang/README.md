# Volcano Benchmark: Gang Scheduling Testcases

This directory contains benchmark testcases for Volcano gang scheduling and topology-aware scheduling.

## Directory Structure

```text
benchmark/testcases/gang/
├── gang_test.go              # Main benchmark test logic for gang scenarios
├── config/                   # Volcano scheduling and queue configurations
│   ├── queue.yaml            # Test queue definition
│   └── scheduler-config.yaml # Scheduler configuration loaded before tests run
└── cases/                    # Test configuration profiles (YAML)
    ├── case-template.yaml    # Bare-minimum template for testing jobs, replicas, and minAvailable
    ├── net-topo.yaml         # Concrete test case specifically for network topology scheduling
    ├── vcjob-template.yaml   # Base Volcano Job template used by the Go test framework
    └── comprehensive.yaml    # Detailed reference for all supported job parameters
```

## Usage

Run the following commands from the `benchmark/` root directory.

### 1. Run Basic Gang Scheduling Tests

For the most basic benchmark scenarios, you can specify the number of Jobs, Replicas, and MinAvailable directly as environment variables. This bypasses the need to create static configuration files:

```bash
make test-gang-env JOBS=10 REPLICAS=100 MIN_AVAILABLE=100
```
This command injects these core parameters into `cases/case-template.yaml` using `envsubst` to run the test. 

*Note: This template is designed for simplicity and only covers these three basic scaling parameters.*

### 2. Run Network Topology Testing

A simple configuration is provided in `cases/net-topo.yaml` to demonstrate network topology scheduling. You can run it using the generic configuration target:

```bash
make test-config SCENARIO=gang CONFIG=testcases/gang/cases/net-topo.yaml
```

*Note: This is only a minimal test case for Network Topology. For customized topologies, please refer to the complete parameter reference in `cases/comprehensive.yaml` and the job format in `cases/vcjob-template.yaml`.*

### 3. Run Custom Profiles

If you want to create your own customized test profile (e.g., `cases/my-custom-test.yaml`), `cases/comprehensive.yaml` serves as the complete parameter reference. It contains all configurable fields supported by the underlying test framework. You can pick the parameters you need from it to build customized benchmark scenarios.

Run your custom profile via the generic configuration target:

```bash
make test-config SCENARIO=gang CONFIG=cases/my-custom-test.yaml
```