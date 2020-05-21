# MindSpore Volcano Example

#### These examples shows how to run MindSpore via Volcano. Since MindSpore itself is relatively new, these examples maybe oversimplified, but will evolve with both communities.

## Introduction of MindSpore

MindSpore is a new open source deep learning training/inference framework that
could be used for mobile, edge and cloud scenarios. MindSpore is designed to
provide development experience with friendly design and efficient execution for
the data scientists and algorithmic engineers, native support for Ascend AI
processor, and software hardware co-optimization.

MindSpore is open sourced on both [Github](https://github.com/mindspore-ai/mindspore ) and [Gitee](https://gitee.com/mindspore/mindspore ).

## Prerequisites

These two examples are tested under below env:

- Ubuntu: `16.04.6 LTS` 
- docker: `v18.06.1-ce`
- Kubernetes: `v1.16.6`
- NVIDIA Docker: `2.3.0`
- NVIDIA/k8s-device-plugin: `1.0.0-beta6`
- NVIDIA drivers: `418.39`
- CUDA: `10.1`

## MindSpore CPU example

Using a modified MindSpore CPU image as the container image which
trains LeNet with MNIST dataset. 

pull image: `docker pull lyd911/mindspore-cpu-example:0.2.0`  
to run: `kubectl apply -f mindspore-cpu.yaml`  
to check the result: `kubectl logs mindspore-cpu-pod-0`

## MindSpore GPU example

Using a modified image which the openssh-server is installed from
the official MindSpore GPU image. To check the eligibility of
MindSpore GPU's ability to communicate with other processes, we
leverage the mpimaster and mpiworker task spec of Volcano. In this
example, we launch one mpimaster and two mpiworkers, the python script 
is taken from [MindSpore Gitee README](https://gitee.com/mindspore/mindspore/blob/master/README.md ), which is also modified to be 
able to run parallelly.

pull image: `docker pull lyd911/mindspore-gpu-example:0.2.0`  
to run: `kubectl apply -f mindspore-gpu.yaml`  
to check result: `kubectl logs mindspore-gpu-mpimster-0`

The expected output should be (2*3) of multi-dimensional array.

## Future

An end to end example of training a network using MindSpore on 
distributed GPU via Volcano is expected in the future.
