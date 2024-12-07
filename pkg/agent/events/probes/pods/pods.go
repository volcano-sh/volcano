/*
Copyright 2024 The Volcano Authors.

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

package pods

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis/extension"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/probes"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"
)

func init() {
	probes.RegisterEventProbeFunc(string(framework.PodEventName), NewPodProbe)
}

type PodProbe struct {
	queue workqueue.RateLimitingInterface
}

func NewPodProbe(config *config.Configuration, mgr *metriccollect.MetricCollectorManager, queue workqueue.RateLimitingInterface) framework.Probe {
	podHandler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { enqueuePod(obj, queue) },
		UpdateFunc: func(oldObj, newObj interface{}) { enqueuePod(newObj, queue) },
		DeleteFunc: func(obj interface{}) {},
	}
	config.InformerFactory.K8SInformerFactory.Core().V1().Pods().Informer().AddEventHandler(podHandler)
	return &PodProbe{
		queue: queue,
	}
}

func (p *PodProbe) ProbeName() string {
	return "PodProbe"
}

func (p *PodProbe) Run(stop <-chan struct{}) {
	klog.InfoS("Start pod probe")
	<-stop
}

func (p *PodProbe) RefreshCfg(cfg *api.ColocationConfig) error {
	return nil
}

func enqueuePod(obj interface{}, queue workqueue.RateLimitingInterface) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		klog.ErrorS(nil, "Pod phase invoked with an invalid data struct", "obj", obj)
		return
	}

	for _, podCondition := range pod.Status.Conditions {
		if podCondition.Type == corev1.PodReady && podCondition.Status == corev1.ConditionTrue && pod.DeletionTimestamp == nil {
			qosLevel := extension.GetQosLevel(pod)
			podEvent := framework.PodEvent{
				UID:      pod.UID,
				QoSClass: pod.Status.QOSClass,
				QoSLevel: int64(qosLevel),
				Pod:      pod,
			}
			klog.V(5).InfoS("Receive pod event", "pod", klog.KObj(pod))
			queue.Add(podEvent)
		}
	}
}
