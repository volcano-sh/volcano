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

package controller

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	schedcache "github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
)

type PolicyController struct {
	config     *rest.Config
	clientset  *clientset.Clientset
	kubeclient *kubernetes.Clientset
	cache      schedcache.Cache
	allocator  policy.Interface
	podSets    *cache.FIFO
}

func podSetKey(obj interface{}) (string, error) {
	podSet, ok := obj.(*schedcache.PodSet)
	if !ok {
		return "", fmt.Errorf("not a PodSet")
	}

	return fmt.Sprintf("%s/%s", podSet.Namespace, podSet.Name), nil
}

func NewPolicyController(config *rest.Config, policyName string) (*PolicyController, error) {
	cs, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("fail to create client for PolicyController: %#v", err)
	}

	kc, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client for PolicyController: %#v", err)
	}

	alloc, err := policy.New(policyName)
	if err != nil {
		return nil, fmt.Errorf("failed to create allocator: %#v", err)
	}

	queueController := &PolicyController{
		config:     config,
		clientset:  cs,
		kubeclient: kc,
		cache:      schedcache.New(config),
		allocator:  alloc,
		podSets:    cache.NewFIFO(podSetKey),
	}

	return queueController, nil
}

func (pc *PolicyController) Run(stopCh <-chan struct{}) {
	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	go wait.Until(pc.runOnce, 2*time.Second, stopCh)
	go wait.Until(pc.processAllocDecision, 0, stopCh)
}

func (pc *PolicyController) buildConsumers(
	consumers []*schedcache.ConsumerInfo,
	podSets map[string][]*schedcache.PodSet,
	pods []*schedcache.PodInfo,
) map[string]*schedcache.ConsumerInfo {
	result := map[string]*schedcache.ConsumerInfo{}

	// TODO(k82cn): build consumer in cache.

	// Append user-defined c to the result
	for _, c := range consumers {
		for _, pod := range pods {
			if pod.Namespace == c.Namespace {
				c.Pods = append(c.Pods, pod)
			}
		}

		if ps, exist := podSets[c.Namespace]; exist {
			c.PodSets = ps
		}

		result[c.Name] = c
	}

	return result
}

func (pc *PolicyController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	// TODO(k82cn): cancel all alloc decision processing firstly.

	snapshot := pc.cache.Snapshot()

	podSets, pods := pc.groupPods(snapshot.Pods)

	consumers := pc.buildConsumers(snapshot.Consumers, podSets, pods)

	consumers = pc.allocator.Allocate(consumers, snapshot.Nodes)

	pc.enqueue(consumers)
}

func (pc *PolicyController) groupPods(pods []*schedcache.PodInfo) (map[string][]*schedcache.PodSet, []*schedcache.PodInfo) {
	glog.V(4).Info("Enter groupPods ...")
	defer glog.V(4).Info("Leaving groupPods ...")

	podSets := make(map[types.UID]*schedcache.PodSet, 0)
	orpPods := make([]*schedcache.PodInfo, 0)

	for _, p := range pods {
		if len(p.Owner) == 0 {
			orpPods = append(orpPods, p)
			continue
		}

		// TODO (k82cn): replace Owner with label/selector, e.g. Deployment/RS
		if _, found := podSets[p.Owner]; !found {
			ps := schedcache.NewPodSet(p.Owner)

			ps.Namespace = p.Namespace
			ps.Name = string(p.Owner)

			podSets[p.Owner] = ps
		}

		podSets[p.Owner].AddPodInfo(p)
	}

	res := make(map[string][]*schedcache.PodSet)
	for _, ps := range podSets {
		if _, found := res[ps.Namespace]; !found {
			res[ps.Namespace] = make([]*schedcache.PodSet, 0)
		}

		res[ps.Namespace] = append(res[ps.Namespace], ps)
	}

	return res, orpPods
}

func (pc *PolicyController) enqueue(consumers map[string]*schedcache.ConsumerInfo) {
	for _, c := range consumers {
		for _, ps := range c.PodSets {
			pc.podSets.Add(ps)
		}
	}
}

func (pc *PolicyController) processAllocDecision() {
	pc.podSets.Pop(func(obj interface{}) error {
		ps, ok := obj.(*schedcache.PodSet)
		if !ok {
			return fmt.Errorf("not a PodSet")
		}

		for _, p := range ps.Pending {
			if len(p.NodeName) != 0 {
				if err := pc.kubeclient.CoreV1().Pods(p.Namespace).Bind(&v1.Binding{
					ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: p.UID},
					Target: v1.ObjectReference{
						Kind: "Node",
						Name: p.NodeName,
					},
				}); err != nil {
					glog.Infof("Failed to bind pod <%v/%v>: %#v", p.Namespace, p.Name, err)
					return err
				}
			}
		}

		for _, p := range ps.Running {
			if len(p.NodeName) == 0 {
				// TODO(k82cn): it's better to use /eviction instead of delete to avoid race-condition.
				if err := pc.kubeclient.CoreV1().Pods(p.Namespace).Delete(p.Name, &metav1.DeleteOptions{}); err != nil {
					glog.Infof("Failed to preempt pod <%v/%v>: %#v", p.Namespace, p.Name, err)
					return err
				}
			}
		}

		return nil
	})
}
