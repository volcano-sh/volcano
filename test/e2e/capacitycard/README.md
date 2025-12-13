# Capacity-Card Plugin Integration Tests

This document describes the integration tests used to verify the functionality of the Volcano scheduler plugin `capacity-card`.

## Test Structure

The integration tests are located in the `/mnt/d/code/volcano/test/e2e/capacitycard/` directory and include the following files:

- `main_test.go`: Test main function that initializes the test environment
- `e2e_test.go`: Test suite registration function
- `capacity_card_test.go`: Main test case implementation
- `README.md`: Test documentation

## Test Features

The test cases cover the following core functionality:

1. **Basic Queue Capacity Management Tests**
   - Verify queue creation and status management with GPU card quota annotations

2. **Job Enqueue Check - Success Cases**
   - Test whether jobs with card resource requests can be enqueued normally
   - Verify the relationship between queue card quotas and job card requests

3. **Task Card Resource Allocation Tests**
   - Verify task-level card name request functionality
   - Test task scheduling with card name annotations

4. **Card Resource Exceeding Quota Tests**
   - Test scenarios where new job requests exceed remaining quota after the queue has already used partial card quota
   - Verify the queue card resource quota limitation mechanism

## How to Run Tests

### Prerequisites

- Kubernetes cluster is deployed
- Volcano is installed
- Scheduler configuration has enabled the capacity-card plugin
- fake-gpu-operator Helm Chart is installed to simulate GPU card resources

### Installing fake-gpu-operator

Label nodes to specify which GPU card resource pool they belong to using the following command:

<node-name> is the node name used to specify which node to label.
<config> is the node labeling configuration used to specify which GPU card resource pool the node belongs to. Refer to the nodePools configuration in fake-gpu-operator/values.yaml.

```bash
kubectl label node <node-name> run.ai/simulated-gpu-node-pool=<config>
```

Before running tests, you need to install the fake-gpu-operator Helm Chart to simulate GPU card resources. You can install it using the following command:
where `--version <VERSION>` is optional

```bash
helm upgrade -i gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator --namespace gpu-operator --create-namespace --version <VERSION> -f ../hack/fake-gpu-operator-values.yaml
```

### Run Commands

According to the project's Makefile structure, the correct run command is as follows:

```bash
cd /mnt/d/code/volcano
E2E_TYPE=CAPACITYCARD ./hack/run-e2e-kind.sh
```

Or you can build the image first and then run:

```bash
cd /mnt/d/code/volcano
make images
E2E_TYPE=CAPACITYCARD ./hack/run-e2e-kind.sh
```

## Test Description

The tests use the Ginkgo and Gomega testing frameworks, following the standard structure of Volcano E2E tests:

1. Each test case creates an independent test namespace
2. Tests create queues with card quota annotations
3. Create jobs containing card resource requests
4. Verify that jobs are created and managed correctly
5. Clean up all created resources after test completion

## Test Annotation Description

- `volcano.sh/card.quota`: Queue-level card resource quota, represented in JSON format
- `volcano.sh/card.request`: Job-level card resource request, represented in JSON format
- `volcano.sh/card.name`: Task-level card name request

## Notes

- The test environment needs to support GPU resources, especially the `nvidia.com/gpu` resource type
- Ensure the capacity-card plugin is enabled in the scheduler configuration
- Tests will create and delete resources during execution, please run in a test environment