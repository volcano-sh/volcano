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

package preemption

import (
	"sort"
	"strings"
	"sync"

	"github.com/golang/glog"
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/apis/v1"
	schedulercache "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client/clientset"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/policy/util"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	clientv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type preemptedPodInfo struct {
	pod                     *v1.Pod
	totalReleasingResources map[apiv1.ResourceName]resource.Quantity
}

type basePreemption struct {
	localDataMutex            *sync.Mutex
	updateQuotaAllocatorMutex *sync.Mutex

	name   string
	config *rest.Config
	client *kubernetes.Clientset

	terminatingPodsForPreempt   map[string]preemptedPodInfo
	terminatingPodsForUnderused map[string]*v1.Pod
	totalPreemptingResources    map[apiv1.ResourceName]resource.Quantity

	podInformer clientv1.PodInformer
}

func New(config *rest.Config) Interface {
	return newBasePreemption("base-preemption", config)
}

func newBasePreemption(name string, config *rest.Config) *basePreemption {
	bp := &basePreemption{
		localDataMutex:            new(sync.Mutex),
		updateQuotaAllocatorMutex: new(sync.Mutex),
		name:   name,
		config: config,
		client: kubernetes.NewForConfigOrDie(config),
		terminatingPodsForPreempt:   make(map[string]preemptedPodInfo),
		terminatingPodsForUnderused: make(map[string]*v1.Pod),
		totalPreemptingResources: map[apiv1.ResourceName]resource.Quantity{
			"cpu":    resource.MustParse("0"),
			"memory": resource.MustParse("0"),
		},
	}

	informerFactory := informers.NewSharedInformerFactory(bp.client, 0)
	bp.podInformer = informerFactory.Core().V1().Pods()
	bp.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					glog.V(4).Infof("filter pod name(%s) namespace(%s) status(%s)\n", t.Name, t.Namespace, t.Status.Phase)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				DeleteFunc: bp.terminatePodDone,
			},
		})

	return bp
}

func calculatePodResources(pod *v1.Pod) map[string]resource.Quantity {
	totalResource := make(map[string]resource.Quantity)
	for _, container := range pod.Spec.Containers {
		for k, v := range container.Resources.Requests {
			// only handle cpu/memory resource now
			if k.String() != "cpu" && k.String() != "memory" {
				continue
			}
			if _, ok := totalResource[k.String()]; ok {
				result := totalResource[k.String()].DeepCopy()
				result.Add(v)
				totalResource[k.String()] = result
			} else {
				totalResource[k.String()] = v
			}
		}
	}
	return totalResource
}

func killPod(client *kubernetes.Clientset, pod *v1.Pod) error {
	err := client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &meta_v1.DeleteOptions{})
	return err
}

func updateQuotaAllocators(quotaAllocators map[string]*schedulercache.QuotaAllocatorInfo, config *rest.Config) error {
	cs, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Errorf("Fail to create client for quotaAllocator, %#v", err)
		return nil
	}

	quotaAllocatorList, err := cs.ArbV1().QuotaAllocators("").List(meta_v1.ListOptions{})
	if err != nil {
		glog.Errorf("Fail to get quotaAllocator list, %#v", err)
		return nil
	}

	for _, oldQuotaAllocator := range quotaAllocatorList.Items {
		if len(quotaAllocators) == 0 {
			break
		}

		q, ok := quotaAllocators[oldQuotaAllocator.Name]
		if !ok {
			glog.V(4).Infof("QuotaAllocator %s not exist in quotaAllocators", oldQuotaAllocator.Name)
			continue
		}

		// only update QuotaAllocator Status Allocated/Deserved/Used/Preempting
		oldQuotaAllocator.Status.Allocated.Resources = q.QuotaAllocator().Status.Allocated.Resources
		oldQuotaAllocator.Status.Deserved.Resources = q.QuotaAllocator().Status.Deserved.Resources
		oldQuotaAllocator.Status.Used.Resources = q.QuotaAllocator().Status.Used.Resources
		oldQuotaAllocator.Status.Preempting.Resources = q.QuotaAllocator().Status.Preempting.Resources

		_, err := cs.ArbV1().QuotaAllocators(oldQuotaAllocator.Namespace).Update(&oldQuotaAllocator)
		if err != nil {
			glog.Errorf("Fail to update quotaAllocator info, name %s, %#v", q.QuotaAllocator().Name, err)
		}
	}
	return nil
}

func (p *basePreemption) Run(stopCh <-chan struct{}) {
	go p.podInformer.Informer().Run(stopCh)
}

