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
	"sync"
	"time"

	"github.com/golang/glog"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/scheduling/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	infov1 "k8s.io/client-go/informers/core/v1"
	policyv1 "k8s.io/client-go/informers/policy/v1beta1"
	schedv1 "k8s.io/client-go/informers/scheduling/v1beta1"
	storagev1 "k8s.io/client-go/informers/storage/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"k8s.io/kubernetes/pkg/scheduler/volumebinder"

	"github.com/kubernetes-sigs/kube-batch/cmd/kube-batch/app/options"
	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	kbver "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	"github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned/scheme"
	kbschema "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned/scheme"
	kbinfo "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinfov1 "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	kbapi "github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
)

func init() {
	schemeBuilder := runtime.SchemeBuilder{
		v1.AddToScheme,
	}

	utilruntime.Must(schemeBuilder.AddToScheme(kbschema.Scheme))
}

// New returns a Cache implementation.
func New(config *rest.Config, schedulerName string, defaultQueue string) Cache {
	return newSchedulerCache(config, schedulerName, defaultQueue)
}

//SchedulerCache cache for the kube batch
type SchedulerCache struct {
	sync.Mutex

	kubeclient *kubernetes.Clientset
	kbclient   *kbver.Clientset

	defaultQueue string
	// schedulerName is the name for kube batch scheduler
	schedulerName string

	podInformer      infov1.PodInformer
	nodeInformer     infov1.NodeInformer
	pdbInformer      policyv1.PodDisruptionBudgetInformer
	nsInformer       infov1.NamespaceInformer
	podGroupInformer kbinfov1.PodGroupInformer
	queueInformer    kbinfov1.QueueInformer
	pvInformer       infov1.PersistentVolumeInformer
	pvcInformer      infov1.PersistentVolumeClaimInformer
	scInformer       storagev1.StorageClassInformer
	pcInformer       schedv1.PriorityClassInformer

	Binder        Binder
	Evictor       Evictor
	StatusUpdater StatusUpdater
	VolumeBinder  VolumeBinder

	Recorder record.EventRecorder

	Jobs                 map[kbapi.JobID]*kbapi.JobInfo
	Nodes                map[string]*kbapi.NodeInfo
	Queues               map[kbapi.QueueID]*kbapi.QueueInfo
	PriorityClasses      map[string]*v1beta1.PriorityClass
	defaultPriorityClass *v1beta1.PriorityClass
	defaultPriority      int32

	errTasks    workqueue.RateLimitingInterface
	deletedJobs workqueue.RateLimitingInterface
}

type defaultBinder struct {
	kubeclient *kubernetes.Clientset
}

