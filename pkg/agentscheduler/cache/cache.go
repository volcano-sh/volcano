/*
 Copyright 2021 The Volcano Authors.

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
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	infov1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	resourceslicetracker "k8s.io/dynamic-resource-allocation/resourceslice/tracker"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"

	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	vcache "volcano.sh/volcano/pkg/scheduler/cache"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	commonutil "volcano.sh/volcano/pkg/util"
	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"
)

func init() {
	schemeBuilder := runtime.SchemeBuilder{
		v1.AddToScheme,
	}

	utilruntime.Must(schemeBuilder.AddToScheme(scheme.Scheme))
}

var _ Cache = &SchedulerCache{}

// New returns a Cache implementation.
func New(config *rest.Config, schedulerNames []string, defaultQueue string, nodeSelectors []string, nodeWorkers uint32, ignoredProvisioners []string, resyncPeriod time.Duration) Cache {
	return newSchedulerCache(config, schedulerNames, defaultQueue, nodeSelectors, nodeWorkers, ignoredProvisioners, resyncPeriod)
}

// SchedulerCache cache for the kube batch
type SchedulerCache struct {
	sync.Mutex

	kubeClient kubernetes.Interface
	restConfig *rest.Config
	vcClient   vcclient.Interface
	// schedulerName is the name for volcano scheduler
	schedulerNames     []string
	nodeSelectorLabels map[string]sets.Empty
	metricsConf        map[string]string

	resyncPeriod time.Duration
	podInformer  infov1.PodInformer
	nodeInformer infov1.NodeInformer

	Binder        Binder
	StatusUpdater StatusUpdater

	Recorder record.EventRecorder

	Nodes    map[string]*schedulingapi.NodeInfo // TODO: do we need to also add a seperate lock for Nodes cache?
	NodeList []string

	taskCache *TaskCache

	errTasks  workqueue.TypedRateLimitingInterface[string]
	nodeQueue workqueue.TypedRateLimitingInterface[string]

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	BindFlowChannel chan *vcache.BindContext
	bindCache       []*vcache.BindContext
	batchNum        int

	// A map from image name to its imageState.
	imageStates map[string]*imageState

	nodeWorkers uint32

	binderRegistry *BinderRegistry

	// sharedDRAManager is used in DRA plugin, contains resourceClaimTracker, resourceSliceLister and deviceClassLister
	sharedDRAManager k8sframework.SharedDRAManager

	ConflictAwareBinder *ConflictAwareBinder

	// schedulingQueue is used to store pods waiting to be scheduled
	schedulingQueue k8sschedulingqueue.SchedulingQueue
}

// TaskCache encapsulates the task map with a seperate lock
type TaskCache struct {
	sync.RWMutex
	tasks map[schedulingapi.TaskID]*schedulingapi.TaskInfo
}

func NewTaskCache() *TaskCache {
	return &TaskCache{
		tasks: make(map[schedulingapi.TaskID]*schedulingapi.TaskInfo),
	}
}

func (tc *TaskCache) Get(id schedulingapi.TaskID) (*schedulingapi.TaskInfo, bool) {
	tc.RLock()
	defer tc.RUnlock()
	task, ok := tc.tasks[id]
	return task, ok
}

func (tc *TaskCache) Add(task *schedulingapi.TaskInfo) {
	tc.Lock()
	defer tc.Unlock()
	tc.tasks[task.UID] = task
}

func (tc *TaskCache) Update(task *schedulingapi.TaskInfo) {
	tc.Lock()
	defer tc.Unlock()
	tc.tasks[task.UID] = task
}

func (tc *TaskCache) Delete(id schedulingapi.TaskID) {
	tc.Lock()
	defer tc.Unlock()
	delete(tc.tasks, id)
}

type imageState struct {
	// Size of the image
	size int64
	// A set of node names for nodes having this image present
	nodes sets.Set[string]
}

// DefaultBinder with kube client and event recorder
type DefaultBinder struct {
	kubeclient kubernetes.Interface
	recorder   record.EventRecorder
}

// Bind will send bind request to api server
func (db *DefaultBinder) Bind(kubeClient kubernetes.Interface, tasks []*schedulingapi.TaskInfo) map[schedulingapi.TaskID]string {
	errMsg := make(map[schedulingapi.TaskID]string)
	for _, task := range tasks {
		p := task.Pod
		if err := db.kubeclient.CoreV1().Pods(p.Namespace).Bind(context.TODO(),
			&v1.Binding{
				ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: p.UID, Annotations: p.Annotations},
				Target: v1.ObjectReference{
					Kind: "Node",
					Name: task.NodeName,
				},
			},
			metav1.CreateOptions{}); err != nil {
			klog.Errorf("Failed to bind pod <%v/%v> to node %s : %#v", p.Namespace, p.Name, task.NodeName, err)
			errMsg[task.UID] = err.Error()
		}
	}

	return errMsg
}

// NewDefaultBinder create binder with kube client and event recorder, support fake binder if passed fake client and fake event recorder
func NewDefaultBinder(kbclient kubernetes.Interface, record record.EventRecorder) *DefaultBinder {
	return &DefaultBinder{
		kubeclient: kbclient,
		recorder:   record,
	}
}

// defaultStatusUpdater is the default implementation of the StatusUpdater interface
type defaultStatusUpdater struct {
	kubeclient kubernetes.Interface
	vcclient   vcclient.Interface
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

// UpdatePodStatus will Update pod status
func (su *defaultStatusUpdater) UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error) {
	return su.kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
}

// updateNodeSelectors parse and update node selector key value pairs to schedule cache
func (sc *SchedulerCache) updateNodeSelectors(nodeSelectors []string) {
	for _, nodeSelectorLabel := range nodeSelectors {
		nodeSelectorLabelLen := len(nodeSelectorLabel)
		if nodeSelectorLabelLen <= 0 {
			continue
		}
		// check input
		index := strings.Index(nodeSelectorLabel, ":")
		if index < 0 || index >= (nodeSelectorLabelLen-1) {
			continue
		}
		nodeSelectorLabelName := strings.TrimSpace(nodeSelectorLabel[:index])
		nodeSelectorLabelValue := strings.TrimSpace(nodeSelectorLabel[index+1:])
		key := nodeSelectorLabelName + ":" + nodeSelectorLabelValue
		sc.nodeSelectorLabels[key] = sets.Empty{}
	}
}

// setBatchBindParallel configure the parallel when binding tasks to apiserver
func (sc *SchedulerCache) setBatchBindParallel() {
	sc.BindFlowChannel = make(chan *vcache.BindContext, 5000)
	var batchNum int
	batchNum, err := strconv.Atoi(os.Getenv("BATCH_BIND_NUM"))
	if err == nil && batchNum > 0 {
		sc.batchNum = batchNum
	} else {
		sc.batchNum = 1
	}
}

func newSchedulerCache(config *rest.Config, schedulerNames []string, defaultQueue string, nodeSelectors []string, nodeWorkers uint32, ignoredProvisioners []string, resyncPeriod time.Duration) *SchedulerCache {
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("failed init kubeClient, with err: %v", err))
	}
	vcClient, err := vcclient.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("failed init vcClient, with err: %v", err))
	}
	eventClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(fmt.Sprintf("failed init eventClient, with err: %v", err))
	}

	errTaskRateLimiter := workqueue.NewTypedMaxOfRateLimiter[string](
		workqueue.NewTypedItemExponentialFailureRateLimiter[string](5*time.Millisecond, 1000*time.Second),
		&workqueue.TypedBucketRateLimiter[string]{Limiter: rate.NewLimiter(rate.Limit(100), 1000)},
	)

	sc := &SchedulerCache{
		Nodes:              make(map[string]*schedulingapi.NodeInfo),
		errTasks:           workqueue.NewTypedRateLimitingQueue[string](errTaskRateLimiter),
		nodeQueue:          workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
		kubeClient:         kubeClient,
		vcClient:           vcClient,
		restConfig:         config,
		schedulerNames:     schedulerNames,
		nodeSelectorLabels: make(map[string]sets.Empty),
		imageStates:        make(map[string]*imageState),

		NodeList:    []string{},
		nodeWorkers: nodeWorkers,
		taskCache:   NewTaskCache(),
	}

	sc.resyncPeriod = resyncPeriod

	if len(nodeSelectors) > 0 {
		sc.updateNodeSelectors(nodeSelectors)
	}
	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: eventClient.CoreV1().Events("")})
	sc.Recorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: commonutil.GenerateComponentName(sc.schedulerNames)})

	// set concurrency configuration when binding
	sc.setBatchBindParallel()
	if bindMethodMap == nil {
		klog.V(3).Info("no registered bind method, new a default one")
		bindMethodMap = NewDefaultBinder(sc.kubeClient, sc.Recorder)
	}
	sc.Binder = GetBindMethod()

	sc.StatusUpdater = &defaultStatusUpdater{
		kubeclient: sc.kubeClient,
		vcclient:   sc.vcClient,
	}

	sc.binderRegistry = NewBinderRegistry()

	// add all events handlers
	sc.addEventHandler()

	sc.schedulingQueue = k8sschedulingqueue.NewSchedulingQueue(
		Less,
		sc.informerFactory,
		k8sschedulingqueue.WithClock(defaultSchedulerOptions.clock),
		k8sschedulingqueue.WithPodInitialBackoffDuration(time.Duration(defaultSchedulerOptions.podInitialBackoffSeconds)*time.Second),
		k8sschedulingqueue.WithPodMaxBackoffDuration(time.Duration(defaultSchedulerOptions.podMaxBackoffSeconds)*time.Second),
		k8sschedulingqueue.WithPodMaxInUnschedulablePodsDuration(defaultSchedulerOptions.podMaxInUnschedulablePodsDuration),
	)

	sc.ConflictAwareBinder = NewConflictAwareBinder(sc, sc.schedulingQueue)

	return sc
}

func (sc *SchedulerCache) addEventHandler() {
	informerFactory := informers.NewSharedInformerFactory(sc.kubeClient, sc.resyncPeriod)
	sc.informerFactory = informerFactory

	// explicitly register informers to the factory, otherwise resources listers cannot get anything
	// even with no error returned.
	// `Namespace` informer is used by `InterPodAffinity` plugin,
	// `SelectorSpread` and `PodTopologySpread` plugins uses the following four so far.
	informerFactory.Core().V1().Namespaces().Informer()
	informerFactory.Core().V1().Services().Informer()
	if utilfeature.DefaultFeatureGate.Enabled(features.WorkLoadSupport) {
		informerFactory.Core().V1().ReplicationControllers().Informer()
		informerFactory.Apps().V1().ReplicaSets().Informer()
		informerFactory.Apps().V1().StatefulSets().Informer()
	}

	// `PodDisruptionBudgets` informer is used by `Pdb` plugin
	if utilfeature.DefaultFeatureGate.Enabled(features.PodDisruptionBudgetsSupport) {
		informerFactory.Policy().V1().PodDisruptionBudgets().Informer()
	}

	// create informer for node information
	sc.nodeInformer = informerFactory.Core().V1().Nodes()
	sc.nodeInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Node:
					return true
				case cache.DeletedFinalStateUnknown:
					var ok bool
					_, ok = t.Obj.(*v1.Node)
					if !ok {
						klog.Errorf("Cannot convert to *v1.Node: %v", t.Obj)
						return false
					}
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddNode,
				UpdateFunc: sc.UpdateNode,
				DeleteFunc: sc.DeleteNode,
			},
		},
	)

	sc.podInformer = informerFactory.Core().V1().Pods()
	// 1. Pods already scheduled, refresh its state in cache
	sc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch v := obj.(type) {
				case *v1.Pod:
					if len(v.Spec.NodeName) != 0 {
						return true
					}
					return false
				case cache.DeletedFinalStateUnknown:
					if _, ok := v.Obj.(*v1.Pod); ok {
						// The carried object may be stale, always pass to clean up stale obj in event handlers.
						return true
					}
					klog.Errorf("Cannot convert object %T to *v1.Pod", v.Obj)
					return false
				default:
					klog.ErrorS(nil, "Unable to handle object", "objType", fmt.Sprintf("%T", obj), "obj", obj)
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddPodToCache,
				UpdateFunc: sc.UpdatePodInCache,
				DeleteFunc: sc.DeletePodFromCache,
			},
		})

	// 2. Pods not scheduled yet, and needed to be scheduled by agent scheduler, add them to scheduling queue
	sc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch v := obj.(type) {
				case *v1.Pod:
					// if the pod is not scheduled and scheduled by agent scheduler
					if len(v.Spec.NodeName) == 0 && slices.Contains(sc.schedulerNames, v.Spec.SchedulerName) {
						return true
					}
					return false
				case cache.DeletedFinalStateUnknown:
					if _, ok := v.Obj.(*v1.Pod); ok {
						// The carried object may be stale, always pass to clean up stale obj in event handlers.
						return true
					}
					klog.Errorf("Cannot convert object %T to *v1.Pod", v.Obj)
					return false
				default:
					klog.ErrorS(nil, "Unable to handle object", "objType", fmt.Sprintf("%T", obj), "obj", obj)
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddPodToSchedulingQueue,
				UpdateFunc: sc.UpdatePodInSchedulingQueue,
				DeleteFunc: sc.DeletePodFromSchedulingQueue,
			},
		})

	vcinformers := vcinformer.NewSharedInformerFactory(sc.vcClient, sc.resyncPeriod)
	sc.vcInformerFactory = vcinformers

	if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DynamicResourceAllocation) {
		ctx := context.TODO()
		logger := klog.FromContext(ctx)
		resourceClaimInformer := informerFactory.Resource().V1().ResourceClaims().Informer()
		resourceClaimCache := assumecache.NewAssumeCache(logger, resourceClaimInformer, "ResourceClaim", "", nil)
		resourceSliceTrackerOpts := resourceslicetracker.Options{
			EnableDeviceTaints: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRADeviceTaints),
			SliceInformer:      informerFactory.Resource().V1().ResourceSlices(),
			KubeClient:         sc.kubeClient,
		}
		// If device taints are disabled, the additional informers are not needed and
		// the tracker turns into a simple wrapper around the slice informer.
		if resourceSliceTrackerOpts.EnableDeviceTaints {
			resourceSliceTrackerOpts.TaintInformer = informerFactory.Resource().V1alpha3().DeviceTaintRules()
			resourceSliceTrackerOpts.ClassInformer = informerFactory.Resource().V1().DeviceClasses()
		}
		resourceSliceTracker, err := resourceslicetracker.StartTracker(ctx, resourceSliceTrackerOpts)
		if err != nil {
			klog.V(3).Infof("couldn't start resource slice tracker: %v", err)
		}
		sc.sharedDRAManager = dynamicresources.NewDRAManager(ctx, resourceClaimCache, resourceSliceTracker, informerFactory)
	}
}

// Run  starts the schedulerCache
func (sc *SchedulerCache) Run(stopCh <-chan struct{}) {
	sc.informerFactory.Start(stopCh)
	sc.vcInformerFactory.Start(stopCh)
	sc.WaitForCacheSync(stopCh)
	for i := 0; i < int(sc.nodeWorkers); i++ {
		go wait.Until(sc.runNodeWorker, 0, stopCh)
	}

	// Re-sync error tasks.
	go wait.Until(sc.processResyncTask, 0, stopCh)

	go wait.Until(sc.processBindTask, time.Millisecond*20, stopCh)

	sc.ConflictAwareBinder.Run(stopCh)
}

// WaitForCacheSync sync the cache with the api server
func (sc *SchedulerCache) WaitForCacheSync(stopCh <-chan struct{}) {
	sc.informerFactory.WaitForCacheSync(stopCh)
	sc.vcInformerFactory.WaitForCacheSync(stopCh)
}

// Bind binds task to the target host.
func (sc *SchedulerCache) Bind(ctx context.Context, bindContexts []*vcache.BindContext, preBinders map[string]PreBinder) {
	readyToBindTasks := make([]*schedulingapi.TaskInfo, len(bindContexts))
	for index := range readyToBindTasks {
		readyToBindTasks[index] = bindContexts[index].TaskInfo
	}
	tmp := time.Now()
	errMsg := sc.Binder.Bind(sc.kubeClient, readyToBindTasks)
	if len(errMsg) == 0 {
		klog.V(3).Infof("bind ok, latency %v", time.Since(tmp))
	} else {
		klog.V(3).Infof("There are %d tasks in total and %d binds failed, latency %v", len(readyToBindTasks), len(errMsg), time.Since(tmp))
	}

	for _, bindContext := range bindContexts {
		if reason, ok := errMsg[bindContext.TaskInfo.UID]; !ok {
			sc.Recorder.Eventf(bindContext.TaskInfo.Pod, v1.EventTypeNormal, "Scheduled", "Successfully assigned %v/%v to %v", bindContext.TaskInfo.Namespace, bindContext.TaskInfo.Name, bindContext.TaskInfo.NodeName)
		} else {
			unschedulableMsg := fmt.Sprintf("failed to bind to node %s: %s", bindContext.TaskInfo.NodeName, reason)
			if err := sc.TaskUnschedulable(bindContext.TaskInfo, schedulingapi.PodReasonSchedulerError, unschedulableMsg); err != nil {
				klog.ErrorS(err, "Failed to update pod status when bind task error", "task", bindContext.TaskInfo.Name)
			}

			for _, preBinder := range preBinders {
				if preBinder != nil {
					preBinder.PreBindRollBack(ctx, bindContext)
				}
			}

			klog.V(2).Infof("resyncTask task %s", bindContext.TaskInfo.Name)
			sc.resyncTask(bindContext.TaskInfo)
		}
	}
}

// Client returns the kubernetes clientSet
func (sc *SchedulerCache) Client() kubernetes.Interface {
	return sc.kubeClient
}

// VCClient returns the volcano clientSet
func (sc *SchedulerCache) VCClient() vcclient.Interface {
	return sc.vcClient
}

// ClientConfig returns the rest config
func (sc *SchedulerCache) ClientConfig() *rest.Config {
	return sc.restConfig
}

// SharedInformerFactory returns the scheduler SharedInformerFactory
func (sc *SchedulerCache) SharedInformerFactory() informers.SharedInformerFactory {
	return sc.informerFactory
}

// SchedulingQueue returns the scheduling queue instance in the cache
func (sc *SchedulerCache) SchedulingQueue() k8sschedulingqueue.SchedulingQueue {
	return sc.schedulingQueue
}

// SetSharedInformerFactory sets the scheduler SharedInformerFactory for unit test
func (sc *SchedulerCache) SetSharedInformerFactory(factory informers.SharedInformerFactory) {
	sc.informerFactory = factory
}

// UpdateSchedulerNumaInfo used to update scheduler node cache NumaSchedulerInfo
func (sc *SchedulerCache) UpdateSchedulerNumaInfo(AllocatedSets map[string]schedulingapi.ResNumaSets) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	for nodeName, sets := range AllocatedSets {
		if _, found := sc.Nodes[nodeName]; !found {
			continue
		}

		numaInfo := sc.Nodes[nodeName].NumaSchedulerInfo
		if numaInfo == nil {
			continue
		}

		numaInfo.Allocate(sets)
	}
	return nil
}

// EventRecorder returns the Event Recorder
func (sc *SchedulerCache) EventRecorder() record.EventRecorder {
	return sc.Recorder
}

// taskUnschedulable updates pod status of pending task
func (sc *SchedulerCache) TaskUnschedulable(task *schedulingapi.TaskInfo, reason, message string) error {
	pod := task.Pod

	condition := &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  reason, // Add more reasons in order to distinguish more specific scenario of pending tasks
		Message: message,
	}

	updateCond := podConditionHaveUpdate(&pod.Status, condition)

	if updateCond {
		pod = pod.DeepCopy()

		if updateCond && podutil.UpdatePodCondition(&pod.Status, condition) {
			klog.V(3).Infof("Updating pod condition for %s/%s to (%s==%s)", pod.Namespace, pod.Name, condition.Type, condition.Status)
		}

		// The reason field in 'Events' should be "FailedScheduling", there is not constants defined for this in
		// k8s core, so using the same string here.
		// The reason field in PodCondition can be "Unschedulable"
		sc.Recorder.Eventf(pod, v1.EventTypeWarning, "FailedScheduling", message)
		if _, err := sc.StatusUpdater.UpdatePodStatus(pod); err != nil {
			return err
		}
	} else {
		klog.V(4).Infof("task unscheduleable %s/%s, message: %s, skip by no condition update", pod.Namespace, pod.Name, message)
	}

	return nil
}

// GetTaskInfo retrieves a task by its ID.
func (sc *SchedulerCache) GetTaskInfo(taskID schedulingapi.TaskID) (*schedulingapi.TaskInfo, bool) {
	return sc.taskCache.Get(taskID)
}

// AddTaskInfo adds a new task in the cache
func (sc *SchedulerCache) AddTaskInfo(task *schedulingapi.TaskInfo) {
	sc.taskCache.Add(task)
}

// UpdateTaskInfo updates a task in the cache.
func (sc *SchedulerCache) UpdateTaskInfo(task *schedulingapi.TaskInfo) {
	sc.taskCache.Update(task)
}

// DeleteTaskInfo removes a task from the cache by its ID.
func (sc *SchedulerCache) DeleteTaskInfo(taskID schedulingapi.TaskID) {
	sc.taskCache.Delete(taskID)
}

func (sc *SchedulerCache) resyncTask(task *schedulingapi.TaskInfo) {
	sc.errTasks.AddRateLimited(string(task.UID))
}

func (sc *SchedulerCache) parseErrTaskKey(key string) (*schedulingapi.TaskInfo, error) {
	// TODO  need to refactoring
	return nil, nil
}

func (sc *SchedulerCache) processResyncTask() {
	taskKey, shutdown := sc.errTasks.Get()
	if shutdown {
		return
	}

	klog.V(5).Infof("the length of errTasks is %d", sc.errTasks.Len())

	defer sc.errTasks.Done(taskKey)

	task, err := sc.parseErrTaskKey(taskKey)
	if err != nil {
		klog.ErrorS(err, "Failed to get task for sync task", "taskKey", taskKey)
		sc.errTasks.Forget(taskKey)
		return
	}

	reSynced := false
	if err := sc.syncTask(task); err != nil {
		klog.ErrorS(err, "Failed to sync task, retry it", "namespace", task.Namespace, "name", task.Name)
		sc.resyncTask(task)
		reSynced = true
	} else {
		klog.V(4).Infof("Successfully synced task <%s/%s>", task.Namespace, task.Name)
		sc.errTasks.Forget(taskKey)
	}

	// execute custom bind err handler call back func if exists.
	if task.CustomBindErrHandler != nil && !task.CustomBindErrHandlerSucceeded {
		err := task.CustomBindErrHandler()
		if err != nil {
			klog.ErrorS(err, "Failed to execute custom bind err handler, retry it.")
		} else {
			task.CustomBindErrHandlerSucceeded = true
		}
		if !task.CustomBindErrHandlerSucceeded && !reSynced {
			sc.resyncTask(task)
		}
	}
}

func (sc *SchedulerCache) runNodeWorker() {
	for sc.processSyncNode() {
	}
}

func (sc *SchedulerCache) processSyncNode() bool {
	nodeName, shutdown := sc.nodeQueue.Get()
	if shutdown {
		return false
	}
	defer sc.nodeQueue.Done(nodeName)

	klog.V(5).Infof("started sync node %s", nodeName)
	err := sc.SyncNode(nodeName)
	if err == nil {
		sc.nodeQueue.Forget(nodeName)
		return true
	}

	klog.Errorf("Failed to sync node <%s>, retry it.", nodeName)
	sc.nodeQueue.AddRateLimited(nodeName)
	return true
}

// AddBindTask add task to be bind to a cache which consumes by go runtime
func (sc *SchedulerCache) AddBindTask(bindContext *vcache.BindContext) error {
	klog.V(5).Infof("add bind task %v/%v", bindContext.TaskInfo.Namespace, bindContext.TaskInfo.Name)
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	task := bindContext.TaskInfo

	node, found := sc.Nodes[task.NodeName]
	if !found {
		return fmt.Errorf("failed to bind Task %v to host %v, host does not exist",
			task.UID, task.NodeName)
	}

	originalStatus := task.Status
	if err := sc.UpdateTaskStatus(task, schedulingapi.Binding); err != nil {
		return err
	}

	err := bindContext.TaskInfo.SetPodResourceDecision()
	if err != nil {
		return fmt.Errorf("set task %v/%v resource decision failed, err %v", task.Namespace, task.Name, err)
	}
	task.NumaInfo = bindContext.TaskInfo.NumaInfo.Clone()

	// Add task to the node.
	if err := node.AddTask(task); err != nil {
		// After failing to update task to a node we need to revert task status from Releasing,
		// otherwise task might be stuck in the Releasing state indefinitely.
		if err := sc.UpdateTaskStatus(task, originalStatus); err != nil {
			klog.Errorf("Task <%s/%s> will be resynchronized after failing to revert status "+
				"from %s to %s after failing to update Task on Node <%s>: %v",
				task.Namespace, task.Name, task.Status, originalStatus, node.Name, err)
			sc.resyncTask(task)
		}
		return err
	}
	// bind generation after task is added to node, so next allocation on this node in newer generation must aware of this task
	node.NextBindGeneration()

	sc.BindFlowChannel <- bindContext

	return nil
}

func (sc *SchedulerCache) processBindTask() {
	for {
		select {
		case bindContext, ok := <-sc.BindFlowChannel:
			if !ok {
				return
			}

			sc.bindCache = append(sc.bindCache, bindContext)
			if len(sc.bindCache) == sc.batchNum {
				sc.BindTask()
			}
		default:
		}

		if len(sc.BindFlowChannel) == 0 {
			break
		}
	}

	if len(sc.bindCache) == 0 {
		return
	}
	sc.BindTask()
}

// executePreBind executes PreBind for one bindContext
func (sc *SchedulerCache) executePreBind(ctx context.Context, bindContext *vcache.BindContext, preBinders map[string]PreBinder) error {
	executedPreBinders := make([]PreBinder, 0, len(preBinders))
	for _, preBinder := range preBinders {
		if preBinder == nil {
			continue
		}

		if err := preBinder.PreBind(ctx, bindContext); err != nil {
			// If PreBind fails, rollback the executed PreBinders
			for i := len(executedPreBinders) - 1; i >= 0; i-- {
				if executedPreBinders[i] != nil {
					executedPreBinders[i].PreBindRollBack(ctx, bindContext)
				}
			}
			return err
		}
		executedPreBinders = append(executedPreBinders, preBinder)
	}

	return nil
}

// executePreBinds executes PreBind for a list of bindContexts
func (sc *SchedulerCache) executePreBinds(ctx context.Context, bindContexts []*vcache.BindContext, preBinders map[string]PreBinder) []*vcache.BindContext {
	logger := klog.FromContext(ctx)
	successfulBindContexts := make([]*vcache.BindContext, 0, len(bindContexts))

	for _, bindContext := range bindContexts {
		if err := sc.executePreBind(ctx, bindContext, preBinders); err != nil {
			reason := fmt.Sprintf("execute preBind for pod %s failed: %v, resync the task", klog.KObj(bindContext.TaskInfo.Pod), err)
			klog.Error(reason)
			sc.resyncTask(bindContext.TaskInfo)
			if updateErr := sc.TaskUnschedulable(bindContext.TaskInfo, schedulingapi.PodReasonSchedulerError, reason); updateErr != nil {
				logger.Error(updateErr, "Failed to update pod status", "pod", klog.KObj(bindContext.TaskInfo.Pod))
			}
			continue
		}
		successfulBindContexts = append(successfulBindContexts, bindContext)
	}

	return successfulBindContexts
}

// BindTask do k8s binding with a goroutine
func (sc *SchedulerCache) BindTask() {
	klog.V(5).Infof("batch bind task count %d", sc.batchNum)
	tmpBindCache := make([]*vcache.BindContext, len(sc.bindCache))
	copy(tmpBindCache, sc.bindCache)

	// Currently, bindContexts only contain 1 element.
	go func(bindContexts []*vcache.BindContext) {
		logger := klog.Background()
		ctx := klog.NewContext(context.Background(), logger)
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		preBinders := sc.binderRegistry.getRegisteredPreBinders()
		successfulPreBindContexts := sc.executePreBinds(cancelCtx, bindContexts, preBinders)
		sc.Bind(ctx, successfulPreBindContexts, preBinders)
	}(tmpBindCache)

	// The slice here needs to point to a new underlying array, otherwise bindCache may not be able to trigger garbage collection immediately
	// if it is not expanded, causing memory leaks.
	sc.bindCache = make([]*vcache.BindContext, 0)
}

// Snapshot returns the complete snapshot of the cluster from cache, used for dump purpose only
func (sc *SchedulerCache) Snapshot() *schedulingapi.ClusterInfo {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &schedulingapi.ClusterInfo{
		Nodes:        make(map[string]*schedulingapi.NodeInfo),
		RealNodesSet: make(map[string]sets.Set[string]),
		NodeList:     make([]string, len(sc.NodeList)),
	}

	// TODO add agent scheduler cache

	copy(snapshot.NodeList, sc.NodeList)
	for _, value := range sc.Nodes {
		value.RefreshNumaSchedulerInfoByCrd()
	}

	for _, value := range sc.Nodes {
		if !value.Ready() {
			continue
		}

		snapshot.Nodes[value.Name] = value.Clone()
	}
	klog.V(3).InfoS("SnapShot for scheduling", "NodeNum", len(snapshot.Nodes))
	return snapshot
}

func (sc *SchedulerCache) UpdateSnapshot(snapshot *k8sutil.Snapshot) error {
	//TODO: update the passed-in snapshot with the latest cache info
	return nil
}

func (sc *SchedulerCache) SharedDRAManager() k8sframework.SharedDRAManager {
	return sc.sharedDRAManager
}

// String returns information about the cache in a string format
func (sc *SchedulerCache) String() string {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	str := "Cache:\n"

	if len(sc.Nodes) != 0 {
		str += "Nodes:\n"
		for _, n := range sc.Nodes {
			str += fmt.Sprintf("\t %s: idle(%v) used(%v) allocatable(%v) pods(%d)\n",
				n.Name, n.Idle, n.Used, n.Allocatable, len(n.Tasks))

			i := 0
			for _, p := range n.Tasks {
				str += fmt.Sprintf("\t\t %d: %v\n", i, p)
				i++
			}
		}
	}

	if len(sc.NodeList) != 0 {
		str += fmt.Sprintf("NodeList: %v\n", sc.NodeList)
	}

	return str
}

func (sc *SchedulerCache) SetMetricsConf(conf map[string]string) {
	sc.metricsConf = conf
}

// createImageStateSummary returns a summarizing snapshot of the given image's state.
func (sc *SchedulerCache) createImageStateSummary(state *imageState) *fwk.ImageStateSummary {
	return &fwk.ImageStateSummary{
		Size:     state.size,
		NumNodes: len(state.nodes),
	}
}

func (sc *SchedulerCache) RegisterBinder(name string, binder interface{}) {
	if sc.binderRegistry == nil {
		sc.binderRegistry = NewBinderRegistry()
	}
	sc.binderRegistry.Register(name, binder)
}

// TODO: refer to UpdateTaskStatus
func (sc *SchedulerCache) UpdateTaskStatus(task *api.TaskInfo, status api.TaskStatus) error {
	task.Status = status
	return nil
}

func (sc *SchedulerCache) EnqueueScheduleResult(scheduleResult *PodScheduleResult) {
	sc.ConflictAwareBinder.EnqueueScheduleResult(scheduleResult)
}
