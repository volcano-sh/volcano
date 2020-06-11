/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import (
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/controller/volume/scheduling"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
	schedulerlisters "k8s.io/kubernetes/pkg/scheduler/listers"

	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// OpenSession start the session
func OpenSession(cache cache.Cache, tiers []conf.Tier, configurations []conf.Configuration) *Session {
	ssn := openSession(cache)
	ssn.Tiers = tiers
	ssn.Configurations = configurations

	for _, tier := range tiers {
		for _, plugin := range tier.Plugins {
			if pb, found := GetPluginBuilder(plugin.Name); !found {
				klog.Errorf("Failed to get plugin %s.", plugin.Name)
			} else {
				plugin := pb(plugin.Arguments)
				ssn.plugins[plugin.Name()] = plugin
			}
		}
	}

	for _, plugin := range ssn.plugins {
		onSessionOpenStart := time.Now()
		plugin.OnSessionOpen(ssn)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionOpen, metrics.Duration(onSessionOpenStart))
	}

	return ssn
}

// CloseSession close the session
func CloseSession(ssn *Session) {
	for _, plugin := range ssn.plugins {
		onSessionCloseStart := time.Now()
		plugin.OnSessionClose(ssn)
		metrics.UpdatePluginDuration(plugin.Name(), metrics.OnSessionClose, metrics.Duration(onSessionCloseStart))
	}

	closeSession(ssn)
}

type framework struct {
	snapshot schedulerlisters.SharedLister
}

var _ v1alpha1.FrameworkHandle = &framework{}

// SnapshotSharedLister returns the scheduler's SharedLister of the latest NodeInfo
// snapshot. The snapshot is taken at the beginning of a scheduling cycle and remains
// unchanged until a pod finishes "Reserve". There is no guarantee that the information
// remains unchanged after "Reserve".
func (f *framework) SnapshotSharedLister() schedulerlisters.SharedLister {
	return f.snapshot
}

// IterateOverWaitingPods acquires a read lock and iterates over the WaitingPods map.
func (f *framework) IterateOverWaitingPods(callback func(v1alpha1.WaitingPod)) {
	panic("not implemented")
}

// GetWaitingPod returns a reference to a WaitingPod given its UID.
func (f *framework) GetWaitingPod(uid types.UID) v1alpha1.WaitingPod {
	panic("not implemented")
}

// RejectWaitingPod rejects a WaitingPod given its UID.
func (f *framework) RejectWaitingPod(uid types.UID) {
	panic("not implemented")
}

// HasFilterPlugins returns true if at least one filter plugin is defined.
func (f *framework) HasFilterPlugins() bool {
	panic("not implemented")
	return false
}

// HasScorePlugins returns true if at least one score plugin is defined.
func (f *framework) HasScorePlugins() bool {
	panic("not implemented")
	return false
}

// ListPlugins returns a map of extension point name to plugin names configured at each extension
// point. Returns nil if no plugins where configred.
func (f *framework) ListPlugins() map[string][]config.Plugin {
	panic("not implemented")
	return nil
}

// ClientSet returns a kubernetes clientset.
func (f *framework) ClientSet() kubernetes.Interface {
	panic("not implemented")
	return nil
}

// SharedInformerFactory returns a shared informer factory.
func (f *framework) SharedInformerFactory() informers.SharedInformerFactory {
	panic("not implemented")
	return nil
}

// VolumeBinder returns the volume binder used by scheduler.
func (f *framework) VolumeBinder() scheduling.SchedulerVolumeBinder {
	panic("not implemented")
	return nil
}

// NewFrameworkHandle creates a FrameworkHandle interface, which is used by k8s plugins.
func NewFrameworkHandle(pods []*v1.Pod, nodes []*v1.Node) v1alpha1.FrameworkHandle {
	snapshot := cache.NewSnapshot(pods, nodes)
	return &framework{
		snapshot: snapshot,
	}
}
