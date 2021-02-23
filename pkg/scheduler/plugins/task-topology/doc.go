/*
Copyright 2021 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tasktopology

// here is an example for you to understand what this plugin do.
//
// In the beginning, we have a job with 6 task:
//   ps0, ps1, worker0, worker1, worker2, worker3
//   for simplicity, each task just need 1 cpu.
// and set the task-affinity to:
//   --affinity [[ps, worker]] --anti-affinity [[ps]]
//
// at the OnSessionOpen stage, we try to generate the bucket by affinity:
// 1. sort the task by taskAffinityOrder
//    in this order, the anti-affinity prior to affinity
//    because anti-affinity would generate more bucket
//    task with orders:
//      ps0, ps1, worker0, worker1, worker2, worker3
// 2. generate bucket
//      ps0, there is no bucket, generate bucket 1
//      ps1, has 1 bucket, but has anti-affinity config, generate bucket 2
//      worker0, affinity to all two bucket, choose bucket 1,
//      worker1, affinity to all two bucket, but by resource balancing, choose bucket 2
//      worker2, choose 1
//      worker3, choose 2
//    now, we have bucket
//      b1: ps0, worker0, worker2
//      b2: ps1, worker1, worker3
//
// after bucket generation, the allocate actions would using these functions in order:
//   taskOrderFn, nodeOrderFn, allocateFunc
//
// after taskOrderFn, we will have task with bucket order:
//   ps0, worker0, worker2, ps1, worker1, worker3,
//
// now for nodeOrderFn, suppose that we have 3 node,
//   node1: cpu 2
//   node2: cpu 1
//   node3: cpu 4
//
// the score for nodeOrderFn would mapping to [0, 10], but now just using bucket score for simplicity
// for ps1:
//   name       bucket in node      score
//   node1:     ps0 worker0         2
//   node2:     ps0                 1
//   node3:     ps0 worker0 worker2 3
//
// obviously, ps0 will bind to node3.
// | node1: - | node2: - | node3: ps0 |
//
// for worker1:
//   name           bucket in node      score
//   node1:         worker0 worker2     2
//   node2:         worker0             1
//   node3:  ps0 |  worker0 worker2     3
//
// and then, worker0 will follow the ps0, and bind to node3.
// and the same to worker2.
// | node1: - | node2: - | node3: ps0 worker0 worker2 |
//
// next task, for ps1:
//   name                           bucket in node      score
//   node1:                         ps1 worker1         2
//   node2:                         ps1                 1
//   node3: ps0 worker0 worker2 |   ps1                 0 // because of anti-affinity
//
// so, ps1 will bind to node1.
// | node1: ps1 | node2: - | node3: ps0 worker0 worker2 |
// and worker1 will bind to node1.
// | node1: ps1 worker1 | node2: - | node3: ps0 worker0 worker2 |
//
// for worker3, the node2 and node3 has the same score,
// the choice will affect by other plugin like binpack or leastRequestPriority.
