/*
Copyright 2020 The Volcano Authors.

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

package k8s

import (
	"context"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/parallelize"
	scheduling "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
)

// Framework is a K8S framework who mainly provides some methods
// about snapshot and plugins such as predicates
type Framework struct {
	snapshot        framework.SharedLister
	kubeClient      kubernetes.Interface
	informerFactory informers.SharedInformerFactory
}

var _ framework.Handle = &Framework{}

// SnapshotSharedLister returns the scheduler's SharedLister of the latest NodeInfo
// snapshot. The snapshot is taken at the beginning of a scheduling cycle and remains
// unchanged until a pod finishes "Reserve". There is no guarantee that the information
// remains unchanged after "Reserve".
func (f *Framework) SnapshotSharedLister() framework.SharedLister {
	return f.snapshot
}

// IterateOverWaitingPods acquires a read lock and iterates over the WaitingPods map.
func (f *Framework) IterateOverWaitingPods(callback func(framework.WaitingPod)) {
	panic("not implemented")
}

// GetWaitingPod returns a reference to a WaitingPod given its UID.
func (f *Framework) GetWaitingPod(uid types.UID) framework.WaitingPod {
	panic("not implemented")
}

// HasFilterPlugins returns true if at least one filter plugin is defined.
func (f *Framework) HasFilterPlugins() bool {
	panic("not implemented")
}

// HasScorePlugins returns true if at least one score plugin is defined.
func (f *Framework) HasScorePlugins() bool {
	panic("not implemented")
}

// ListPlugins returns a map of extension point name to plugin names configured at each extension
// point. Returns nil if no plugins where configred.
func (f *Framework) ListPlugins() map[string][]config.Plugin {
	panic("not implemented")
}

// ClientSet returns a kubernetes clientset.
func (f *Framework) ClientSet() kubernetes.Interface {
	return f.kubeClient
}

// SharedInformerFactory returns a shared informer factory.
func (f *Framework) SharedInformerFactory() informers.SharedInformerFactory {
	return f.informerFactory
}

// VolumeBinder returns the volume binder used by scheduler.
func (f *Framework) VolumeBinder() scheduling.SchedulerVolumeBinder {
	panic("not implemented")
}

// EventRecorder was introduced in k8s v1.19.6 and to be implemented
func (f *Framework) EventRecorder() events.EventRecorder {
	return nil
}

func (f *Framework) AddNominatedPod(pod *framework.PodInfo, nodeName string) {
	panic("implement me")
}

func (f *Framework) DeleteNominatedPodIfExists(pod *v1.Pod) {
	panic("implement me")
}

func (f *Framework) UpdateNominatedPod(oldPod *v1.Pod, newPodInfo *framework.PodInfo) {
	panic("implement me")
}

func (f *Framework) NominatedPodsForNode(nodeName string) []*framework.PodInfo {
	panic("implement me")
}

func (f *Framework) RunPreScorePlugins(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodes []*v1.Node) *framework.Status {
	panic("implement me")
}

func (f *Framework) RunScorePlugins(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodes []*v1.Node) (framework.PluginToNodeScores, *framework.Status) {
	panic("implement me")
}

func (f *Framework) RunFilterPlugins(ctx context.Context, state *framework.CycleState, pod *v1.Pod, info *framework.NodeInfo) framework.PluginToStatus {
	panic("implement me")
}

func (f *Framework) RunPreFilterExtensionAddPod(ctx context.Context, state *framework.CycleState, podToSchedule *v1.Pod, podInfoToAdd *framework.PodInfo, nodeInfo *framework.NodeInfo) *framework.Status {
	panic("implement me")
}

func (f *Framework) RunPreFilterExtensionRemovePod(ctx context.Context, state *framework.CycleState, podToSchedule *v1.Pod, podInfoToRemove *framework.PodInfo, nodeInfo *framework.NodeInfo) *framework.Status {
	panic("implement me")
}

func (f *Framework) RejectWaitingPod(uid types.UID) bool {
	panic("implement me")
}

func (f *Framework) KubeConfig() *rest.Config {
	panic("implement me")
}

func (f *Framework) RunFilterPluginsWithNominatedPods(ctx context.Context, state *framework.CycleState, pod *v1.Pod, info *framework.NodeInfo) *framework.Status {
	panic("implement me")
}

func (f *Framework) Extenders() []framework.Extender {
	panic("implement me")
}

func (f *Framework) Parallelizer() parallelize.Parallelizer {
	return parallelize.NewParallelizer(16)
}

// NewFrameworkHandle creates a FrameworkHandle interface, which is used by k8s plugins.
func NewFrameworkHandle(nodeMap map[string]*framework.NodeInfo, client kubernetes.Interface, informerFactory informers.SharedInformerFactory) framework.Handle {
	snapshot := NewSnapshot(nodeMap)
	return &Framework{
		snapshot:        snapshot,
		kubeClient:      client,
		informerFactory: informerFactory,
	}
}
