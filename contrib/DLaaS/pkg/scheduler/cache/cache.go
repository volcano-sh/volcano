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

package cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	clientv1 "k8s.io/client-go/informers/core/v1"
	policyv1 "k8s.io/client-go/informers/policy/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	client "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned/clients"
	informerfactory "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion"
	arbclient "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion/v1"
	arbapi "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
)

// New returns a Cache implementation.
func New(config *rest.Config, schedulerName string) Cache {
	return newSchedulerCache(config, schedulerName)
}

type SchedulerCache struct {
	sync.Mutex

	kubeclient *kubernetes.Clientset

	podInformer            clientv1.PodInformer
	nodeInformer           clientv1.NodeInformer
	pdbInformer            policyv1.PodDisruptionBudgetInformer
	schedulingSpecInformer arbclient.SchedulingSpecInformer

	Binder  Binder
	Evictor Evictor

	Jobs  map[arbapi.JobID]*arbapi.JobInfo
	Nodes map[string]*arbapi.NodeInfo

	errTasks    *cache.FIFO
	deletedJobs *cache.FIFO
}

type defaultBinder struct {
	kubeclient *kubernetes.Clientset
}

func (db *defaultBinder) Bind(p *v1.Pod, hostname string) error {
	if err := db.kubeclient.CoreV1().Pods(p.Namespace).Bind(&v1.Binding{
		ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: p.UID},
		Target: v1.ObjectReference{
			Kind: "Node",
			Name: hostname,
		},
	}); err != nil {
		glog.Errorf("Failed to bind pod <%v/%v>: %#v", p.Namespace, p.Name, err)
		return err
	}
	return nil
}

type defaultEvictor struct {
	kubeclient *kubernetes.Clientset
}

func (de *defaultEvictor) Evict(p *v1.Pod) error {
	// TODO (k82cn): makes grace period configurable.
	threeSecs := int64(3)

	if err := de.kubeclient.CoreV1().Pods(p.Namespace).Delete(p.Name, &metav1.DeleteOptions{
		GracePeriodSeconds: &threeSecs,
	}); err != nil {
		glog.Errorf("Failed to evict pod <%v/%v>: %#v", p.Namespace, p.Name, err)
		return err
	}
	return nil
}

func taskKey(obj interface{}) (string, error) {
	if obj == nil {
		return "", fmt.Errorf("the object is nil")
	}

	task, ok := obj.(*arbapi.TaskInfo)

	if !ok {
		return "", fmt.Errorf("failed to convert %v to TaskInfo", obj)
	}

	return string(task.UID), nil
}

func jobKey(obj interface{}) (string, error) {
	if obj == nil {
		return "", fmt.Errorf("the object is nil")
	}

	job, ok := obj.(*arbapi.JobInfo)

	if !ok {
		return "", fmt.Errorf("failed to convert %v to TaskInfo", obj)
	}

	return string(job.UID), nil
}

func newSchedulerCache(config *rest.Config, schedulerName string) *SchedulerCache {
	sc := &SchedulerCache{
		Jobs:        make(map[arbapi.JobID]*arbapi.JobInfo),
		Nodes:       make(map[string]*arbapi.NodeInfo),
		errTasks:    cache.NewFIFO(taskKey),
		deletedJobs: cache.NewFIFO(jobKey),
	}

	sc.kubeclient = kubernetes.NewForConfigOrDie(config)

	sc.Binder = &defaultBinder{
		kubeclient: sc.kubeclient,
	}

	sc.Evictor = &defaultEvictor{
		kubeclient: sc.kubeclient,
	}

	informerFactory := informers.NewSharedInformerFactory(sc.kubeclient, 0)

	// create informer for node information
	sc.nodeInformer = informerFactory.Core().V1().Nodes()
	sc.nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddNode,
			UpdateFunc: sc.UpdateNode,
			DeleteFunc: sc.DeleteNode,
		},
		0,
	)

	// create informer for pod information
	sc.podInformer = informerFactory.Core().V1().Pods()
	sc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch obj.(type) {
				case *v1.Pod:
					pod := obj.(*v1.Pod)
					if strings.Compare(pod.Spec.SchedulerName, schedulerName) == 0 && pod.Status.Phase == v1.PodPending {
						return true
					}
					return pod.Status.Phase == v1.PodRunning
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddPod,
				UpdateFunc: sc.UpdatePod,
				DeleteFunc: sc.DeletePod,
			},
		})

	sc.pdbInformer = informerFactory.Policy().V1beta1().PodDisruptionBudgets()
	sc.pdbInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddPDB,
		UpdateFunc: sc.UpdatePDB,
		DeleteFunc: sc.DeletePDB,
	})

	// create queue informer
	queueClient, _, err := client.NewClient(config)
	if err != nil {
		panic(err)
	}

	schedulingSpecInformerFactory := informerfactory.NewSharedInformerFactory(queueClient, 0)
	// create informer for Queue information
	sc.schedulingSpecInformer = schedulingSpecInformerFactory.SchedulingSpec().SchedulingSpecs()
	sc.schedulingSpecInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddSchedulingSpec,
		UpdateFunc: sc.UpdateSchedulingSpec,
		DeleteFunc: sc.DeleteSchedulingSpec,
	})

	return sc
}