//Bind will send bind request to api server
func (db *defaultBinder) Bind(p *v1.Pod, hostname string) error {
	if err := db.kubeclient.CoreV1().Pods(p.Namespace).Bind(&v1.Binding{
		ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: p.UID, Annotations: p.Annotations},
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

//Evict will send delete pod request to api server
func (de *defaultEvictor) Evict(p *v1.Pod) error {
	glog.V(3).Infof("Evicting pod %v/%v", p.Namespace, p.Name)

	if err := de.kubeclient.CoreV1().Pods(p.Namespace).Delete(p.Name, nil); err != nil {
		glog.Errorf("Failed to evict pod <%v/%v>: %#v", p.Namespace, p.Name, err)
		return err
	}
	return nil
}

// defaultStatusUpdater is the default implementation of the StatusUpdater interface
type defaultStatusUpdater struct {
	kubeclient *kubernetes.Clientset
	kbclient   *kbver.Clientset
}

// following the same logic as podutil.UpdatePodCondition
func podConditionHaveUpdate(status *v1.PodStatus, condition *v1.PodCondition) bool {
	lastTransitionTime := metav1.Now()
	// Try to find this pod condition.
	_, oldCondition := podutil.GetPodCondition(status, condition.Type)

	if oldCondition == nil {
		// We are adding new pod condition.
		return true
	}
	// We are updating an existing condition, so we need to check if it has changed.
	if condition.Status == oldCondition.Status {
		lastTransitionTime = oldCondition.LastTransitionTime
	}

	isEqual := condition.Status == oldCondition.Status &&
		condition.Reason == oldCondition.Reason &&
		condition.Message == oldCondition.Message &&
		condition.LastProbeTime.Equal(&oldCondition.LastProbeTime) &&
		lastTransitionTime.Equal(&oldCondition.LastTransitionTime)

	// Return true if one of the fields have changed.
	return !isEqual
}

// UpdatePodCondition will Update pod with podCondition
func (su *defaultStatusUpdater) UpdatePodCondition(pod *v1.Pod, condition *v1.PodCondition) (*v1.Pod, error) {
	glog.V(3).Infof("Updating pod condition for %s/%s to (%s==%s)", pod.Namespace, pod.Name, condition.Type, condition.Status)
	if podutil.UpdatePodCondition(&pod.Status, condition) {
		return su.kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(pod)
	}
	return pod, nil
}

// UpdatePodGroup will Update pod with podCondition
func (su *defaultStatusUpdater) UpdatePodGroup(pg *v1alpha1.PodGroup) (*v1alpha1.PodGroup, error) {
	return su.kbclient.SchedulingV1alpha1().PodGroups(pg.Namespace).Update(pg)
}

type defaultVolumeBinder struct {
	volumeBinder *volumebinder.VolumeBinder
}

// AllocateVolumes allocates volume on the host to the task
func (dvb *defaultVolumeBinder) AllocateVolumes(task *api.TaskInfo, hostname string) error {
	allBound, err := dvb.volumeBinder.Binder.AssumePodVolumes(task.Pod, hostname)
	task.VolumeReady = allBound

	return err
}

// BindVolumes binds volumes to the task
func (dvb *defaultVolumeBinder) BindVolumes(task *api.TaskInfo) error {
	// If task's volumes are ready, did not bind them again.
	if task.VolumeReady {
		return nil
	}

	return dvb.volumeBinder.Binder.BindPodVolumes(task.Pod)
}

func newSchedulerCache(config *rest.Config, schedulerName string, defaultQueue string) *SchedulerCache {
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("failed init kubeClient, with err: %v", err))
	}
	kbClient, err := kbver.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("failed init kbClient, with err: %v", err))
	}
	eventClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("failed init eventClient, with err: %v", err))
	}

	sc := &SchedulerCache{
		Jobs:            make(map[kbapi.JobID]*kbapi.JobInfo),
		Nodes:           make(map[string]*kbapi.NodeInfo),
		Queues:          make(map[kbapi.QueueID]*kbapi.QueueInfo),
		PriorityClasses: make(map[string]*v1beta1.PriorityClass),
		errTasks:        workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		deletedJobs:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		kubeclient:      kubeClient,
		kbclient:        kbClient,
		defaultQueue:    defaultQueue,
		schedulerName:   schedulerName,
	}

	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: eventClient.CoreV1().Events("")})
	sc.Recorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: schedulerName})

	sc.Binder = &defaultBinder{
		kubeclient: sc.kubeclient,
	}

	sc.Evictor = &defaultEvictor{
		kubeclient: sc.kubeclient,
	}

	sc.StatusUpdater = &defaultStatusUpdater{
		kubeclient: sc.kubeclient,
		kbclient:   sc.kbclient,
	}

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
					if !responsibleForPod(pod, schedulerName) {
						if len(pod.Spec.NodeName) == 0 {
							return false
						}
					}
					return true
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

	sc.pcInformer = informerFactory.Scheduling().V1beta1().PriorityClasses()
	sc.pcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddPriorityClass,
		UpdateFunc: sc.UpdatePriorityClass,
		DeleteFunc: sc.DeletePriorityClass,
	})

	kbinformer := kbinfo.NewSharedInformerFactory(sc.kbclient, 0)
	// create informer for PodGroup information
	sc.podGroupInformer = kbinformer.Scheduling().V1alpha1().PodGroups()
	sc.podGroupInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddPodGroup,
		UpdateFunc: sc.UpdatePodGroup,
		DeleteFunc: sc.DeletePodGroup,
	})

	// create informer for Queue information
	sc.queueInformer = kbinformer.Scheduling().V1alpha1().Queues()
	sc.queueInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddQueue,
		UpdateFunc: sc.UpdateQueue,
		DeleteFunc: sc.DeleteQueue,
	})

	return sc
}

// Run  starts the schedulerCache
func (sc *SchedulerCache) Run(stopCh <-chan struct{}) {
	go sc.pdbInformer.Informer().Run(stopCh)
	go sc.podInformer.Informer().Run(stopCh)
	go sc.nodeInformer.Informer().Run(stopCh)
	go sc.podGroupInformer.Informer().Run(stopCh)
	go sc.pvInformer.Informer().Run(stopCh)
	go sc.pvcInformer.Informer().Run(stopCh)
	go sc.scInformer.Informer().Run(stopCh)
	go sc.queueInformer.Informer().Run(stopCh)

	if options.ServerOpts.EnablePriorityClass {
		go sc.pcInformer.Informer().Run(stopCh)
	}

	// Re-sync error tasks.
	go wait.Until(sc.processResyncTask, 0, stopCh)

	// Cleanup jobs.
	go wait.Until(sc.processCleanupJob, 0, stopCh)
}

