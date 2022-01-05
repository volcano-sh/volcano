# Nvidia GPU Operator Integration

## Background
Nvidia have a standard instructions on how to integrate their GPUs and drivers into a Kubernetes Cluster with GPU-Operator. GPU scheduling is required many HPC applications, particularly multi-node multi-gpu deep learning training.

## Motivation
Exclusive GPU sharing currently doesn't take advantage of the work already done by Nvidia's GPU operator in finding GPUs and adding node annotations. Using these annotations foregoes the effort of running our own device plugins for device discovery. This also has the advantage of forcing users to only be exposed to the scheduled of GPUs in their container rather than asking them to use the environment variables that are annotated by previous implementations.

## Goals
 - Support for exclusive access scheduling with based on the standard annotations provided by Nvidia.
 - Limited user effort other than following Nvidia instructions for creating gpu cluster and changing volcano-scheduler-configmap.
 - Add this feature with minimised required changes to existing codebase.

## Non-Goals
 - Support for gpu-sharing and mixed node annotation support (nodes should have the same type of GPU on them)


## Usage
Follow [how_to_use_gpu_requests](../user-guide/how_to_use_gpu_requests.md) to install gpu-operator and make the relevant changes to configmaps.
