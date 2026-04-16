/*
 Copyright 2026 The Volcano Authors.

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

package allocate

import (
	"sync"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
)

// schGateManager handles asynchronous removal of Volcano scheduling gates from pods.
// When the SchedulingGatesQueueAdmission feature is enabled, pods opt in via the
// scheduling.volcano.sh/queue-allocation-gate annotation. The webhook injects a scheduling
// gate at creation time, and this manager removes it asynchronously after the queue capacity
// check passes, allowing cluster autoscalers to see the Unschedulable condition only when
// it reflects genuine cluster resource shortage rather than queue limits.
type schGateManager struct {
	kubeClient   kubernetes.Interface // Cached client for worker goroutines
	opCh         chan schGateRemovalOperation
	workersWg    sync.WaitGroup
	stopCh       chan struct{}
	workerNum    int
	shutdownOnce sync.Once
}

// schGateRemovalOperation is a request to remove the scheduling gate from a pod.
type schGateRemovalOperation struct {
	namespace string
	name      string
}

func newSchGateManager(workerNum int) *schGateManager {
	return &schGateManager{
		stopCh:    make(chan struct{}),
		workerNum: workerNum,
	}
}

func (m *schGateManager) start() {
	channelSize := m.workerNum * gateRemovalBufferPerWorker
	m.opCh = make(chan schGateRemovalOperation, channelSize)

	for i := 0; i < m.workerNum; i++ {
		m.workersWg.Add(1)
		go m.worker()
	}
	klog.V(3).Infof("Started %d async workers for gate removal", m.workerNum)
}

func (m *schGateManager) stop() {
	m.shutdownOnce.Do(func() {
		close(m.stopCh)
		m.workersWg.Wait()
		if m.opCh != nil {
			close(m.opCh)
		}
		klog.V(3).Infof("Async gate removal workers shut down")
	})
}

func (m *schGateManager) worker() {
	defer m.workersWg.Done()
	for {
		select {
		case <-m.stopCh:
			klog.V(4).Infof("Scheduling gate operation worker shutting down")
			return
		case op := <-m.opCh:
			if err := cache.RemoveVolcanoSchGate(m.kubeClient, op.namespace, op.name); err != nil {
				klog.Errorf("Failed to remove gate from %s/%s: %v", op.namespace, op.name, err)
			} else {
				klog.V(3).Infof("Removed Volcano scheduling gate from pod %s/%s", op.namespace, op.name)
			}
		}
	}
}

// enqueue queues an async gate removal for the given task.
// Returns true if the operation was enqueued, false if the channel is full.
func (m *schGateManager) enqueue(task *api.TaskInfo) bool {
	if !api.HasOnlyVolcanoSchedulingGate(task.Pod) {
		return false
	}
	op := schGateRemovalOperation{
		namespace: task.Namespace,
		name:      task.Name,
	}
	select {
	case m.opCh <- op:
		klog.V(3).Infof("Queued gate removal for %s/%s", task.Namespace, task.Name)
		return true
	default:
		klog.Warningf("Gate operation queue full, skipping gate removal for %s/%s", task.Namespace, task.Name)
		return false
	}
}
