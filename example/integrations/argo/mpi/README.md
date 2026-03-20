If installing via the Quick Start [guide](https://argo-workflows.readthedocs.io/en/latest/quick-start/), run  
```
kubectl create namespace argo
kubectl apply -n argo -f "https://github.com/argoproj/argo-workflows/releases/download/v3.7.10/quick-start-minimal.yaml"
kubectl apply -f rbac.yaml
```
Then submit `hello-world.yaml` via kubectl or the Argo Workflows UI.  

Notes:  
- These steps may be incompatible with Argo Workflows version 4.0+.
- MPI master / worker syntax for hello-world.yaml is adapted from [here](https://github.com/volcano-sh/volcano/blob/master/example/integrations/mpi/mpi-example.yaml).  
- InitContainer approach of using `kubectl wait ...` for mpimaster pod is adapted from [this](https://techcommunity.microsoft.com/blog/azurehighperformancecomputingblog/deploy-ndm-v4-a100-kubernetes-cluster/3838871) Azure guide for MPI / HPC on K8s. Additional RBAC permissions are required for the InitContainer to query worker node status.  
- Resource requests in `hello-world.yaml` can be increased above 0 to consume NICs or GPUs, as shown [here](https://azure.github.io/aks-rdma-infiniband/configurations/network-operator#1-sr-iov-device-plugin). Consuming all NICs / GPUs for a node will prevent other pods from scheduling there.  
- The `follow-master-logs` DAG task runs in parallel with the `mpimaster` pod, providing runtime log visibility. Expected output of the `follow-master-logs` task would be - 
```
Waiting for MPI master pod...
Found pod mpi-hello-job-mpimaster-0
Waiting for pod to start or finish...
Pod phase: Running
Streaming logs...
Running MPI hello world...
Warning: Permanently added 'mpi-hello-job-mpiworker-0.mpi-hello-job' (ED25519) to the list of known hosts.
Warning: Permanently added 'mpi-hello-job-mpiworker-1.mpi-hello-job' (ED25519) to the list of known hosts.
Hello world from processor mpi-hello-job-mpiworker-0, rank 0 out of 2 processors
Hello world from processor mpi-hello-job-mpiworker-1, rank 1 out of 2 processors
```