// WaitForCacheSync sync the cache with the api server
func (sc *SchedulerCache) WaitForCacheSync(stopCh <-chan struct{}) bool {

	return cache.WaitForCacheSync(stopCh,
		func() []cache.InformerSynced {
			informerSynced := []cache.InformerSynced{
				sc.pdbInformer.Informer().HasSynced,
				sc.podInformer.Informer().HasSynced,
				sc.podGroupInformer.Informer().HasSynced,
				sc.nodeInformer.Informer().HasSynced,
				sc.pvInformer.Informer().HasSynced,
				sc.pvcInformer.Informer().HasSynced,
				sc.scInformer.Informer().HasSynced,
				sc.queueInformer.Informer().HasSynced,
			}
			if options.ServerOpts.EnablePriorityClass {
				informerSynced = append(informerSynced, sc.pcInformer.Informer().HasSynced)
			}
			return informerSynced
		}()...,
	)
}

// findJobAndTask returns job and the task info
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

// Evict will evict the pod
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
	if err := node.UpdateTask(task); err != nil {
		return err
	}

	p := task.Pod

	go func() {
		err := sc.Evictor.Evict(p)
		if err != nil {
			sc.resyncTask(task)
		}
	}()

	if !shadowPodGroup(job.PodGroup) {
		sc.Recorder.Eventf(job.PodGroup, v1.EventTypeNormal, "Evict", reason)
	}

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
	if err := node.AddTask(task); err != nil {
		return err
	}

	p := task.Pod

	go func() {
		if err := sc.Binder.Bind(p, hostname); err != nil {
			sc.resyncTask(task)
		} else {
			sc.Recorder.Eventf(p, v1.EventTypeNormal, "Scheduled", "Successfully assigned %v/%v to %v", p.Namespace, p.Name, hostname)
		}
	}()

	return nil
}

// AllocateVolumes allocates volume on the host to the task
func (sc *SchedulerCache) AllocateVolumes(task *api.TaskInfo, hostname string) error {
	return sc.VolumeBinder.AllocateVolumes(task, hostname)
}

// BindVolumes binds volumes to the task
func (sc *SchedulerCache) BindVolumes(task *api.TaskInfo) error {
	return sc.VolumeBinder.BindVolumes(task)
}

// taskUnschedulable updates pod status of pending task
func (sc *SchedulerCache) taskUnschedulable(task *api.TaskInfo, message string) error {
	pod := task.Pod

	condition := &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  v1.PodReasonUnschedulable,
		Message: message,
	}

	if podConditionHaveUpdate(&pod.Status, condition) {
		pod = pod.DeepCopy()

		// The reason field in 'Events' should be "FailedScheduling", there is not constants defined for this in
		// k8s core, so using the same string here.
		// The reason field in PodCondition should be "Unschedulable"
		sc.Recorder.Eventf(pod, v1.EventTypeWarning, "FailedScheduling", message)
		if _, err := sc.StatusUpdater.UpdatePodCondition(pod, condition); err != nil {
			return err
		}
	} else {
		glog.V(4).Infof("task unscheduleable %s/%s, message: %s, skip by no condition update", pod.Namespace, pod.Name, message)
	}

	return nil
}

func (sc *SchedulerCache) deleteJob(job *kbapi.JobInfo) {
	glog.V(3).Infof("Try to delete Job <%v:%v/%v>", job.UID, job.Namespace, job.Name)

	sc.deletedJobs.AddRateLimited(job)
}