// Preprocessing kill running pod for each quotaAllocator to make used < allocated
func (p *basePreemption) Preprocessing(quotaAllocators map[string]*schedulercache.QuotaAllocatorInfo, pods []*schedulercache.PodInfo) (map[string]*schedulercache.QuotaAllocatorInfo, error) {
	glog.V(4).Infof("Enter Preprocessing ...")
	defer glog.V(4).Infof("Leaving Preprocessing ...")

	p.localDataMutex.Lock()
	defer p.localDataMutex.Unlock()

	// calculate used resources for each quotaAllocator
	for _, q := range quotaAllocators {
		allPods := make(map[string]*v1.Pod)
		for _, pod := range pods {
			if strings.Compare(q.QuotaAllocator().Namespace, pod.Pod().Namespace) != 0 {
				continue
			}
			if _, ok := p.terminatingPodsForUnderused[pod.Name()]; ok {
				continue
			}
			if _, ok := p.terminatingPodsForPreempt[pod.Name()]; ok {
				continue
			}

			allPods[pod.Name()] = pod.Pod()
			podResources := calculatePodResources(pod.Pod())
			glog.V(4).Infof("Total occupied resources by pod %s, %#v", pod.Name(), podResources)

			for k, v := range podResources {
				if k != "cpu" && k != "memory" {
					continue
				}
				resType := apiv1.ResourceName(k)
				if _, ok := q.QuotaAllocator().Status.Used.Resources[resType]; ok {
					result := q.QuotaAllocator().Status.Used.Resources[resType].DeepCopy()
					result.Add(v)
					q.QuotaAllocator().Status.Used.Resources[resType] = result
				} else {
					q.QuotaAllocator().Status.Used.Resources[resType] = v
				}
			}
		}
		// sort pod by priority
		q.Pods = sortPodByPriority(allPods)

		glog.V(4).Infof("QuotaAllocator %s status, deserved (%#v), allocated (%#v), used (%#v), preempting (%#v)",
			q.Name(), q.QuotaAllocator().Status.Deserved.Resources, q.QuotaAllocator().Status.Allocated.Resources,
			q.QuotaAllocator().Status.Used.Resources, q.QuotaAllocator().Status.Preempting.Resources)
	}

	// kill pod to make quotaAllocator Used <= Allocated
	for _, q := range quotaAllocators {
		leftPods, pod, findPod := popPod(q.Pods)
		for findPod {
			if q.UsedUnderAllocated() {
				glog.V(4).Infof("QuotaAllocator %s is underused, used <= allocated, try next quotaAllocator", q.Name())
				leftPods = addPodFront(leftPods, pod)
				break
			}
			glog.V(4).Infof("QuotaAllocator %s is overused, used > allocated, terminate pod %s to release resources", q.Name(), pod.Name)

			// choose a pod to kill and check used <= allocated again
			podResources := calculatePodResources(pod)
			if err := killPod(p.client, pod); err == nil {
				// kill successfully
				p.terminatingPodsForUnderused[pod.Name] = pod
				for k, v := range podResources {
					if k != "cpu" && k != "memory" {
						continue
					}
					resType := apiv1.ResourceName(k)
					if _, ok := q.QuotaAllocator().Status.Used.Resources[resType]; ok {
						result := q.QuotaAllocator().Status.Used.Resources[resType].DeepCopy()
						result.Sub(v)
						q.QuotaAllocator().Status.Used.Resources[resType] = result
					} else {
						glog.Errorf("Cannot find resource %s in quotaAllocator used resource", k)
					}
				}
			} else {
				// TODO may need some error handling when kill pod failed
				glog.Errorf("Failed to kill pod %s", pod.Name)
			}

			leftPods, pod, findPod = popPod(leftPods)
		}
		q.Pods = leftPods
		glog.V(4).Infof("QuotaAllocator %s status after kill pods, deserved (%#v), allocated (%#v), used (%#v), preempting (%#v)",
			q.Name(), q.QuotaAllocator().Status.Deserved.Resources, q.QuotaAllocator().Status.Allocated.Resources,
			q.QuotaAllocator().Status.Used.Resources, q.QuotaAllocator().Status.Preempting.Resources)
	}

	return quotaAllocators, nil
}

