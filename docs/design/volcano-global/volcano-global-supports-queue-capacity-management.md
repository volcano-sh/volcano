# Volcano Global Supports Queue Capacity Management

@Vacant2333 2024/9/22

容量管理需要: 决定目前是否还能提交任务, 是否达到容量上限(allocatable func)
capacity插件, 是否用超deserve(overused func)
queue的优先级排序也写在这里面,(从priority插件挪到这里, priority插件只管rb)
调度的最终逻辑需要修改, 判断overused, 而不是一个queue把资源全部拿完


## Introduction

Target issue: [OSPP 2024: Volcano Support Multi-Cloud AI Job Scheduling(queue capacity management)](https://github.com/volcano-sh/volcano/issues/3731)

With the rapid development of large AI models, a single K8s cluster is increasingly unable to meet the needs of
large model AI job training due to resource and performance bottlenecks. More and more users are using
multiple clusters to manage and run AI jobs. Volcano is developing Supports task scheduling of multi-cluster AI jobs,
which involves multi-cluster job management, multi-tenant task fair scheduling, queue management and
other series of requirements. The multi-cluster orchestration system [Karmada](https://karmada.io/) has gradually become an industry standard.
Volcano needs to build AI job scheduling capabilities in multi-cluster scenarios based on Karmada's existing capabilities,
and make up for the lack of queue management and other capabilities in Karmada scheduling to solve AI jobs in multi-cluster scenarios.
Task scheduling, queue management, and **multi-tenant quota management** issues.

This proposal targets the queue capacity management capability.

### Goals

- Support queue capacity management

## Proposal

