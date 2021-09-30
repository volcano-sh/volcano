# Hierarchical Queue
## Motivation
**Hierarchical Queue** is widely used in traditional scheduling systems such as [Yarn](https://hadoop.apache.org/docs/current/hadoop-yarn/hadoop-yarn-site/YARN.html). 
It's an excellent struct representing complex hierarchical group sharing comparing with weighted flatted sharing of namespace 
or queues. Simultaneously, it calls for more complex algorithms to deal with modification of inner members. Although 
[HDRF](https://github.com/volcano-sh/volcano/blob/master/docs/design/hdrf.md) was introduced in v1.1.0, it's lack of fully 
implementation for hierarchical queue. More apis are needed to manage the resource.

## Scope
* API
* Resource Management of Hierarchical Queue

## API
### Hierarchical Queue Example
```
root
 |-- node1
 |     |-- node1-1
 |     |-- node1-2
 |-- node2   
 |     |-- node2-1
```
### Hierarchical Queue API
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: HierarchicalQueue
metadata:
  name: hierarchical-queue
Spec:
  queues:
  - name: node1
    parent: root
    weight: 40
  - name: node2
    parent: root
    weight: 60
  - name: node1-1
    parent: node1
    weight: 50
  - name: node1-2
    parent: node1
    weight: 50
  - name: node2-1
    parent: node2
    weight: 100
```
### Queue Node API
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Queue
metadata:
  name: node-2-1
  annotations:
    volcano.sh/hierarchy: root/node2/node-2-1
    volcano.sh/hierarchy-weights: 1/60/100
Spec:
  weight: 100
  capability:
    cpu: "1024c"
    memory: "2048G"
```
## Resource Management
### CREAT
* Only **one** hierarchical queue instance is allowed to be created.
* Hierarchical Queue and flatted queue **can't** exist at the same time.
* Queue name/weight/parent are required except for `root`.
* nodes in `spec.queues` must be defined as a hierarchy queue. 
* Once a queue node is defined, create a queue node instance as above. 
### DELETE
* If users want to delete the whole hierarchical queue, conditions should be satisfied as follows before delete the `root` 
queue node.
  - Status of **root** node is `closed`.
  - No jobs are in the hierarchical queue.
* If users want to delete some queue node(not leaf node), all the descendant nodes will also be deleted. Conditions should 
be satisfied as follows.
  queue node.
    - Status of this node is `closed`.
    - No jobs are in the descendant leaf queues.
### GET
* Users can get the whole hierarchical queue by getting sub hierarchical queue of `root` node.
* Users can get sub hierarchical queue of any node.
* Users can get details of any node.
### UPDATE
* Users can only update hierarchical queue instance. Any node **can't** be updated alone by Queue node API.
* Queue name is not allowed to be updated. If any queue name is modified, that means deleting the origin queue and create
a new queue with same children.

## Cluster Resource Allocation
Refers to implementation of [HDRF](https://github.com/volcano-sh/volcano/blob/master/docs/design/hdrf.md)

## Reference
* [Apache Hadoop Yarn](https://hadoop.apache.org/docs/current/hadoop-yarn/hadoop-yarn-site/YARN.html)
* [Hierarchical Scheduling for Diverse Datacenter Workloads](https://people.eecs.berkeley.edu/~alig/papers/h-drf.pdf)