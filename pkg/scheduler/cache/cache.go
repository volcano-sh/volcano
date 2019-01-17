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
	infov1 "k8s.io/client-go/informers/core/v1"
	policyv1 "k8s.io/client-go/informers/policy/v1beta1"
	storagev1 "k8s.io/client-go/informers/storage/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"k8s.io/kubernetes/pkg/scheduler/volumebinder"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	"github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned/scheme"
	kbinfo "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinfov1 "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	kbapi "github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

// New returns a Cache implementation.
func New(config *rest.Config, schedulerName string, nsAsQueue bool) Cache {
	return newSchedulerCache(config, schedulerName, nsAsQueue)
}

type SchedulerCache struct {
	sync.Mutex

	kubeclient *kubernetes.Clientset
	kbclient   *kbver.Clientset

	podInformer      infov1.PodInformer
	nodeInformer     infov1.NodeInformer
	pdbInformer      policyv1.PodDisruptionBudgetInformer
	nsInformer       infov1.NamespaceInformer
	podGroupInformer kbinfov1.PodGroupInformer
	queueInformer    kbinfov1.QueueInformer
	pvInformer       infov1.PersistentVolumeInformer
	pvcInformer      infov1.PersistentVolumeClaimInformer
	scInformer       storagev1.StorageClassInformer

	Binder            Binder
	Evictor           Evictor
	TaskStatusUpdater TaskStatusUpdater
	VolumeBinder      VolumeBinder

	recorder record.EventRecorder

	Jobs   map[kbapi.JobID]*kbapi.JobInfo
	Nodes  map[string]*kbapi.NodeInfo
	Queues map[kbapi.QueueID]*kbapi.QueueInfo

	errTasks    *cache.FIFO
	deletedJobs *cache.FIFO

	namespaceAsQueue bool
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

	glog.V(3).Infof("Evicting pod %v/%v", p.Namespace, p.Name)

	if err := de.kubeclient.CoreV1().Pods(p.Namespace).Delete(p.Name, &metav1.DeleteOptions{
		GracePeriodSeconds: &threeSecs,
	}); err != nil {
		glog.Errorf("Failed to evict pod <%v/%v>: %#v", p.Namespace, p.Name, err)
		return err
	}
	return nil
}

// defaultTaskStatusUpdater is the default implementation of the TaskStatusUpdater interface
type defaultTaskStatusUpdater struct {
	kubeclient *kubernetes.Clientset
}

// Update pod with podCondition
func (tsUpdater *defaultTaskStatusUpdater) Update(pod *v1.Pod, condition *v1.PodCondition) error {
	glog.V(3).Infof("Updating pod condition for %s/%s to (%s==%s)", pod.Namespace, pod.Name, condition.Type, condition.Status)
	if podutil.UpdatePodCondition(&pod.Status, condition) {
		_, err := tsUpdater.kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(pod)
		return err
	}
	return nil
}

type defaultVolumeBinder struct {
	volumeBinder *volumebinder.VolumeBinder
}

// AllocateVolume allocates volume on the host to the task
func (dvb *defaultVolumeBinder) AllocateVolumes(task *api.TaskInfo, hostname string) error {
	allBound, err := dvb.volumeBinder.Binder.AssumePodVolumes(task.Pod, hostname)
	task.VolumeReady = allBound

	return err
}

// BindVolume binds volumes to the task
func (dvb *defaultVolumeBinder) BindVolumes(task *api.TaskInfo) error {
	// If task's volumes are ready, did not bind them again.
	if task.VolumeReady {
		return nil
	}

	return dvb.volumeBinder.Binder.BindPodVolumes(task.Pod)
}

func taskKey(obj interface{}) (string, error) {
	if obj == nil {
		return "", fmt.Errorf("the object is nil")
	}

	task, ok := obj.(*kbapi.TaskInfo)

	if !ok {
		return "", fmt.Errorf("failed to convert %v to TaskInfo", obj)
	}

	return string(task.UID), nil
}

func jobKey(obj interface{}) (string, error) {
	if obj == nil {
		return "", fmt.Errorf("the object is nil")
	}

	job, ok := obj.(*kbapi.JobInfo)

	if !ok {
		return "", fmt.Errorf("failed to convert %v to TaskInfo", obj)
	}

	return string(job.UID), nil
}