func (p *basePreemption) PreemptResources(quotaAllocators map[string]*schedulercache.QuotaAllocatorInfo) error {
	glog.V(4).Infof("Enter PreemptResources ...")
	defer glog.V(4).Infof("Leaving PreemptResources ...")
	// Divided quotaAllocators into three categories
	//   quotaAllocatorsOverused    - Deserved < Allocated
	//   quotaAllocatorsPerfectused - Deserved = Allocated, do nothing for these quotaAllocators in PreemptResources()
	//   quotaAllocatorsUnderused   - Deserved > Allocated
	quotaAllocatorsOverused := make(map[string]*schedulercache.QuotaAllocatorInfo)
	quotaAllocatorsPerfectused := make(map[string]*schedulercache.QuotaAllocatorInfo)
	quotaAllocatorsUnderused := make(map[string]*schedulercache.QuotaAllocatorInfo)
	for _, q := range quotaAllocators {
		result := schedulercache.CompareResources(q.QuotaAllocator().Status.Deserved.Resources, q.QuotaAllocator().Status.Allocated.Resources)
		if result == -1 {
			quotaAllocatorsOverused[q.Name()] = q
		} else if result == 0 {
			quotaAllocatorsPerfectused[q.Name()] = q
		} else if result == 1 {
			quotaAllocatorsUnderused[q.Name()] = q
		}
	}
	glog.V(4).Infof("Divided quotaAllocators into three categories, quotaAllocatorsOverused(%d), quotaAllocatorsPerfectused(%d), quotaAllocatorsUnderused(%d)",
		len(quotaAllocatorsOverused), len(quotaAllocatorsPerfectused), len(quotaAllocatorsUnderused))

	preemptingResources := map[apiv1.ResourceName]resource.Quantity{
		"cpu":    resource.MustParse("0"),
		"memory": resource.MustParse("0"),
	}

	// handler quotaAllocatorsOverused which will be preempted resources to other quotaAllocator
	preemptingPods := make(map[string]preemptedPodInfo)
	for _, q := range quotaAllocatorsOverused {
		if q.UsedUnderDeserved() {
			glog.V(4).Infof("Overused quotaAllocator %s and it is Used <= Deserved, no pod terminated for it", q.Name())
			// Used <= Deserved
			// update Allocated to Deserved directly
			q.QuotaAllocator().Status.Allocated.Resources = q.QuotaAllocator().Status.Deserved.Resources
		} else {
			glog.V(4).Infof("Overused quotaAllocator %s and it is Used > Deserved, some pods will be terminated for it", q.Name())
			// Used > Deserved
			// kill pod randomly to make Used <= Deserved
			// after the pod is terminated, it will release some resource to other quotaAllocators
			leftPods, pod, findPod := popPod(q.Pods)
			for findPod {
				// skip if Used <= Deserved
				if q.UsedUnderDeserved() {
					leftPods = addPodFront(leftPods, pod)
					break
				}

				// released resource by the killed pod
				// it may be not same as its occupied resources
				releasingResources := map[apiv1.ResourceName]resource.Quantity{
					"cpu":    resource.MustParse("0"),
					"memory": resource.MustParse("0"),
				}

				// calculate releasing resources of pod
				podResources := calculatePodResources(pod)
				for k, res := range podResources {
					if k != "cpu" && k != "memory" {
						continue
					}

					resType := apiv1.ResourceName(k)
					usedResource := q.QuotaAllocator().Status.Used.Resources[resType].DeepCopy()
					deservedResource := q.QuotaAllocator().Status.Deserved.Resources[resType].DeepCopy()
					if usedResource.Cmp(deservedResource) == 1 {
						usedResource.Sub(deservedResource)
						if usedResource.Cmp(res) <= 0 {
							releasingResources[resType] = usedResource
						} else {
							releasingResources[resType] = res
						}
					}

					result := q.QuotaAllocator().Status.Used.Resources[resType].DeepCopy()
					result.Sub(res)
					q.QuotaAllocator().Status.Used.Resources[resType] = result
				}

				preemptingResources = schedulercache.ResourcesAdd(preemptingResources, releasingResources)
				preemptingPods[pod.Name] = preemptedPodInfo{
					pod: pod,
					totalReleasingResources: releasingResources,
				}

				leftPods, pod, findPod = popPod(leftPods)
				glog.V(4).Infof("Pod %s will be terminated to release resources (%#v)", pod.Name, releasingResources)
			}
			q.Pods = leftPods
			q.QuotaAllocator().Status.Allocated.Resources = q.QuotaAllocator().Status.Deserved.Resources
		}
	}

	p.localDataMutex.Lock()
	// record preempting pods
	for k, v := range preemptingPods {
		p.terminatingPodsForPreempt[k] = v
	}
	p.totalPreemptingResources = schedulercache.ResourcesAdd(p.totalPreemptingResources, preemptingResources)
	// copy totalPreemptingResources for scheduling
	leftPreemptingResources := make(map[apiv1.ResourceName]resource.Quantity)
	for k, v := range p.totalPreemptingResources {
		leftPreemptingResources[k] = v
	}
	p.localDataMutex.Unlock()

	resourceTypes := []string{"cpu", "memory"}
	// handler quotaAllocatorsUnderused which will preempt resources from other quotaAllocator
	for _, q := range quotaAllocatorsUnderused {
		if schedulercache.ResourcesIsZero(leftPreemptingResources) {
			// there is no preempting resources left
			// change Allocated to Deserved directly
			q.QuotaAllocator().Status.Allocated.Resources = q.QuotaAllocator().Status.Deserved.Resources
		} else {
			// assign preempting resources to quotaAllocator first
			unmetResources := schedulercache.ResourcesSub(q.QuotaAllocator().Status.Deserved.Resources, q.QuotaAllocator().Status.Allocated.Resources)

			for _, res := range resourceTypes {
				resType := apiv1.ResourceName(res)
				leftRes := leftPreemptingResources[resType].DeepCopy()
				unmetRes := unmetResources[resType].DeepCopy()
				if unmetRes.Cmp(leftRes) <= 0 {
					leftRes.Sub(unmetRes)
					leftPreemptingResources[resType] = leftRes
				} else {
					unmetRes.Sub(leftRes)
					leftPreemptingResources[resType] = resource.MustParse("0")
					allocatedRes := q.QuotaAllocator().Status.Allocated.Resources[resType].DeepCopy()
					allocatedRes.Add(unmetRes)
					q.QuotaAllocator().Status.Allocated.Resources[resType] = allocatedRes
				}
			}
		}
	}
	if !schedulercache.ResourcesIsZero(leftPreemptingResources) {
		glog.Errorf("leftPreemptingResources is not empty, something error, %#v", leftPreemptingResources)
	}

	// update QuotaAllocator to API server under p.updateQuotaAllocatorMutex
	p.updateQuotaAllocatorMutex.Lock()
	updateQuotaAllocators(quotaAllocatorsOverused, p.config)
	updateQuotaAllocators(quotaAllocatorsUnderused, p.config)
	p.updateQuotaAllocatorMutex.Unlock()

	p.localDataMutex.Lock()
	// terminate pod after quotaAllocator is updated
	for _, v := range preemptingPods {
		if err := killPod(p.client, v.pod); err != nil {
			// kill pod failed, it may be terminated before
			// TODO call terminatePodDone later to update quotaAllocator
			glog.Errorf("Terminate pod %s failed", v.pod.Name)
		}
	}
	p.localDataMutex.Unlock()

	return nil
}

