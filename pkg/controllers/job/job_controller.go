/*
Copyright 2017 The Volcano Authors.

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

package job

import (
	"context"
	"fmt"
	"hash"
	"hash/fnv"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	kubeschedulinginformers "k8s.io/client-go/informers/scheduling/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	kubeschedulinglisters "k8s.io/client-go/listers/scheduling/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcscheme "volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	batchinformer "volcano.sh/apis/pkg/client/informers/externalversions/batch/v1alpha1"
	businformer "volcano.sh/apis/pkg/client/informers/externalversions/bus/v1alpha1"
	schedulinginformers "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	batchlister "volcano.sh/apis/pkg/client/listers/batch/v1alpha1"
	buslister "volcano.sh/apis/pkg/client/listers/bus/v1alpha1"
	schedulinglisters "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/controllers/apis"
	jobcache "volcano.sh/volcano/pkg/controllers/cache"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/controllers/job/state"
	"volcano.sh/volcano/pkg/features"
)

func init() {
	framework.RegisterController(&jobcontroller{})
}

type delayAction struct {
	// The namespacing name of the job
	jobKey string

	// The name of the task
	taskName string

	// The name of the pod
	podName string

	// The event caused the action
	event busv1alpha1.Event

	// The action to take.
	action busv1alpha1.Action

	// The delay before the action is executed
	delay time.Duration

	// The cancel function of the action
	cancel context.CancelFunc
}

// jobcontroller the Job jobcontroller type.
type jobcontroller struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	jobInformer   batchinformer.JobInformer
	podInformer   coreinformers.PodInformer
	pvcInformer   coreinformers.PersistentVolumeClaimInformer
	pgInformer    schedulinginformers.PodGroupInformer
	svcInformer   coreinformers.ServiceInformer
	cmdInformer   businformer.CommandInformer
	pcInformer    kubeschedulinginformers.PriorityClassInformer
	queueInformer schedulinginformers.QueueInformer

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	// A store of jobs
	jobLister batchlister.JobLister
	jobSynced func() bool

	// A store of pods
	podLister corelisters.PodLister
	podSynced func() bool

	pvcLister corelisters.PersistentVolumeClaimLister
	pvcSynced func() bool

	// A store of podgroups
	pgLister schedulinglisters.PodGroupLister
	pgSynced func() bool

	// A store of service
	svcLister corelisters.ServiceLister
	svcSynced func() bool

	cmdLister buslister.CommandLister
	cmdSynced func() bool

	pcLister kubeschedulinglisters.PriorityClassLister
	pcSynced func() bool

	queueLister schedulinglisters.QueueLister
	queueSynced func() bool

	// queue that need to sync up
	queueList    []workqueue.TypedRateLimitingInterface[any]
	commandQueue workqueue.TypedRateLimitingInterface[any]
	cache        jobcache.Cache
	// Job Event recorder
	recorder record.EventRecorder

	errTasks      workqueue.TypedRateLimitingInterface[any]
	workers       uint32
	maxRequeueNum int

	delayActionMapLock sync.RWMutex
	// delayActionMap stores delayed actions for jobs, where outer map key is job key (namespace/name),
	// inner map key is pod name, and value is the delayed action to be performed
	delayActionMap map[string]map[string]*delayAction
}

func (cc *jobcontroller) Name() string {
	return "job-controller"
}

// Initialize creates the new Job controller.
func (cc *jobcontroller) Initialize(opt *framework.ControllerOption) error {
	cc.kubeClient = opt.KubeClient
	cc.vcClient = opt.VolcanoClient

	sharedInformers := opt.SharedInformerFactory
	workers := opt.WorkerNum
	// Initialize event client
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: cc.kubeClient.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(vcscheme.Scheme, v1.EventSource{Component: "vc-controller-manager"})

	cc.informerFactory = sharedInformers
	cc.queueList = make([]workqueue.TypedRateLimitingInterface[any], workers)
	cc.commandQueue = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	cc.cache = jobcache.New()
	cc.errTasks = newRateLimitingQueue()
	cc.recorder = recorder
	cc.workers = workers
	cc.maxRequeueNum = opt.MaxRequeueNum
	if cc.maxRequeueNum < 0 {
		cc.maxRequeueNum = -1
	}

	var i uint32
	for i = 0; i < workers; i++ {
		cc.queueList[i] = workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())
	}

	factory := opt.VCSharedInformerFactory
	cc.vcInformerFactory = factory
	if utilfeature.DefaultFeatureGate.Enabled(features.WorkLoadSupport) {
		cc.jobInformer = factory.Batch().V1alpha1().Jobs()
		cc.jobInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    cc.addJob,
			UpdateFunc: cc.updateJob,
			DeleteFunc: cc.deleteJob,
		})
		cc.jobLister = cc.jobInformer.Lister()
		cc.jobSynced = cc.jobInformer.Informer().HasSynced
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.QueueCommandSync) {
		cc.cmdInformer = factory.Bus().V1alpha1().Commands()
		cc.cmdInformer.Informer().AddEventHandler(
			cache.FilteringResourceEventHandler{
				FilterFunc: func(obj interface{}) bool {
					switch v := obj.(type) {
					case *busv1alpha1.Command:
						if v.TargetObject != nil &&
							v.TargetObject.APIVersion == batchv1alpha1.SchemeGroupVersion.String() &&
							v.TargetObject.Kind == "Job" {
							return true
						}

						return false
					default:
						return false
					}
				},
				Handler: cache.ResourceEventHandlerFuncs{
					AddFunc: cc.addCommand,
				},
			},
		)
		cc.cmdLister = cc.cmdInformer.Lister()
		cc.cmdSynced = cc.cmdInformer.Informer().HasSynced
	}

	cc.podInformer = sharedInformers.Core().V1().Pods()
	cc.podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    cc.addPod,
		UpdateFunc: cc.updatePod,
		DeleteFunc: cc.deletePod,
	})

	cc.podLister = cc.podInformer.Lister()
	cc.podSynced = cc.podInformer.Informer().HasSynced

	cc.pvcInformer = sharedInformers.Core().V1().PersistentVolumeClaims()
	cc.pvcLister = cc.pvcInformer.Lister()
	cc.pvcSynced = cc.pvcInformer.Informer().HasSynced

	cc.svcInformer = sharedInformers.Core().V1().Services()
	cc.svcLister = cc.svcInformer.Lister()
	cc.svcSynced = cc.svcInformer.Informer().HasSynced

	cc.pgInformer = factory.Scheduling().V1beta1().PodGroups()
	cc.pgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: cc.updatePodGroup,
	})
	cc.pgLister = cc.pgInformer.Lister()
	cc.pgSynced = cc.pgInformer.Informer().HasSynced

	if utilfeature.DefaultFeatureGate.Enabled(features.PriorityClass) {
		cc.pcInformer = sharedInformers.Scheduling().V1().PriorityClasses()
		cc.pcLister = cc.pcInformer.Lister()
		cc.pcSynced = cc.pcInformer.Informer().HasSynced
	}

	cc.queueInformer = factory.Scheduling().V1beta1().Queues()
	cc.queueLister = cc.queueInformer.Lister()
	cc.queueSynced = cc.queueInformer.Informer().HasSynced

	cc.delayActionMap = make(map[string]map[string]*delayAction)

	// Register actions
	state.SyncJob = cc.syncJob
	state.KillJob = cc.killJob
	state.KillTarget = cc.killTarget
	return nil
}

// Run start JobController.
func (cc *jobcontroller) Run(stopCh <-chan struct{}) {
	cc.informerFactory.Start(stopCh)
	cc.vcInformerFactory.Start(stopCh)

	for informerType, ok := range cc.informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	for informerType, ok := range cc.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	go wait.Until(cc.handleCommands, 0, stopCh)
	var i uint32
	for i = 0; i < cc.workers; i++ {
		go func(num uint32) {
			wait.Until(
				func() {
					cc.worker(num)
				},
				time.Second,
				stopCh)
		}(i)
	}

	go cc.cache.Run(stopCh)

	// Re-sync error tasks.
	go wait.Until(cc.processResyncTask, 0, stopCh)

	klog.Infof("JobController is running ...... ")
}

func (cc *jobcontroller) worker(i uint32) {
	klog.Infof("worker %d start ...... ", i)

	for cc.processNextReq(i) {
	}
}

func (cc *jobcontroller) belongsToThisRoutine(key string, count uint32) bool {
	var hashVal hash.Hash32
	var val uint32

	hashVal = fnv.New32()
	hashVal.Write([]byte(key))

	val = hashVal.Sum32()

	return val%cc.workers == count
}

func (cc *jobcontroller) getWorkerQueue(key string) workqueue.RateLimitingInterface {
	var hashVal hash.Hash32
	var val uint32

	hashVal = fnv.New32()
	hashVal.Write([]byte(key))

	val = hashVal.Sum32()

	queue := cc.queueList[val%cc.workers]

	return queue
}

func (cc *jobcontroller) processNextReq(count uint32) bool {
	queue := cc.queueList[count]
	obj, shutdown := queue.Get()
	if shutdown {
		klog.Errorf("Fail to pop item from queue")
		return false
	}

	req := obj.(apis.Request)
	defer queue.Done(req)

	key := jobcache.JobKeyByReq(&req)
	if !cc.belongsToThisRoutine(key, count) {
		klog.Errorf("should not occur The job does not belongs to this routine key:%s, worker:%d...... ", key, count)
		queueLocal := cc.getWorkerQueue(key)
		queueLocal.Add(req)
		return true
	}

	klog.V(3).Infof("Try to handle request <%v>", req)

	cc.CleanPodDelayActionsIfNeed(req)

	jobInfo, err := cc.cache.Get(key)
	if err != nil {
		// TODO(k82cn): ignore not-ready error.
		klog.Errorf("Failed to get job by <%v> from cache: %v", req, err)
		return true
	}

	st := state.NewState(jobInfo)
	if st == nil {
		klog.Errorf("Invalid state <%s> of Job <%v/%v>",
			jobInfo.Job.Status.State, jobInfo.Job.Namespace, jobInfo.Job.Name)
		return true
	}

	delayAct := applyPolicies(jobInfo.Job, &req)

	if delayAct.delay != 0 {
		klog.V(3).Infof("Execute <%v> on Job <%s/%s> after %s",
			delayAct.action, req.Namespace, req.JobName, delayAct.delay.String())
		cc.recordJobEvent(jobInfo.Job.Namespace, jobInfo.Job.Name, batchv1alpha1.ExecuteAction, fmt.Sprintf(
			"Execute action %s after %s", delayAct.action, delayAct.delay.String()))
		cc.AddDelayActionForJob(req, delayAct)
		return true
	}

	klog.V(3).Infof("Execute <%v> on Job <%s/%s> in <%s> by <%T>.",
		delayAct.action, req.Namespace, req.JobName, jobInfo.Job.Status.State.Phase, st)

	if delayAct.action != busv1alpha1.SyncJobAction {
		cc.recordJobEvent(jobInfo.Job.Namespace, jobInfo.Job.Name, batchv1alpha1.ExecuteAction, fmt.Sprintf(
			"Start to execute action %s ", delayAct.action))
	}

	action := GetStateAction(delayAct)

	if err := st.Execute(action); err != nil {
		cc.handleJobError(queue, req, st, err, delayAct.action)
	}

	// If no error, forget it.
	queue.Forget(req)

	// If the action is not an internal action, cancel all delayed actions
	if !isInternalAction(delayAct.action) {
		cc.cleanupDelayActions(delayAct.jobKey)
	}

	return true
}

// CleanPodDelayActionsIfNeed is used to clean delayed actions for Pod events when the pod phase changed:
// if the event is not PodPending event:
//   - cancel corresponding Pod Pending delayed action
//   - if the event is PodRunning state, cancel corresponding Pod Failed and Pod Evicted delayed actions
func (cc *jobcontroller) CleanPodDelayActionsIfNeed(req apis.Request) {
	if req.Event != busv1alpha1.PodPendingEvent {
		key := jobcache.JobKeyByReq(&req)
		cc.delayActionMapLock.Lock()
		defer cc.delayActionMapLock.Unlock()

		if taskMap, exists := cc.delayActionMap[key]; exists {
			if delayAct, exists := taskMap[req.PodName]; exists {
				shouldCancel := false

				if delayAct.event == busv1alpha1.PodPendingEvent {
					shouldCancel = true
				}

				if (delayAct.event == busv1alpha1.PodFailedEvent || delayAct.event == busv1alpha1.PodEvictedEvent) &&
					req.Event == busv1alpha1.PodRunningEvent {
					shouldCancel = true
				}

				if shouldCancel {
					klog.V(3).Infof("Cancel delayed action <%v> for pod <%s> of Job <%s>", delayAct.action, req.PodName, delayAct.jobKey)
					delayAct.cancel()
					delete(taskMap, req.PodName)
				}
			}
		}
	}
}

func (cc *jobcontroller) AddDelayActionForJob(req apis.Request, delayAct *delayAction) {
	cc.delayActionMapLock.Lock()
	defer cc.delayActionMapLock.Unlock()

	m, ok := cc.delayActionMap[delayAct.jobKey]
	if !ok {
		m = make(map[string]*delayAction)
		cc.delayActionMap[delayAct.jobKey] = m
	}
	if oldDelayAct, exists := m[req.PodName]; exists && oldDelayAct.action == delayAct.action {
		return
	}
	m[req.PodName] = delayAct

	ctx, cancel := context.WithTimeout(context.Background(), delayAct.delay)
	delayAct.cancel = cancel

	go func() {
		<-ctx.Done()
		if ctx.Err() == context.Canceled {
			klog.V(4).Infof("Job<%s/%s>'s delayed action %s is canceled", req.Namespace, req.JobName, delayAct.action)
			return
		}

		klog.V(4).Infof("Job<%s/%s>'s delayed action %s is expired, execute it", req.Namespace, req.JobName, delayAct.action)

		jobInfo, err := cc.cache.Get(delayAct.jobKey)
		if err != nil {
			klog.Errorf("Failed to get job by <%v> from cache: %v", req, err)
			return
		}

		st := state.NewState(jobInfo)
		if st == nil {
			klog.Errorf("Invalid state <%s> of Job <%v/%v>",
				jobInfo.Job.Status.State, jobInfo.Job.Namespace, jobInfo.Job.Name)
			return
		}
		queue := cc.getWorkerQueue(delayAct.jobKey)

		if err := st.Execute(GetStateAction(delayAct)); err != nil {
			cc.handleJobError(queue, req, st, err, delayAct.action)
		}

		queue.Forget(req)

		cc.cleanupDelayActions(delayAct.jobKey)
	}()
}

func (cc *jobcontroller) handleJobError(queue workqueue.TypedRateLimitingInterface[any], req apis.Request, st state.State, err error, action busv1alpha1.Action) {
	if cc.maxRequeueNum == -1 || queue.NumRequeues(req) < cc.maxRequeueNum {
		klog.V(2).Infof("Failed to handle Job <%s/%s>: %v",
			req.Namespace, req.JobName, err)
		queue.AddRateLimited(req)
		return
	}

	cc.recordJobEvent(req.Namespace, req.JobName, batchv1alpha1.ExecuteAction,
		fmt.Sprintf("Job failed on action %s for retry limit reached", action))
	klog.Warningf("Terminating Job <%s/%s> and releasing resources", req.Namespace, req.JobName)

	if err = st.Execute(state.Action{Action: busv1alpha1.TerminateJobAction}); err != nil {
		klog.Errorf("Failed to terminate Job<%s/%s>: %v", req.Namespace, req.JobName, err)
	}
	klog.Warningf("Dropping job<%s/%s> out of the queue: %v because max retries has reached",
		req.Namespace, req.JobName, err)
}

func (cc *jobcontroller) cleanupDelayActions(jobKey string) {
	cc.delayActionMapLock.Lock()
	defer cc.delayActionMapLock.Unlock()

	if m, exists := cc.delayActionMap[jobKey]; exists {
		for _, delayAct := range m {
			if delayAct.cancel != nil {
				delayAct.cancel()
			}
		}
		cc.delayActionMap[jobKey] = make(map[string]*delayAction)
	}
}