func (sc *SchedulerCache) Run(stopCh <-chan struct{}) {
	go sc.pdbInformer.Informer().Run(stopCh)
	go sc.podInformer.Informer().Run(stopCh)
	go sc.nodeInformer.Informer().Run(stopCh)
	go sc.schedulingSpecInformer.Informer().Run(stopCh)

	// Re-sync error tasks.
	go sc.resync()

	// Cleanup jobs.
	go sc.cleanupJobs()
}

func (sc *SchedulerCache) WaitForCacheSync(stopCh <-chan struct{}) bool {
	return cache.WaitForCacheSync(stopCh,
		sc.pdbInformer.Informer().HasSynced,
		sc.podInformer.Informer().HasSynced,
		sc.schedulingSpecInformer.Informer().HasSynced,
		sc.nodeInformer.Informer().HasSynced)
}

func (sc *SchedulerCache) findJobAndTask(taskInfo *arbapi.TaskInfo) (*arbapi.JobInfo, *arbapi.TaskInfo, error) {
	job, found := sc.Jobs[taskInfo.Job]
	if !found {
		return nil, nil, fmt.Errorf("failed to find Job %v for Task %v",
			taskInfo.Job, taskInfo.UID)
	}

	task, found := job.Tasks[taskInfo.UID]
	if !found {
		return nil, nil, fmt.Errorf("failed to find task in status %v by id %v",
			taskInfo.Status, taskInfo.UID)
	}

	return job, task, nil
}

func (sc *SchedulerCache) Evict(taskInfo *arbapi.TaskInfo) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	job, task, err := sc.findJobAndTask(taskInfo)

	if err != nil {
		return err
	}

	node, found := sc.Nodes[task.NodeName]
	if !found {
		return fmt.Errorf("failed to bind Task %v to host %v, host does not exist",
			task.UID, task.NodeName)
	}

	err = job.UpdateTaskStatus(task, arbapi.Releasing)
	if err != nil {
		return err
	}

	// Add new task to node.
	node.UpdateTask(task)

	p := task.Pod

	go func() {
		err := sc.Evictor.Evict(p)
		if err != nil {
			sc.resyncTask(task)
		}
	}()

	return nil
}

// Bind binds task to the target host.
func (sc *SchedulerCache) Bind(taskInfo *arbapi.TaskInfo, hostname string) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	job, task, err := sc.findJobAndTask(taskInfo)

	if err != nil {
		return err
	}

	node, found := sc.Nodes[hostname]
	if !found {
		return fmt.Errorf("failed to bind Task %v to host %v, host does not exist",
			task.UID, hostname)
	}

	err = job.UpdateTaskStatus(task, arbapi.Binding)
	if err != nil {
		return err
	}

	// Set `.nodeName` to the hostname
	task.NodeName = hostname

	// Add task to the node.
	node.AddTask(task)

	p := task.Pod

	go func() {
		if err := sc.Binder.Bind(p, hostname); err != nil {
			sc.resyncTask(task)
		}
	}()

	return nil
}