func (p *basePreemption) terminatePodDone(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			glog.Errorf("Cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.Pod: %v", t)
		return
	}

	p.localDataMutex.Lock()
	defer p.localDataMutex.Unlock()
	// if the pod is terminated for underused, remove it from terminatingPodsForUnderused directly
	if _, ok := p.terminatingPodsForUnderused[pod.Name]; ok {
		delete(p.terminatingPodsForUnderused, pod.Name)
	}
	// if the pod is terminated for preemption, remove it from terminatingPods, update totalPreemptingResources
	if ppInfo, ok := p.terminatingPodsForPreempt[pod.Name]; ok {
		delete(p.terminatingPodsForPreempt, pod.Name)
		p.totalPreemptingResources = schedulercache.ResourcesSub(p.totalPreemptingResources, ppInfo.totalReleasingResources)
	}
}

func sortPodByPriority(pods map[string]*v1.Pod) []*v1.Pod {
	sortedPods := util.PodSlice{}

	for _, pod := range pods {
		sortedPods = append(sortedPods, pod)
	}
	sort.Sort(sortedPods)

	return sortedPods
}

func popPod(pods []*v1.Pod) ([]*v1.Pod, *v1.Pod, bool) {
	if len(pods) == 0 {
		return nil, nil, false
	}

	pod := pods[0]
	leftPods := append(pods[:0], pods[1:]...)

	return leftPods, pod, true
}

func addPodFront(pods []*v1.Pod, pod *v1.Pod) []*v1.Pod {
	front := append([]*v1.Pod{}, pod)
	result := append(front[0:], pods...)

	return result
}
