# MPI Job with Argo Workflows Integration

This example demonstrates how to run MPI (Message Passing Interface) jobs using Volcano and Argo Workflows.

## Overview

MPI is widely used for scientific computing and HPC (High Performance Computing) applications. This example shows:
- A master-worker MPI architecture managed by Volcano
- Argo Workflow orchestration for job submission and log monitoring
- A "log follower" pattern for monitoring distributed jobs from Argo UI

## Prerequisites

1. Argo Workflows installed
2. Volcano installed
3. MPI implementation available in container image (e.g., OpenMPI)

## Architecture

```
Argo Workflow
├── submit-volcano-job (creates MPI Job)
└── follow-master-logs (monitors master pod logs)
    └── waits for master pod → follows logs

Volcano Job
├── mpimaster (1 replica) - coordinates MPI workers
└── mpiworker (N replicas) - compute workers
```

## Usage

### 1. Submit the WorkflowTemplate

```bash
kubectl apply -f mpi-workflowtemplate.yaml
```

### 2. Submit a workflow instance

```bash
argo submit --from workflowtemplate/mpi-volcano \
  -p job-name=my-mpi-job \
  -p mpi-worker-replicas=4
```

### 3. Monitor the workflow

```bash
argo list
argo logs <workflow-name>
```

## WorkflowTemplate Details

The workflow consists of two main steps:

1. **submit-volcano-job**: Creates a Volcano Job with MPI master and workers
2. **follow-master-logs**: Waits for the master pod and streams its logs

### MPI Master

The master pod:
- Runs the main MPI application
- Coordinates with worker pods via SSH (enabled by Volcano ssh plugin)
- Uses the `TaskCompleted` policy to mark job as complete when finished

### MPI Workers

The worker pods:
- Run MPI worker processes
- Communicate with master via MPI over the network
- Automatically scale based on `mpi-worker-replicas` parameter

## Customization

### Change MPI Application

Modify the container command in the `submit-volcano-job` template:

```yaml
command: [mpirun]
args:
  - "--hostfile"
  - "/etc/volcano/mpi.hostfile"
  - "-np"
  - "{{workflow.parameters.mpi-worker-replicas}}"
  - "your-mpi-application"
```

### Adjust Resources

Modify resource requests/limits in the Volcano Job spec:

```yaml
resources:
  requests:
    cpu: "4"
    memory: "8Gi"
    nvidia.com/gpu: "1"  # for GPU workloads
```

## Notes

- The `ssh` plugin is required for MPI communication between pods
- The `svc` plugin provides DNS-based service discovery
- Worker replicas can be parameterized via Argo workflow parameters
- Log follower pod allows monitoring from Argo UI without accessing individual pods

## References

- [Volcano MPI Documentation](https://volcano.sh/en/docs/vcjob/)
- [Argo Workflows Resource Template](https://argoproj.github.io/argo-workflows/walk-through/kubernetes-resources/)
- [Azure HPC on Kubernetes](https://techcommunity.microsoft.com/blog/azurehighperformancecomputingblog/deploy-ndm-v4-a100-kubernetes-cluster/3838871)