func newSchedulerCache(config *rest.Config, schedulerName string, nsAsQueue bool) *SchedulerCache {
	sc := &SchedulerCache{
		Jobs:             make(map[kbapi.JobID]*kbapi.JobInfo),
		Nodes:            make(map[string]*kbapi.NodeInfo),
		Queues:           make(map[kbapi.QueueID]*kbapi.QueueInfo),
		errTasks:         cache.NewFIFO(taskKey),
		deletedJobs:      cache.NewFIFO(jobKey),
		kubeclient:       kubernetes.NewForConfigOrDie(config),
		kbclient:         kbver.NewForConfigOrDie(config),
		namespaceAsQueue: nsAsQueue,
	}

	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: sc.kubeclient.CoreV1().Events("")})
	sc.recorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "kube-batch"})

	sc.Binder = &defaultBinder{
		kubeclient: sc.kubeclient,
	}

	sc.Evictor = &defaultEvictor{
		kubeclient: sc.kubeclient,
	}

	sc.TaskStatusUpdater = &defaultTaskStatusUpdater{kubeclient: sc.kubeclient}

	informerFactory := informers.NewSharedInformerFactory(sc.kubeclient, 0)

	sc.pvcInformer = informerFactory.Core().V1().PersistentVolumeClaims()
	sc.pvInformer = informerFactory.Core().V1().PersistentVolumes()
	sc.scInformer = informerFactory.Storage().V1().StorageClasses()
	sc.VolumeBinder = &defaultVolumeBinder{
		volumeBinder: volumebinder.NewVolumeBinder(
			sc.kubeclient,
			sc.pvcInformer,
			sc.pvInformer,
			sc.scInformer,
			30*time.Second,
		),
	}

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
					return pod.Status.Phase != v1.PodPending
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

	kbinformer := kbinfo.NewSharedInformerFactory(sc.kbclient, 0)
	// create informer for PodGroup information
	sc.podGroupInformer = kbinformer.Scheduling().V1alpha1().PodGroups()
	sc.podGroupInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddPodGroup,
		UpdateFunc: sc.UpdatePodGroup,
		DeleteFunc: sc.DeletePodGroup,
	})

	if sc.namespaceAsQueue {
		// create informer for Namespace information
		sc.nsInformer = informerFactory.Core().V1().Namespaces()
		sc.nsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddNamespace,
			UpdateFunc: sc.UpdateNamespace,
			DeleteFunc: sc.DeleteNamespace,
		})
	} else {
		// create informer for Queue information
		sc.queueInformer = kbinformer.Scheduling().V1alpha1().Queues()
		sc.queueInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddQueue,
			UpdateFunc: sc.UpdateQueue,
			DeleteFunc: sc.DeleteQueue,
		})
	}

	return sc
}

func (sc *SchedulerCache) Run(stopCh <-chan struct{}) {
	go sc.pdbInformer.Informer().Run(stopCh)
	go sc.podInformer.Informer().Run(stopCh)
	go sc.nodeInformer.Informer().Run(stopCh)
	go sc.podGroupInformer.Informer().Run(stopCh)
	go sc.pvInformer.Informer().Run(stopCh)
	go sc.pvcInformer.Informer().Run(stopCh)
	go sc.scInformer.Informer().Run(stopCh)

	if sc.namespaceAsQueue {
		go sc.nsInformer.Informer().Run(stopCh)
	} else {
		go sc.queueInformer.Informer().Run(stopCh)
	}

	// Re-sync error tasks.
	go sc.resync()

	// Cleanup jobs.
	go sc.cleanupJobs()
}

func (sc *SchedulerCache) WaitForCacheSync(stopCh <-chan struct{}) bool {
	var queueSync func() bool
	if sc.namespaceAsQueue {
		queueSync = sc.nsInformer.Informer().HasSynced
	} else {
		queueSync = sc.queueInformer.Informer().HasSynced
	}

	return cache.WaitForCacheSync(stopCh,
		sc.pdbInformer.Informer().HasSynced,
		sc.podInformer.Informer().HasSynced,
		sc.podGroupInformer.Informer().HasSynced,
		sc.nodeInformer.Informer().HasSynced,
		sc.pvInformer.Informer().HasSynced,
		sc.pvcInformer.Informer().HasSynced,
		sc.scInformer.Informer().HasSynced,
		queueSync,
	)
}