func (sc *SchedulerCache) deleteJob(job *arbapi.JobInfo) {
	glog.V(3).Infof("Try to delete Job <%v:%v/%v>", job.UID, job.Namespace, job.Name)

	time.AfterFunc(5*time.Second, func() {
		sc.deletedJobs.AddIfNotPresent(job)
	})
}

func (sc *SchedulerCache) processCleanupJob() error {
	_, err := sc.deletedJobs.Pop(func(obj interface{}) error {
		job, ok := obj.(*arbapi.JobInfo)
		if !ok {
			return fmt.Errorf("failed to convert %v to *v1.Pod", obj)
		}

		func() {
			sc.Mutex.Lock()
			defer sc.Mutex.Unlock()

			if arbapi.JobTerminated(job) {
				delete(sc.Jobs, job.UID)
				glog.V(3).Infof("Job <%v:%v/%v> was deleted.", job.UID, job.Namespace, job.Name)
			} else {
				// Retry
				sc.deleteJob(job)
			}
		}()

		return nil
	})

	return err
}

func (sc *SchedulerCache) cleanupJobs() {
	for {
		err := sc.processCleanupJob()
		if err != nil {
			glog.Errorf("Failed to process job clean up: %v", err)
		}
	}
}

func (sc *SchedulerCache) resyncTask(task *arbapi.TaskInfo) {
	sc.errTasks.AddIfNotPresent(task)
}

func (sc *SchedulerCache) resync() {
	for {
		err := sc.processResyncTask()
		if err != nil {
			glog.Errorf("Failed to process resync: %v", err)
		}
	}
}

func (sc *SchedulerCache) processResyncTask() error {
	_, err := sc.errTasks.Pop(func(obj interface{}) error {
		task, ok := obj.(*arbapi.TaskInfo)
		if !ok {
			return fmt.Errorf("failed to convert %v to *v1.Pod", obj)
		}

		if err := sc.syncTask(task); err != nil {
			glog.Errorf("Failed to sync pod <%v/%v>", task.Namespace, task.Name)
			return err
		}
		return nil
	})

	return err
}

func (sc *SchedulerCache) Snapshot() *arbapi.ClusterInfo {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &arbapi.ClusterInfo{
		Nodes: make([]*arbapi.NodeInfo, 0, len(sc.Nodes)),
		Jobs:  make([]*arbapi.JobInfo, 0, len(sc.Jobs)),
	}

	for _, value := range sc.Nodes {
		snapshot.Nodes = append(snapshot.Nodes, value.Clone())
	}

	for _, value := range sc.Jobs {
		// If no scheduling spec, does not handle it.
		if value.SchedSpec == nil && value.PDB == nil {
			glog.V(3).Infof("The scheduling spec of Job <%v> is nil, ignore it.", value.UID)
			continue
		}

		snapshot.Jobs = append(snapshot.Jobs, value.Clone())
	}

	return snapshot
}

func (sc *SchedulerCache) LoadSchedulerConf(path string) (map[string]string, error) {
	ns, name, err := cache.SplitMetaNamespaceKey(path)
	if err != nil {
		return nil, err
	}

	confMap, err := sc.kubeclient.CoreV1().ConfigMaps(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return confMap.Data, nil
}

func (sc *SchedulerCache) String() string {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	str := "Cache:\n"

	if len(sc.Nodes) != 0 {
		str = str + "Nodes:\n"
		for _, n := range sc.Nodes {
			str = str + fmt.Sprintf("\t %s: idle(%v) used(%v) allocatable(%v) pods(%d)\n",
				n.Name, n.Idle, n.Used, n.Allocatable, len(n.Tasks))

			i := 0
			for _, p := range n.Tasks {
				str = str + fmt.Sprintf("\t\t %d: %v\n", i, p)
				i++
			}
		}
	}

	if len(sc.Jobs) != 0 {
		str = str + "Jobs:\n"
		for _, job := range sc.Jobs {
			str = str + fmt.Sprintf("\t Job(%s) name(%s) minAvailable(%v)\n",
				job.UID, job.Name, job.MinAvailable)

			i := 0
			for _, task := range job.Tasks {
				str = str + fmt.Sprintf("\t\t %d: %v\n", i, task)
				i++
			}
		}
	}

	return str
}
