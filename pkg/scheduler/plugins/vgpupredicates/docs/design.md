# design.md

## Introduction

volcano-vgpu is designed to provide volcano cluster the ability to 

***Virtualize GPU*** A task can allocate a vGPU by specifying the device memory limit. volcano-vgpu will make sure that task senses the vGPU as a physical GPU with specified device memory.

***Share multiple GPUs*** A task can allocate vGPUs. It can do so by setting both device number and device memory.

***Device memory control*** Volcano-vgpu will ensure proper isolation between vGPUs. The device memory usage of a vGPU will never excced the specified limit.

***Virtual Device Memory*** Users can choose to oversubscribe device memory in certain GPU. It allows device memory usage in that GPU exceed the device limit by using host memory as swap.

## Components

The component of volcano-vgpu contains several adjustments in volcano main repo, and a seperate vgpu-volcano-device-plugin reposiotry, as the image shown below:

![img](./components.png)

Note that the orange components are which need to be modifyed, while others are new components to be added.


## Walkthrough of vgpu pods

## How to maintain vgpu status

