## Hierarchical Dominant Resource Fairness (HDRF)

## Background

DRF(dominant resource fairness) is widely used in multi-resource scheduling.
In the volcano scheduler, resource sharing fairness is guaranteed by setting the weights of queues and namespaces, while
this method can not represent a tree-like hierarchy.
To represent a more complex hierarchical group sharing other than a big weighted flattened sharing of namespaces or queues, the HDRF(hierarchical dominant resource fairness) may need to be adopted.
The original [paper](https://people.eecs.berkeley.edu/~alig/papers/h-drf.pdf) refers to two points that naive DRF does not suit in.
The first is the starvation caused by children with a complementary dominant resource.
```
              n
    /                   \
  n1 w=50%             n2 w=50%
(0 CPU,1 GPU)
                  /            \
                n2,1  w=50%    n2,2 w=50%
              (1 CPU,0 GPU)  (0 CPU,1 GPU)

```
In the case above, the n2,1 group has a higher dominant share (for example CPU, 100% share), which gives
a 100% share to its parent n2 group. Consider a job removed, and a new job added, then sibling n2,2 group with different dominant share(for example GPU 50%) will be punished since GPU will be always allocated to the n1 group which has a smaller dominant share than the n2 group. To avoid this starvation, HDRF rescales children to the minimum node. In this case, the n2,1 group will be scaled to (10,0) * (0.5/1) = (5,0), it is then summed to the parent, thus the parent n2 group will have a share of 50% which is our desired state.

The second is nodes blocking caused by a saturated node.
```
                                n
/                    |                     |               \
n1                   n2                    n3               n4
(1 CPU,0 GPU)  (1 CPU,0 GPU)          /          \          (0 CPU,1 GPU)
                                     n3,1          n3,2
                                (1 CPU,0 GPU)  (0 CPU,1 GPU)
```

In the case about, at the point that every leaf is allocated 1/3 of its dominant resource.
As soon as another task is assigned to the n4 group, n4’s dominant share
will be higher than n3’s, resulting in all remaining GPU resources being repeatedly allocated to n3,2.
Thus, in the final allocation n3,2 gets 2/3 of GPUs, while n4 gets only 1/3 of the GPUs.
To fix this problem, HDRF first picks the minimum dominant share, M, among non-blocked nodes.
Second, only every non-blocked node’s resource consumption vector is rescaled such that its dominant share is M.
Third, all nodes' blocked as well as non-blocked vectors are added to
get the parent’s resource consumption vector.
Furthermore, HDRF ignores saturated resources when computing the dominant share of any internal node.

## Implementation

The hierarchy and weights of interest are annotated in the Queue annotations, marked as 'volcano.sh/hierarchy' and 'volcano.sh/hierarchy-weights'.
A new option hierarchyEnable is added in the drf plugin options.
With this feature enabled, on the event of task allocation and deallocation, drf attribute is updated through the following steps:

1.  Based on the queue spec the job belongs to, build hierarchical nodes along the path. For example, the hierarchy "root/eng/prod" with weight "1/2/8" will construct a 3-level hierarchy.
2.  Calculate job drf attribute as ordinary the drf algorithm does, mark it saturated if any of its resources is satisfied, or some kinds of the resources are fully allocated.
3.  Update internal nodes recursively from root, calculate the dominant resource of non-blocking child nodes, scale non-blocking nodes with the smallest dominant share, sum all resources of the non-blocking and blocking nodes up, calculate the dominant resource share of the node among the demanding resources, and mark itself saturated if all the children are saturated.

### allocate

The queue order is determined along the hierarchy path. If the shares of two same level nodes divided by weight meet a tier, the child along the path will be compared. A saturated node has a minimum priority since no more resources can be allocated to this node. If the hierarchy is enabled. A queue containing both queues and jobs is not allowed. For example a "root/sci" queue conflicts with the "root/sci/dev" queue.

### preempt

Except for the namespace and job priority, hierarchical queues' priority should be taken into consideration. Queues with lower share have higher priority and saturated queues have minimum priority.

### reclaim

The deserved share is determined by hdrf share of the queues.

## Limitations

1. This feature conflicts with the proportion plugin. If the hdrf is enabled, the proportion should be disabled, and a reclaim function which compares queue by hierarchical shares and weights should be added.
2. When a job is added in the tree, hdrf travels through the root recursively. It needs to mark all the saturated nodes, if some resources are fully allocated. This may be ineffective.
