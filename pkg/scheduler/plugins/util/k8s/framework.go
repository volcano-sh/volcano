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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/controller/volume/scheduling"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	schedulerlisters "k8s.io/kubernetes/pkg/scheduler/listers"
)

// Framework is a K8S framework who mainly provides some methods
// about snapshot and plugins such as predicates
type Framework struct {
	snapshot schedulerlisters.SharedLister
}

var _ v1alpha1.FrameworkHandle = &Framework{}

// SnapshotSharedLister returns the scheduler's SharedLister of the latest NodeInfo
// snapshot. The snapshot is taken at the beginning of a scheduling cycle and remains
// unchanged until a pod finishes "Reserve". There is no guarantee that the information
// remains unchanged after "Reserve".
func (f *Framework) SnapshotSharedLister() schedulerlisters.SharedLister {
	return f.snapshot
}

// IterateOverWaitingPods acquires a read lock and iterates over the WaitingPods map.
func (f *Framework) IterateOverWaitingPods(callback func(v1alpha1.WaitingPod)) {
	panic("not implemented")
}

// GetWaitingPod returns a reference to a WaitingPod given its UID.
func (f *Framework) GetWaitingPod(uid types.UID) v1alpha1.WaitingPod {
	panic("not implemented")
}

// RejectWaitingPod rejects a WaitingPod given its UID.
func (f *Framework) RejectWaitingPod(uid types.UID) {
	panic("not implemented")
}

// HasFilterPlugins returns true if at least one filter plugin is defined.
func (f *Framework) HasFilterPlugins() bool {
	panic("not implemented")
	return false
}

// HasScorePlugins returns true if at least one score plugin is defined.
func (f *Framework) HasScorePlugins() bool {
	panic("not implemented")
	return false
}

// ListPlugins returns a map of extension point name to plugin names configured at each extension
// point. Returns nil if no plugins where configred.
func (f *Framework) ListPlugins() map[string][]config.Plugin {
	panic("not implemented")
	return nil
}

// ClientSet returns a kubernetes clientset.
func (f *Framework) ClientSet() kubernetes.Interface {
	panic("not implemented")
	return nil
}

// SharedInformerFactory returns a shared informer factory.
func (f *Framework) SharedInformerFactory() informers.SharedInformerFactory {
	panic("not implemented")
	return nil
}

// VolumeBinder returns the volume binder used by scheduler.
func (f *Framework) VolumeBinder() scheduling.SchedulerVolumeBinder {
	panic("not implemented")
	return nil
}

// NewFrameworkHandle creates a FrameworkHandle interface, which is used by k8s plugins.
func NewFrameworkHandle(pods []*v1.Pod, nodes []*v1.Node) v1alpha1.FrameworkHandle {
	snapshot := NewSnapshot(pods, nodes)
	return &Framework{
		snapshot: snapshot,
	}
}
