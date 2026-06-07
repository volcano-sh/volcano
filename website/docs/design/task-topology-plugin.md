# Task Topology Plugin

## Introduction

In big data processing jobs like Tensorflow & Spark, tasks transmitted a large amount of data between each other, causing transmission delay took a large proportion in job execution time. So task topology plugin was proposed to modify scheduling strategy according to transmission topology inside a job, so as to cut the data amount to be transmitted between nodes, decrease transmission delay proportion in job execution time, and improve resource utilization.

## Theory

- For simplicity, task-topology plugin create task topology of a job according to task affinities set in job annotation, then create buckets to store tasks. Tasks with affinity tends to be put in same bucket, and tasks with anti-affinity tends to be put in different bucket. Finally reflect bucket to nodes, so as to minimize the data transmission between nodes.

- Here is an example to describe what task-topology plugin do.

- Suppose a tensorflow job with 6 task: `ps0, ps1, worker0, worker1, worker2, worker3`. For simplicity, each task just need 1 cpu. Set the task affinity as `"affinity": "ps,worker"`, `"anti-affinity": "ps"`

  - In `OnSessionOpen`, task-topology plugin generates the bucket by affinity:
    - sort the task by `taskAffinityOrder`, in this order, the anti-affinity is prior to affinity, because anti-affinity would generate more bucket.
    - Suppose tasks with orders: `ps0, ps1, worker0, worker1, worker2, worker3`

  - generate bucket
    1. ps0, there is no bucket, generate bucket 1
    2. ps1, has 1 bucket, but has anti-affinity config, generate bucket 2
    3. worker0, affinity to all two bucket, choose bucket 1,
    4. worker1, affinity to all two bucket, but by resource balancing, choose bucket 2
    5. worker2, choose 1
    6. worker3, choose 2
    7. now, we have buckets:
        | bucket | tasks |
        | - | - |
        | bucket1 | ps0, worker0, worker2 |
        | bucket2 | ps1, worker1, worker3 |

- After bucket generation, task-topology plugin provides `taskOrderFn` for `allocate` action  to create a `priorityQueue` for allocate. In sample above, the task order will be like: `ps0, worker0, worker2, ps1, worker1, worker3`

- Suppose there are 3 nodes available in cluster:
    | node | resources |
    | - | - |
    | node1 | cpu: 2 |
    | node2 | cpu: 1 |
    | node3 | cpu: 4 |

- Task-topology plugin also provide `nodeOrderFn` to priority score for each node, which would mapping to [0, 10], but now just using bucket score for simplicity:
  - for ps0:
    | node | bucket in node | score |
    | - | - | - |
    | node1 | ps0 worker0 | 2 |
    | node2 | ps0 | 1 |
    | node3 | ps0 worker0 worker2 | 3 |

    obviously, ps0 will bind to node3.
    | node1 | node2 | node3 |
    | - | - | - |
    | | | ps0 |

  - for worker0:
    | node | own tasks | bucket in node | score |
    | - | - | - | - |
    | node1 | | worker0 worker2 | 2 |
    | node2 | | worker0 | 1 |
    | node3 | ps0 | worker0 worker2 | 3 |

    and then, worker0 will follow the ps0, and bind to node3.

  - and the same to worker2.
    obviously, ps0 will bind to node3.
    | node1 | node2 | node3 |
    | - | - | - |
    | | | ps0, worker0, worker2 |

  - next task, for ps1:
    | node | own tasks | bucket in node | score |
    | - | - | - | - |
    | node1 | | ps1 worker1 | 2 |
    | node2 | | ps1 | 1 |
    | node3 | ps0, worker0, worker2 | ps1 | 0(anti-affinity) |

    so, ps1 will bind to node1.

    | node1 | node2 | node3 |
    | - | - | - |
    | ps1 | | ps0, worker0, worker2 |

  - and worker1 will bind to node1.
    | node1 | node2 | node3 |
    | - | - | - |
    | ps1, worker1 | | ps0, worker0, worker2 |

  - for worker3, the node2 and node3 has the same score, the choice will affect by other plugins like `binpack` or `leastRequestPriority`.

## Future Improvement

1. By now task-topology plugin uses annotations as input arguments, it is easy to cooperate with upper applications through various operators, but not official. So next step task-topology plugin could be added into job plugin like `svc` & `ssh`, which could still set inside individual job.
2. By now task-topology plugin only create task topology according to task species & affinities, but a more detailed topology may need a whole matrix with data scale. So one more interface is needed once task-topology plugin needs to be extended.
3. By now task-topology plugin do not interact with other arguments of volcano, `minAvailable`, etc, may need supports about this if necessary.