func (sc *SchedulerCache) findJobAndTask(taskInfo *kbapi.TaskInfo) (*kbapi.JobInfo, *kbapi.TaskInfo, error) {
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

func (sc *SchedulerCache) Evict(taskInfo *kbapi.TaskInfo, reason string) error {
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

	err = job.UpdateTaskStatus(task, kbapi.Releasing)
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

	sc.recorder.Eventf(job.PodGroup, v1.EventTypeNormal, string(kbv1.EvictEvent), reason)

	return nil
}

// Bind binds task to the target host.
func (sc *SchedulerCache) Bind(taskInfo *kbapi.TaskInfo, hostname string) error {
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

	err = job.UpdateTaskStatus(task, kbapi.Binding)
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

// AllocateVolume allocates volume on the host to the task
func (sc *SchedulerCache) AllocateVolumes(task *api.TaskInfo, hostname string) error {
	return sc.VolumeBinder.AllocateVolumes(task, hostname)
}

// BindVolume binds volumes to the task
func (sc *SchedulerCache) BindVolumes(task *api.TaskInfo) error {
	return sc.VolumeBinder.BindVolumes(task)
}

// TaskUnschedulable updates pod status of pending task
func (sc *SchedulerCache) TaskUnschedulable(task *api.TaskInfo, event kbv1.Event, reason string) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	pod := task.Pod.DeepCopy()

	sc.recorder.Eventf(pod, v1.EventTypeWarning, string(event), reason)
	sc.TaskStatusUpdater.Update(pod, &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  v1.PodReasonUnschedulable,
		Message: reason,
	})

	return nil
}

func (sc *SchedulerCache) deleteJob(job *kbapi.JobInfo) {
	glog.V(3).Infof("Try to delete Job <%v:%v/%v>", job.UID, job.Namespace, job.Name)

	time.AfterFunc(5*time.Second, func() {
		sc.deletedJobs.AddIfNotPresent(job)
	})
}

func (sc *SchedulerCache) processCleanupJob() error {
	_, err := sc.deletedJobs.Pop(func(obj interface{}) error {
		job, ok := obj.(*kbapi.JobInfo)
		if !ok {
			return fmt.Errorf("failed to convert %v to *v1.Pod", obj)
		}

		func() {
			sc.Mutex.Lock()
			defer sc.Mutex.Unlock()

			if kbapi.JobTerminated(job) {
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

func (sc *SchedulerCache) resyncTask(task *kbapi.TaskInfo) {
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
		task, ok := obj.(*kbapi.TaskInfo)
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

func (sc *SchedulerCache) Snapshot() *kbapi.ClusterInfo {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &kbapi.ClusterInfo{
		Nodes:  make([]*kbapi.NodeInfo, 0, len(sc.Nodes)),
		Jobs:   make([]*kbapi.JobInfo, 0, len(sc.Jobs)),
		Queues: make([]*kbapi.QueueInfo, 0, len(sc.Queues)),
		Others: make([]*kbapi.TaskInfo, 0, 10),
	}

	for _, value := range sc.Nodes {
		snapshot.Nodes = append(snapshot.Nodes, value.Clone())
	}

	queues := map[kbapi.QueueID]struct{}{}
	for _, value := range sc.Queues {
		snapshot.Queues = append(snapshot.Queues, value.Clone())
		queues[value.UID] = struct{}{}
	}

	for _, value := range sc.Jobs {
		// If no scheduling spec, does not handle it.
		if value.PodGroup == nil && value.PDB == nil {
			glog.V(3).Infof("The scheduling spec of Job <%v:%s/%s> is nil, ignore it.",
				value.UID, value.Namespace, value.Name)

			// Also tracing the running task assigned by other scheduler.
			for _, task := range value.TaskStatusIndex[kbapi.Running] {
				snapshot.Others = append(snapshot.Others, task.Clone())
			}

			continue
		}

		if _, found := queues[value.Queue]; !found {
			glog.V(3).Infof("The Queue <%v> of Job <%v> does not exist, ignore it.",
				value.Queue, value.UID)
			continue
		}

		snapshot.Jobs = append(snapshot.Jobs, value.Clone())
	}

	glog.V(3).Infof("There are <%d> Jobs and <%d> Queues in total for scheduling.",
		len(snapshot.Jobs), len(snapshot.Queues))

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

// Backoff record event for job
func (sc *SchedulerCache) Backoff(job *kbapi.JobInfo, event kbv1.Event, reason string) error {
	if job.PodGroup != nil {
		sc.recorder.Eventf(job.PodGroup, v1.EventTypeWarning, string(event), reason)
	} else if job.PDB != nil {
		sc.recorder.Eventf(job.PDB, v1.EventTypeWarning, string(event), reason)
	} else {
		return fmt.Errorf("no scheduling specification for job")
	}

	for _, tasks := range job.TaskStatusIndex {
		for _, t := range tasks {
			sc.recorder.Eventf(t.Pod, v1.EventTypeWarning, string(event), reason)
		}
	}

	return nil
}
