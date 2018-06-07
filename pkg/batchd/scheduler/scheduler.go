/*
Copyright 2017 The Kubernetes Authors.

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

package scheduler

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	schedcache "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/clientset"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/scheduler/framework"
)

type PolicyController struct {
	config     *rest.Config
	clientset  *clientset.Clientset
	kubeclient *kubernetes.Clientset
	cache      schedcache.Cache
	allocator  Interface
	podSets    *cache.FIFO
}

// func podSetKey(obj interface{}) (string, error) {
// 	podSet, ok := obj.(*arbapi.JobInfo)
// 	if !ok {
// 		return "", fmt.Errorf("not a PodSet")
// 	}

// 	return fmt.Sprintf("%s", podSet.UID), nil
// }

func NewPolicyController(config *rest.Config, schedulerName string) (*PolicyController, error) {
	cs, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("fail to create client for PolicyController: %#v", err)
	}

	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client for PolicyController: %#v", err)
	}

	// alloc, err := New()
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create allocator: %#v", err)
	// }

	policyController := &PolicyController{
		config:     config,
		clientset:  cs,
		kubeclient: kc,
		cache:      schedcache.New(config, schedulerName),
		// allocator:  alloc,
		// podSets:    cache.NewFIFO(podSetKey),
	}

	return policyController, nil
}

func (pc *PolicyController) Run(stopCh <-chan struct{}) {
	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	go wait.Until(pc.runOnce, 2*time.Second, stopCh)
	// go wait.Until(pc.processAllocDecision, 0, stopCh)
}

func (pc *PolicyController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	ssn := framework.OpenSession(pc.cache)

	for _, action := range Actions {
		action.Execute(ssn)
	}

	framework.CloseSession(ssn)
}

// func (pc *PolicyController) enqueue(queues []*arbapi.JobInfo) {
// 	for _, ps := range queues {
// 		pc.podSets.Add(ps)
// 	}
// }

// func (pc *PolicyController) cancelAllocDecisionProcessing() {
// 	// clean up FIFO Queue podSets
// 	err := pc.podSets.Replace([]interface{}{}, "")
// 	if err != nil {
// 		glog.V(4).Infof("Reset podSets error %v", err)
// 	}
// }

// func (pc *PolicyController) assumePods(queues []*arbapi.JobInfo) {
// 	for _, ps := range queues {
// 		for _, p := range ps.Tasks[arbapi.Pending] {
// 			if len(p.NodeName) != 0 {
// 				pc.assume(p.Pod.DeepCopy(), p.NodeName)
// 			}
// 		}
// 	}
// }

// // assume signals to the cache that a pod is already in the cache, so that binding can be asynchronous.
// // assume modifies `assumed`
// func (pc *PolicyController) assume(assumed *v1.Pod, host string) {
// 	assumed.Spec.NodeName = host
// 	err := pc.cache.AssumePod(assumed)
// 	if err != nil {
// 		glog.V(4).Infof("fail to assume pod %s", assumed.Name)
// 	}
// }

// func (pc *PolicyController) processAllocDecision() {
// 	pc.podSets.Pop(func(obj interface{}) error {
// 		ps, ok := obj.(*arbapi.JobInfo)
// 		if !ok {
// 			return fmt.Errorf("not a PodSet")
// 		}

// 		for _, p := range ps.Assigned {
// 			if len(p.NodeName) != 0 {
// 				if err := pc.kubeclient.CoreV1().Pods(p.Namespace).Bind(&v1.Binding{
// 					ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: types.UID(p.UID)},
// 					Target: v1.ObjectReference{
// 						Kind: "Node",
// 						Name: p.NodeName,
// 					},
// 				}); err != nil {
// 					glog.Infof("Failed to bind pod <%v/%v>: %#v", p.Namespace, p.Name, err)
// 					continue
// 				}
// 			}
// 		}

// 		for _, p := range ps.Running {
// 			if len(p.NodeName) == 0 {
// 				// TODO(k82cn): it's better to use /eviction instead of delete to avoid race-condition.
// 				if err := pc.kubeclient.CoreV1().Pods(p.Namespace).Delete(p.Name, &metav1.DeleteOptions{}); err != nil {
// 					glog.Infof("Failed to preempt pod <%v/%v>: %#v", p.Namespace, p.Name, err)
// 					continue
// 				}
// 			}
// 		}

// 		return nil
// 	})
// }