func (sc *SchedulerCache) processCleanupJob() {
	obj, shutdown := sc.deletedJobs.Get()
	if shutdown {
		return
	}

	defer sc.deletedJobs.Done(obj)

	job, found := obj.(*kbapi.JobInfo)
	if !found {
		glog.Errorf("Failed to convert <%v> to *JobInfo", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if kbapi.JobTerminated(job) {
		delete(sc.Jobs, job.UID)
		glog.V(3).Infof("Job <%v:%v/%v> was deleted.", job.UID, job.Namespace, job.Name)
	} else {
		// Retry
		sc.deleteJob(job)
	}
}

func (sc *SchedulerCache) resyncTask(task *kbapi.TaskInfo) {
	sc.errTasks.AddRateLimited(task)
}

func (sc *SchedulerCache) processResyncTask() {
	obj, shutdown := sc.errTasks.Get()
	if shutdown {
		return
	}

	defer sc.errTasks.Done(obj)

	task, ok := obj.(*kbapi.TaskInfo)
	if !ok {
		glog.Errorf("failed to convert %v to *v1.Pod", obj)
		return
	}

	if err := sc.syncTask(task); err != nil {
		glog.Errorf("Failed to sync pod <%v/%v>, retry it.", task.Namespace, task.Name)
		sc.resyncTask(task)
	}
}

// Snapshot returns the complete snapshot of the cluster from cache
func (sc *SchedulerCache) Snapshot() *kbapi.ClusterInfo {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &kbapi.ClusterInfo{
		Nodes:  make(map[string]*kbapi.NodeInfo),
		Jobs:   make(map[kbapi.JobID]*kbapi.JobInfo),
		Queues: make(map[kbapi.QueueID]*kbapi.QueueInfo),
	}

	for _, value := range sc.Nodes {
		if !value.Ready() {
			continue
		}

		snapshot.Nodes[value.Name] = value.Clone()
	}

	for _, value := range sc.Queues {
		snapshot.Queues[value.UID] = value.Clone()
	}

	var cloneJobLock sync.Mutex
	var wg sync.WaitGroup

	cloneJob := func(value *api.JobInfo) {
		if value.PodGroup != nil {
			value.Priority = sc.defaultPriority

			priName := value.PodGroup.Spec.PriorityClassName
			if priorityClass, found := sc.PriorityClasses[priName]; found {
				value.Priority = priorityClass.Value
			}

			glog.V(4).Infof("The priority of job <%s/%s> is <%s/%d>",
				value.Namespace, value.Name, priName, value.Priority)
		}

		clonedJob := value.Clone()

		cloneJobLock.Lock()
		snapshot.Jobs[value.UID] = clonedJob
		cloneJobLock.Unlock()
		wg.Done()
	}

	for _, value := range sc.Jobs {
		// If no scheduling spec, does not handle it.
		if value.PodGroup == nil && value.PDB == nil {
			glog.V(4).Infof("The scheduling spec of Job <%v:%s/%s> is nil, ignore it.",
				value.UID, value.Namespace, value.Name)

			continue
		}

		if _, found := snapshot.Queues[value.Queue]; !found {
			glog.V(3).Infof("The Queue <%v> of Job <%v/%v> does not exist, ignore it.",
				value.Queue, value.Namespace, value.Name)
			continue
		}

		wg.Add(1)
		go cloneJob(value)
	}
	wg.Wait()

	glog.V(3).Infof("There are <%d> Jobs, <%d> Queues and <%d> Nodes in total for scheduling.",
		len(snapshot.Jobs), len(snapshot.Queues), len(snapshot.Nodes))

	return snapshot
}

// String returns information about the cache in a string format
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
			str = str + fmt.Sprintf("\t %s\n", job)
		}
	}

	return str
}

// RecordJobStatusEvent records related events according to job status.
func (sc *SchedulerCache) RecordJobStatusEvent(job *kbapi.JobInfo) {
	baseErrorMessage := job.JobFitErrors
	if baseErrorMessage == "" {
		baseErrorMessage = kbapi.AllNodeUnavailableMsg
	}

	if !shadowPodGroup(job.PodGroup) {
		pgUnschedulable := job.PodGroup != nil &&
			(job.PodGroup.Status.Phase == v1alpha1.PodGroupUnknown ||
				job.PodGroup.Status.Phase == v1alpha1.PodGroupPending)
		pdbUnschedulabe := job.PDB != nil && len(job.TaskStatusIndex[api.Pending]) != 0

		// If pending or unschedulable, record unschedulable event.
		if pgUnschedulable || pdbUnschedulabe {
			sc.Recorder.Eventf(job.PodGroup, v1.EventTypeWarning,
				string(v1alpha1.PodGroupUnschedulableType), baseErrorMessage)
		}
	}

	// Update podCondition for tasks Allocated and Pending before job discarded
	for _, status := range []api.TaskStatus{api.Allocated, api.Pending} {
		for _, taskInfo := range job.TaskStatusIndex[status] {
			msg := baseErrorMessage
			fitError := job.NodesFitErrors[taskInfo.UID]
			if fitError != nil {
				msg = fitError.Error()
			}
			if err := sc.taskUnschedulable(taskInfo, msg); err != nil {
				glog.Errorf("Failed to update unschedulable task status <%s/%s>: %v",
					taskInfo.Namespace, taskInfo.Name, err)
			}
		}
	}
}

// UpdateJobStatus update the status of job and its tasks.
func (sc *SchedulerCache) UpdateJobStatus(job *kbapi.JobInfo, updatePG bool) (*kbapi.JobInfo, error) {
	if updatePG && !shadowPodGroup(job.PodGroup) {
		pg, err := sc.StatusUpdater.UpdatePodGroup(job.PodGroup)
		if err != nil {
			return nil, err
		}
		job.PodGroup = pg
	}

	sc.RecordJobStatusEvent(job)

	return job, nil
}
