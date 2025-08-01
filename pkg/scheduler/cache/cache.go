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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	infov1 "k8s.io/client-go/informers/core/v1"
	schedv1 "k8s.io/client-go/informers/scheduling/v1"
	storagev1 "k8s.io/client-go/informers/storage/v1"
	storagev1beta1 "k8s.io/client-go/informers/storage/v1beta1"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	resourceslicetracker "k8s.io/dynamic-resource-allocation/resourceslice/tracker"
	"k8s.io/klog/v2"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	kubefeatures "k8s.io/kubernetes/pkg/features"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"
	"stathat.com/c/consistent"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingscheme "volcano.sh/apis/pkg/apis/scheduling/scheme"
	vcv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	cpuinformerv1 "volcano.sh/apis/pkg/client/informers/externalversions/nodeinfo/v1alpha1"
	vcinformerv1 "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/features"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/metrics/source"
	commonutil "volcano.sh/volcano/pkg/util"
)

const (
	// default interval for sync data from metrics server, the value is 30s
	defaultMetricsInternal = 30 * time.Second

	taskUpdaterWorker = 16
)

// defaultIgnoredProvisioners contains provisioners that will be ignored during pod pvc request computation and preemption.
var defaultIgnoredProvisioners = []string{"rancher.io/local-path", "hostpath.csi.k8s.io"}

func init() {
	schemeBuilder := runtime.SchemeBuilder{
		v1.AddToScheme,
	}

	utilruntime.Must(schemeBuilder.AddToScheme(scheme.Scheme))
}

// New returns a Cache implementation.
func New(config *rest.Config, schedulerNames []string, defaultQueue string, nodeSelectors []string, nodeWorkers uint32, ignoredProvisioners []string, resyncPeriod time.Duration) Cache {
	return newSchedulerCache(config, schedulerNames, defaultQueue, nodeSelectors, nodeWorkers, ignoredProvisioners, resyncPeriod)
}

// SchedulerCache cache for the kube batch
type SchedulerCache struct {
	sync.Mutex

	kubeClient   kubernetes.Interface
	restConfig   *rest.Config
	vcClient     vcclient.Interface
	defaultQueue string
	// schedulerName is the name for volcano scheduler
	schedulerNames     []string
	nodeSelectorLabels map[string]sets.Empty
	metricsConf        map[string]string

	resyncPeriod               time.Duration
	podInformer                infov1.PodInformer
	nodeInformer               infov1.NodeInformer
	hyperNodeInformer          topologyinformerv1alpha1.HyperNodeInformer
	podGroupInformerV1beta1    vcinformerv1.PodGroupInformer
	queueInformerV1beta1       vcinformerv1.QueueInformer
	pvInformer                 infov1.PersistentVolumeInformer
	pvcInformer                infov1.PersistentVolumeClaimInformer
	scInformer                 storagev1.StorageClassInformer
	pcInformer                 schedv1.PriorityClassInformer
	quotaInformer              infov1.ResourceQuotaInformer
	csiNodeInformer            storagev1.CSINodeInformer
	csiDriverInformer          storagev1.CSIDriverInformer
	csiStorageCapacityInformer storagev1beta1.CSIStorageCapacityInformer
	cpuInformer                cpuinformerv1.NumatopologyInformer

	Binder         Binder
	Evictor        Evictor
	StatusUpdater  StatusUpdater
	PodGroupBinder BatchBinder

	Recorder record.EventRecorder

	Jobs                 map[schedulingapi.JobID]*schedulingapi.JobInfo
	Nodes                map[string]*schedulingapi.NodeInfo
	Queues               map[schedulingapi.QueueID]*schedulingapi.QueueInfo
	PriorityClasses      map[string]*schedulingv1.PriorityClass
	NodeList             []string
	defaultPriorityClass *schedulingv1.PriorityClass
	defaultPriority      int32
	CSINodesStatus       map[string]*schedulingapi.CSINodeStatusInfo
	HyperNodesInfo       *schedulingapi.HyperNodesInfo

	NamespaceCollection map[string]*schedulingapi.NamespaceCollection

	errTasks        workqueue.TypedRateLimitingInterface[string]
	nodeQueue       workqueue.TypedRateLimitingInterface[string]
	DeletedJobs     workqueue.TypedRateLimitingInterface[*schedulingapi.JobInfo]
	hyperNodesQueue workqueue.TypedRateLimitingInterface[string]

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	BindFlowChannel chan *BindContext
	bindCache       []*BindContext
	batchNum        int

	// A map from image name to its imageState.
	imageStates map[string]*imageState

	nodeWorkers uint32

	// IgnoredCSIProvisioners contains a list of provisioners, and pod request pvc with these provisioners will
	// not be counted in pod pvc resource request and node.Allocatable, because the spec.drivers of csinode resource
	// is always null, these provisioners usually are host path csi controllers like rancher.io/local-path and hostpath.csi.k8s.io.
	IgnoredCSIProvisioners sets.Set[string]

	// multiSchedulerInfo holds multi schedulers info without using node selector, please see the following link for more details.
	// https://github.com/volcano-sh/volcano/blob/master/docs/design/deploy-multi-volcano-schedulers-without-using-selector.md
	multiSchedulerInfo

	binderRegistry *BinderRegistry

	// sharedDRAManager is used in DRA plugin, contains resourceClaimTracker, resourceSliceLister and deviceClassLister
	sharedDRAManager k8sframework.SharedDRAManager
}

type multiSchedulerInfo struct {
	schedulerPodName string
	c                *consistent.Consistent
}

type imageState struct {
	// Size of the image
	size int64
	// A set of node names for nodes having this image present
	nodes sets.Set[string]
}

type BindContextExtension interface{}

type BindContext struct {
	TaskInfo *schedulingapi.TaskInfo
	// Extensions stores extra bind context information of each plugin
	Extensions map[string]BindContextExtension
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
		} else {
			metrics.UpdateTaskScheduleDuration(metrics.Duration(p.CreationTimestamp.Time)) // update metrics as soon as pod is bind
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

type defaultEvictor struct {
	kubeclient kubernetes.Interface
	recorder   record.EventRecorder
}

// Evict will send delete pod request to api server
func (de *defaultEvictor) Evict(p *v1.Pod, reason string) error {
	klog.V(3).Infof("Evicting pod %v/%v, because of %v", p.Namespace, p.Name, reason)

	evictMsg := fmt.Sprintf("Pod is evicted, because of %v", reason)
	annotations := map[string]string{}
	// record that we are evicting the pod
	de.recorder.AnnotatedEventf(p, annotations, v1.EventTypeWarning, "Evict", evictMsg)

	pod := p.DeepCopy()
	condition := &v1.PodCondition{
		Type:    v1.PodReady,
		Status:  v1.ConditionFalse,
		Reason:  "Evict",
		Message: evictMsg,
	}
	if !podutil.UpdatePodCondition(&pod.Status, condition) {
		klog.V(1).Infof("UpdatePodCondition: existed condition, not update")
		klog.V(1).Infof("%+v", pod.Status.Conditions)
		return nil
	}

	condition = &v1.PodCondition{
		Type:    v1.DisruptionTarget,
		Status:  v1.ConditionTrue,
		Reason:  v1.PodReasonPreemptionByScheduler,
		Message: fmt.Sprintf("%s: preempting to accommodate a higher priority pod", pod.Spec.SchedulerName),
	}
	if !podutil.UpdatePodCondition(&pod.Status, condition) {
		klog.V(1).Infof("UpdatePodCondition: existed condition, not update")
		klog.V(1).Infof("%+v", pod.Status.Conditions)
		return nil
	}

	if _, err := de.kubeclient.CoreV1().Pods(p.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update pod <%v/%v> status: %v", pod.Namespace, pod.Name, err)
		return err
	}
	if err := de.kubeclient.CoreV1().Pods(p.Namespace).Delete(context.TODO(), p.Name, metav1.DeleteOptions{}); err != nil {
		klog.Errorf("Failed to evict pod <%v/%v>: %#v", p.Namespace, p.Name, err)
		return err
	}

	return nil
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

func podNominatedNodeNameNeedUpdate(status *v1.PodStatus, nodeName string) bool {
	return status.NominatedNodeName != nodeName
}

// UpdatePodStatus will Update pod status
func (su *defaultStatusUpdater) UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error) {
	return su.kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
}

// UpdatePodGroup will Update PodGroup
func (su *defaultStatusUpdater) UpdatePodGroup(pg *schedulingapi.PodGroup) (*schedulingapi.PodGroup, error) {
	podgroup := &vcv1beta1.PodGroup{}
	if err := schedulingscheme.Scheme.Convert(&pg.PodGroup, podgroup, nil); err != nil {
		klog.Errorf("Error while converting PodGroup to v1alpha1.PodGroup with error: %v", err)
		return nil, err
	}

	updated, err := su.vcclient.SchedulingV1beta1().PodGroups(podgroup.Namespace).Update(context.TODO(), podgroup, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Error while updating PodGroup with error: %v", err)
		return nil, err
	}

	podGroupInfo := &schedulingapi.PodGroup{Version: schedulingapi.PodGroupVersionV1Beta1}
	if err := schedulingscheme.Scheme.Convert(updated, &podGroupInfo.PodGroup, nil); err != nil {
		klog.Errorf("Error while converting v1alpha.PodGroup to api.PodGroup with error: %v", err)
		return nil, err
	}

	return podGroupInfo, nil
}

// UpdateQueueStatus will update the status of queue
func (su *defaultStatusUpdater) UpdateQueueStatus(queue *schedulingapi.QueueInfo) error {
	newQueue := &vcv1beta1.Queue{}
	if err := schedulingscheme.Scheme.Convert(queue.Queue, newQueue, nil); err != nil {
		klog.Errorf("error occurred in converting scheduling.Queue to v1beta1.Queue: %s", err.Error())
		return err
	}

	_, err := su.vcclient.SchedulingV1beta1().Queues().UpdateStatus(context.TODO(), newQueue, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("error occurred in updating Queue <%s>: %s", newQueue.Name, err.Error())
		return err
	}
	return nil
}

type podgroupBinder struct {
	kubeclient kubernetes.Interface
	vcclient   vcclient.Interface
}

// Bind will add silo cluster annotation on pod and podgroup
func (pgb *podgroupBinder) Bind(job *schedulingapi.JobInfo, cluster string) (*schedulingapi.JobInfo, error) {
	if len(job.Tasks) == 0 {
		klog.V(4).Infof("Job pods have not been created yet")
		return job, nil
	}
	for _, task := range job.Tasks {
		pod := task.Pod
		pod.Annotations[batch.ForwardClusterKey] = cluster
		pod.ResourceVersion = ""
		_, err := pgb.kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("Error while update pod annotation with error: %v", err)
			return nil, err
		}
	}

	pg := job.PodGroup
	pg.Annotations[batch.ForwardClusterKey] = cluster
	podgroup := &vcv1beta1.PodGroup{}
	if err := schedulingscheme.Scheme.Convert(&pg.PodGroup, podgroup, nil); err != nil {
		klog.Errorf("Error while converting PodGroup to v1alpha1.PodGroup with error: %v", err)
		return nil, err
	}
	newPg, err := pgb.vcclient.SchedulingV1beta1().PodGroups(pg.Namespace).Update(context.TODO(), podgroup, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Error while update PodGroup annotation with error: %v", err)
		return nil, err
	}
	job.PodGroup.ResourceVersion = newPg.ResourceVersion
	klog.V(4).Infof("Bind PodGroup <%s> successfully", job.PodGroup.Name)
	return job, nil
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
	sc.BindFlowChannel = make(chan *BindContext, 5000)
	var batchNum int
	batchNum, err := strconv.Atoi(os.Getenv("BATCH_BIND_NUM"))
	if err == nil && batchNum > 0 {
		sc.batchNum = batchNum
	} else {
		sc.batchNum = 1
	}
}

// newDefaultAndRootQueue init default queue and root queue
func newDefaultAndRootQueue(vcClient vcclient.Interface, defaultQueue string) {
	reclaimable := false
	rootQueue := vcv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "root",
		},
		Spec: vcv1beta1.QueueSpec{
			Reclaimable: &reclaimable,
			Weight:      1,
		},
	}

	err := retry.OnError(wait.Backoff{
		Steps:    60,
		Duration: time.Second,
		Factor:   1,
		Jitter:   0.1,
	}, func(err error) bool {
		return !apierrors.IsAlreadyExists(err)
	}, func() error {
		_, err := vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), &rootQueue, metav1.CreateOptions{})
		return err
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		panic(fmt.Errorf("failed init root queue, with err: %v", err))
	}

	reclaimable = true
	defaultQue := vcv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultQueue,
		},
		Spec: vcv1beta1.QueueSpec{
			Reclaimable: &reclaimable,
			Weight:      1,
		},
	}

	err = retry.OnError(wait.Backoff{
		Steps:    60,
		Duration: time.Second,
		Factor:   1,
		Jitter:   0.1,
	}, func(err error) bool {
		return !apierrors.IsAlreadyExists(err)
	}, func() error {
		_, err := vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), &defaultQue, metav1.CreateOptions{})
		return err
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		panic(fmt.Errorf("failed init default queue, with err: %v", err))
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

	// create default queue and root queue
	klog.Infof("Creating default queue and root queue")
	newDefaultAndRootQueue(vcClient, defaultQueue)

	errTaskRateLimiter := workqueue.NewTypedMaxOfRateLimiter[string](
		workqueue.NewTypedItemExponentialFailureRateLimiter[string](5*time.Millisecond, 1000*time.Second),
		&workqueue.TypedBucketRateLimiter[string]{Limiter: rate.NewLimiter(rate.Limit(100), 1000)},
	)

	sc := &SchedulerCache{
		Jobs:                make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		Nodes:               make(map[string]*schedulingapi.NodeInfo),
		Queues:              make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
		PriorityClasses:     make(map[string]*schedulingv1.PriorityClass),
		errTasks:            workqueue.NewTypedRateLimitingQueue[string](errTaskRateLimiter),
		nodeQueue:           workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
		DeletedJobs:         workqueue.NewTypedRateLimitingQueue[*schedulingapi.JobInfo](workqueue.DefaultTypedControllerRateLimiter[*schedulingapi.JobInfo]()),
		hyperNodesQueue:     workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
		kubeClient:          kubeClient,
		vcClient:            vcClient,
		restConfig:          config,
		defaultQueue:        defaultQueue,
		schedulerNames:      schedulerNames,
		nodeSelectorLabels:  make(map[string]sets.Empty),
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),
		CSINodesStatus:      make(map[string]*schedulingapi.CSINodeStatusInfo),
		imageStates:         make(map[string]*imageState),

		NodeList:    []string{},
		nodeWorkers: nodeWorkers,
	}

	sc.resyncPeriod = resyncPeriod
	sc.schedulerPodName, sc.c = getMultiSchedulerInfo()
	ignoredProvisionersSet := sets.New[string]()
	for _, provisioner := range append(ignoredProvisioners, defaultIgnoredProvisioners...) {
		ignoredProvisionersSet.Insert(provisioner)
	}
	sc.IgnoredCSIProvisioners = ignoredProvisionersSet

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

	sc.Evictor = &defaultEvictor{
		kubeclient: sc.kubeClient,
		recorder:   sc.Recorder,
	}

	sc.StatusUpdater = &defaultStatusUpdater{
		kubeclient: sc.kubeClient,
		vcclient:   sc.vcClient,
	}

	sc.PodGroupBinder = &podgroupBinder{
		kubeclient: sc.kubeClient,
		vcclient:   sc.vcClient,
	}

	sc.binderRegistry = NewBinderRegistry()

	// add all events handlers
	sc.addEventHandler()
	sc.HyperNodesInfo = schedulingapi.NewHyperNodesInfo(sc.nodeInformer.Lister())
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

	sc.pvcInformer = informerFactory.Core().V1().PersistentVolumeClaims()
	sc.pvcInformer.Informer()
	sc.pvInformer = informerFactory.Core().V1().PersistentVolumes()
	sc.pvInformer.Informer()
	sc.scInformer = informerFactory.Storage().V1().StorageClasses()
	sc.scInformer.Informer()
	sc.csiNodeInformer = informerFactory.Storage().V1().CSINodes()
	sc.csiNodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddOrUpdateCSINode,
			UpdateFunc: sc.UpdateCSINode,
			DeleteFunc: sc.DeleteCSINode,
		},
	)

	if options.ServerOpts != nil && options.ServerOpts.EnableCSIStorage && utilfeature.DefaultFeatureGate.Enabled(features.CSIStorage) {
		sc.csiDriverInformer = informerFactory.Storage().V1().CSIDrivers()
		sc.csiDriverInformer.Informer()
		sc.csiStorageCapacityInformer = informerFactory.Storage().V1beta1().CSIStorageCapacities()
		sc.csiStorageCapacityInformer.Informer()
	}

	sc.podInformer = informerFactory.Core().V1().Pods()
	// create informer for pod information
	sc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch v := obj.(type) {
				case *v1.Pod:
					if !responsibleForPod(v, sc.schedulerNames, sc.schedulerPodName, sc.c) {
						if len(v.Spec.NodeName) == 0 {
							return false
						}
						if !responsibleForNode(v.Spec.NodeName, sc.schedulerPodName, sc.c) {
							return false
						}
					}
					return true
				case cache.DeletedFinalStateUnknown:
					if _, ok := v.Obj.(*v1.Pod); ok {
						// The carried object may be stale, always pass to clean up stale obj in event handlers.
						return true
					}
					klog.Errorf("Cannot convert object %T to *v1.Pod", v.Obj)
					return false
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

	if options.ServerOpts != nil && options.ServerOpts.EnablePriorityClass && utilfeature.DefaultFeatureGate.Enabled(features.PriorityClass) {
		sc.pcInformer = informerFactory.Scheduling().V1().PriorityClasses()
		sc.pcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddPriorityClass,
			UpdateFunc: sc.UpdatePriorityClass,
			DeleteFunc: sc.DeletePriorityClass,
		})
	}

	sc.quotaInformer = informerFactory.Core().V1().ResourceQuotas()
	sc.quotaInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddResourceQuota,
		UpdateFunc: sc.UpdateResourceQuota,
		DeleteFunc: sc.DeleteResourceQuota,
	})

	vcinformers := vcinformer.NewSharedInformerFactory(sc.vcClient, sc.resyncPeriod)
	sc.vcInformerFactory = vcinformers

	// create informer for PodGroup(v1beta1) information
	sc.podGroupInformerV1beta1 = vcinformers.Scheduling().V1beta1().PodGroups()
	sc.podGroupInformerV1beta1.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				var pg *vcv1beta1.PodGroup
				switch v := obj.(type) {
				case *vcv1beta1.PodGroup:
					pg = v
				case cache.DeletedFinalStateUnknown:
					var ok bool
					pg, ok = v.Obj.(*vcv1beta1.PodGroup)
					if !ok {
						klog.Errorf("Cannot convert to podgroup: %v", v.Obj)
						return false
					}
				default:
					return false
				}

				return responsibleForPodGroup(pg, sc.schedulerPodName, sc.c)
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddPodGroupV1beta1,
				UpdateFunc: sc.UpdatePodGroupV1beta1,
				DeleteFunc: sc.DeletePodGroupV1beta1,
			},
		})

	// create informer(v1beta1) for Queue information
	sc.queueInformerV1beta1 = vcinformers.Scheduling().V1beta1().Queues()
	sc.queueInformerV1beta1.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddQueueV1beta1,
		UpdateFunc: sc.UpdateQueueV1beta1,
		DeleteFunc: sc.DeleteQueueV1beta1,
	})

	if utilfeature.DefaultFeatureGate.Enabled(features.ResourceTopology) {
		sc.cpuInformer = vcinformers.Nodeinfo().V1alpha1().Numatopologies()
		sc.cpuInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    sc.AddNumaInfoV1alpha1,
			UpdateFunc: sc.UpdateNumaInfoV1alpha1,
			DeleteFunc: sc.DeleteNumaInfoV1alpha1,
		})
	}

	sc.hyperNodeInformer = sc.vcInformerFactory.Topology().V1alpha1().HyperNodes()
	sc.hyperNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddHyperNode,
		UpdateFunc: sc.UpdateHyperNode,
		DeleteFunc: sc.DeleteHyperNode,
	})

	if utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DynamicResourceAllocation) {
		ctx := context.TODO()
		logger := klog.FromContext(ctx)
		resourceClaimInformer := informerFactory.Resource().V1beta1().ResourceClaims().Informer()
		resourceClaimCache := assumecache.NewAssumeCache(logger, resourceClaimInformer, "ResourceClaim", "", nil)
		resourceSliceTrackerOpts := resourceslicetracker.Options{
			EnableDeviceTaints: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRADeviceTaints),
			SliceInformer:      informerFactory.Resource().V1beta1().ResourceSlices(),
			KubeClient:         sc.kubeClient,
		}
		// If device taints are disabled, the additional informers are not needed and
		// the tracker turns into a simple wrapper around the slice informer.
		if resourceSliceTrackerOpts.EnableDeviceTaints {
			resourceSliceTrackerOpts.TaintInformer = informerFactory.Resource().V1alpha3().DeviceTaintRules()
			resourceSliceTrackerOpts.ClassInformer = informerFactory.Resource().V1beta1().DeviceClasses()
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

	// Sync hyperNode.
	go wait.Until(sc.processSyncHyperNode, 0, stopCh)

	// Re-sync error tasks.
	go wait.Until(sc.processResyncTask, 0, stopCh)

	// Cleanup jobs.
	go wait.Until(sc.processCleanupJob, 0, stopCh)

	go wait.Until(sc.processBindTask, time.Millisecond*20, stopCh)

	// Get metrics data
	klog.V(3).Infof("Start metrics collection, metricsConf is %v", sc.metricsConf)
	interval, err := time.ParseDuration(sc.metricsConf["interval"])
	if err != nil || interval <= 0 {
		interval = defaultMetricsInternal
	}
	klog.V(3).Infof("The interval for querying metrics data is %v", interval)
	go wait.Until(sc.GetMetricsData, interval, stopCh)
}

// WaitForCacheSync sync the cache with the api server
func (sc *SchedulerCache) WaitForCacheSync(stopCh <-chan struct{}) {
	sc.informerFactory.WaitForCacheSync(stopCh)
	sc.vcInformerFactory.WaitForCacheSync(stopCh)
}

// findJobAndTask returns job and the task info
func (sc *SchedulerCache) findJobAndTask(taskInfo *schedulingapi.TaskInfo) (*schedulingapi.JobInfo, *schedulingapi.TaskInfo, error) {
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

// Evict will evict the pod.
//
// If error occurs both task and job are guaranteed to be in the original state.
func (sc *SchedulerCache) Evict(taskInfo *schedulingapi.TaskInfo, reason string) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	job, task, err := sc.findJobAndTask(taskInfo)
	if err != nil {
		return err
	}

	node, found := sc.Nodes[task.NodeName]
	if !found {
		return fmt.Errorf("failed to evict Task %v from host %v, host does not exist",
			task.UID, task.NodeName)
	}

	originalStatus := task.Status
	if err := job.UpdateTaskStatus(task, schedulingapi.Releasing); err != nil {
		return err
	}

	// Add new task to node.
	if err := node.UpdateTask(task); err != nil {
		// After failing to update task to a node we need to revert task status from Releasing,
		// otherwise task might be stuck in the Releasing state indefinitely.
		if err := job.UpdateTaskStatus(task, originalStatus); err != nil {
			klog.Errorf("Task <%s/%s> will be resynchronized after failing to revert status "+
				"from %s to %s after failing to update Task on Node <%s>: %v",
				task.Namespace, task.Name, task.Status, originalStatus, node.Name, err)
			sc.resyncTask(task)
		}
		return err
	}

	p := task.Pod

	go func() {
		err := sc.Evictor.Evict(p, reason)
		if err != nil {
			sc.resyncTask(task)
		}
	}()

	podgroup := &vcv1beta1.PodGroup{}
	if job.PodGroup != nil {
		err = schedulingscheme.Scheme.Convert(&job.PodGroup.PodGroup, podgroup, nil)
	} else {
		err = fmt.Errorf("the PodGroup of Job <%s/%s> is nil", job.Namespace, job.Name)
	}

	if err != nil {
		klog.Errorf("Error while converting PodGroup to v1alpha1.PodGroup with error: %v", err)
		return err
	}
	sc.Recorder.Eventf(podgroup, v1.EventTypeNormal, "Evict", reason)
	return nil
}

// Bind binds task to the target host.
func (sc *SchedulerCache) Bind(ctx context.Context, bindContexts []*BindContext) {
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
			if err := sc.taskUnschedulable(bindContext.TaskInfo, schedulingapi.PodReasonSchedulerError, unschedulableMsg, ""); err != nil {
				klog.ErrorS(err, "Failed to update pod status when bind task error", "task", bindContext.TaskInfo.Name)
			}

			sc.binderRegistry.mu.RLock()
			for _, preBinder := range sc.binderRegistry.preBinders {
				if preBinder != nil {
					preBinder.PreBindRollBack(ctx, bindContext)
				}
			}
			sc.binderRegistry.mu.RUnlock()

			klog.V(2).Infof("resyncTask task %s", bindContext.TaskInfo.Name)
			sc.resyncTask(bindContext.TaskInfo)
		}
	}
}

// BindPodGroup binds job to silo cluster
func (sc *SchedulerCache) BindPodGroup(job *schedulingapi.JobInfo, cluster string) error {
	if _, err := sc.PodGroupBinder.Bind(job, cluster); err != nil {
		klog.Errorf("Bind job <%s> to cluster <%s> failed: %v", job.Name, cluster, err)
		return err
	}
	return nil
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
func (sc *SchedulerCache) taskUnschedulable(task *schedulingapi.TaskInfo, reason, message, nominatedNodeName string) error {
	pod := task.Pod

	condition := &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  reason, // Add more reasons in order to distinguish more specific scenario of pending tasks
		Message: message,
	}

	updateCond := podConditionHaveUpdate(&pod.Status, condition)

	// only update pod's nominatedNodeName when nominatedNodeName is not empty
	// consider this situation:
	// 1. at session 1, the pod A preempt another lower priority pod B, and we updated A's nominatedNodeName
	// 2. at session 2, the pod B is still terminating, so the pod A is still pipelined, but it preempt none, so
	// the nominatedNodeName is empty, but we should not override the A's nominatedNodeName to empty
	updateNomiNode := len(nominatedNodeName) > 0 && podNominatedNodeNameNeedUpdate(&pod.Status, nominatedNodeName)

	if updateCond || updateNomiNode {
		pod = pod.DeepCopy()

		if updateCond && podutil.UpdatePodCondition(&pod.Status, condition) {
			klog.V(3).Infof("Updating pod condition for %s/%s to (%s==%s)", pod.Namespace, pod.Name, condition.Type, condition.Status)
		}

		// if nominatedNode field changed, we should update it to the pod status, for k8s
		// autoscaler will check this field and ignore this pod when scale up.
		if updateNomiNode {
			klog.V(3).Infof("Updating pod nominatedNodeName for %s/%s from (%s) to (%s)", pod.Namespace, pod.Name, pod.Status.NominatedNodeName, nominatedNodeName)
			pod.Status.NominatedNodeName = nominatedNodeName
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

func (sc *SchedulerCache) deleteJob(job *schedulingapi.JobInfo) {
	klog.V(3).Infof("Try to delete Job <%v:%v/%v>", job.UID, job.Namespace, job.Name)

	sc.DeletedJobs.Add(job)
}

func (sc *SchedulerCache) retryDeleteJob(job *schedulingapi.JobInfo) {
	klog.V(3).Infof("Retry to delete Job <%v:%v/%v>", job.UID, job.Namespace, job.Name)

	sc.DeletedJobs.AddRateLimited(job)
}

func (sc *SchedulerCache) processCleanupJob() {
	job, shutdown := sc.DeletedJobs.Get()
	if shutdown {
		return
	}

	defer sc.DeletedJobs.Done(job)

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if schedulingapi.JobTerminated(job) {
		oldJob, found := sc.Jobs[job.UID]
		if !found {
			klog.V(3).Infof("Failed to find Job <%v:%v/%v>, ignore it", job.UID, job.Namespace, job.Name)
			sc.DeletedJobs.Forget(job)
			return
		}
		newPgVersion := oldJob.PgUID
		oldPgVersion := job.PgUID
		klog.V(5).Infof("Just add pguid:%v, try to delete pguid:%v", newPgVersion, oldPgVersion)
		if oldPgVersion == newPgVersion {
			delete(sc.Jobs, job.UID)
			metrics.DeleteJobMetrics(job.Name, string(job.Queue), job.Namespace)
			klog.V(3).Infof("Job <%v:%v/%v> was deleted.", job.UID, job.Namespace, job.Name)
		}
		sc.DeletedJobs.Forget(job)
	} else {
		// Retry
		sc.retryDeleteJob(job)
	}
}

func (sc *SchedulerCache) resyncTask(task *schedulingapi.TaskInfo) {
	key := sc.generateErrTaskKey(task)
	sc.errTasks.AddRateLimited(key)
}

func (sc *SchedulerCache) generateErrTaskKey(task *schedulingapi.TaskInfo) string {
	// Job UID is namespace + / +name, for example: theNs/theJob
	// Task UID is derived from the Pod UID, for example: d336abea-4f14-42c7-8a6b-092959a31407
	// In the example above, the key ultimately becomes: theNs/theJob/d336abea-4f14-42c7-8a6b-092959a31407
	return fmt.Sprintf("%s/%s", task.Job, task.UID)
}

func (sc *SchedulerCache) parseErrTaskKey(key string) (*schedulingapi.TaskInfo, error) {
	i := strings.LastIndex(key, "/")
	if i == -1 {
		return nil, fmt.Errorf("failed to split task key %s", key)
	}

	jobUID := key[:i]
	taskUID := key[i+1:]

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	job, found := sc.Jobs[schedulingapi.JobID(jobUID)]
	if !found {
		return nil, fmt.Errorf("failed to find job %s", jobUID)
	}

	task, found := job.Tasks[schedulingapi.TaskID(taskUID)]
	if !found {
		return nil, fmt.Errorf("failed to find task %s", taskUID)
	}

	return task, nil
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

func (sc *SchedulerCache) processSyncHyperNode() {
	worker := func() bool {
		name, shutdown := sc.hyperNodesQueue.Get()
		if shutdown {
			return false
		}
		defer sc.hyperNodesQueue.Done(name)

		klog.V(5).Infof("started sync hyperNode %s", name)
		err := sc.SyncHyperNode(name)
		if err == nil {
			sc.hyperNodesQueue.Forget(name)
			return true
		}

		klog.ErrorS(err, "Failed to sync hyperNode, retry it.", "name", name)
		sc.hyperNodesQueue.AddRateLimited(name)
		return true
	}
	for worker() {
	}
}

// AddBindTask add task to be bind to a cache which consumes by go runtime
func (sc *SchedulerCache) AddBindTask(bindContext *BindContext) error {
	klog.V(5).Infof("add bind task %v/%v", bindContext.TaskInfo.Namespace, bindContext.TaskInfo.Name)
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	job, task, err := sc.findJobAndTask(bindContext.TaskInfo)
	if err != nil {
		return err
	}

	node, found := sc.Nodes[bindContext.TaskInfo.NodeName]
	if !found {
		return fmt.Errorf("failed to bind Task %v to host %v, host does not exist",
			task.UID, bindContext.TaskInfo.NodeName)
	}

	originalStatus := task.Status
	if err := job.UpdateTaskStatus(task, schedulingapi.Binding); err != nil {
		return err
	}

	err = bindContext.TaskInfo.SetPodResourceDecision()
	if err != nil {
		return fmt.Errorf("set task %v/%v resource decision failed, err %v", task.Namespace, task.Name, err)
	}
	task.NumaInfo = bindContext.TaskInfo.NumaInfo.Clone()

	// Add task to the node.
	if err := node.AddTask(task); err != nil {
		// After failing to update task to a node we need to revert task status from Releasing,
		// otherwise task might be stuck in the Releasing state indefinitely.
		if err := job.UpdateTaskStatus(task, originalStatus); err != nil {
			klog.Errorf("Task <%s/%s> will be resynchronized after failing to revert status "+
				"from %s to %s after failing to update Task on Node <%s>: %v",
				task.Namespace, task.Name, task.Status, originalStatus, node.Name, err)
			sc.resyncTask(task)
		}
		return err
	}

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
func (sc *SchedulerCache) executePreBind(ctx context.Context, bindContext *BindContext) error {
	executedPreBinders := make([]PreBinder, 0, len(sc.binderRegistry.preBinders))

	sc.binderRegistry.mu.RLock()
	defer sc.binderRegistry.mu.RUnlock()

	for _, preBinder := range sc.binderRegistry.preBinders {
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
func (sc *SchedulerCache) executePreBinds(ctx context.Context, bindContexts []*BindContext) []*BindContext {
	logger := klog.FromContext(ctx)
	successfulBindContexts := make([]*BindContext, 0, len(bindContexts))

	for _, bindContext := range bindContexts {
		if err := sc.executePreBind(ctx, bindContext); err != nil {
			reason := fmt.Sprintf("execute preBind failed: %v, resync the task", err)
			klog.Error(reason)
			sc.resyncTask(bindContext.TaskInfo)
			if updateErr := sc.taskUnschedulable(bindContext.TaskInfo, schedulingapi.PodReasonSchedulerError, reason, ""); updateErr != nil {
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
	tmpBindCache := make([]*BindContext, len(sc.bindCache))
	copy(tmpBindCache, sc.bindCache)

	// Currently, bindContexts only contain 1 element.
	go func(bindContexts []*BindContext) {
		logger := klog.Background()
		ctx := klog.NewContext(context.Background(), logger)
		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		successfulPreBindContexts := sc.executePreBinds(cancelCtx, bindContexts)
		sc.Bind(ctx, successfulPreBindContexts)
	}(tmpBindCache)

	// The slice here needs to point to a new underlying array, otherwise bindCache may not be able to trigger garbage collection immediately
	// if it is not expanded, causing memory leaks.
	sc.bindCache = make([]*BindContext, 0)
}

// Snapshot returns the complete snapshot of the cluster from cache
func (sc *SchedulerCache) Snapshot() *schedulingapi.ClusterInfo {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &schedulingapi.ClusterInfo{
		Nodes:               make(map[string]*schedulingapi.NodeInfo),
		HyperNodes:          make(map[string]*schedulingapi.HyperNodeInfo),
		HyperNodesSetByTier: make(map[int]sets.Set[string]),
		RealNodesSet:        make(map[string]sets.Set[string]),
		Jobs:                make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		Queues:              make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
		NamespaceInfo:       make(map[schedulingapi.NamespaceName]*schedulingapi.NamespaceInfo),
		RevocableNodes:      make(map[string]*schedulingapi.NodeInfo),
		NodeList:            make([]string, len(sc.NodeList)),
		CSINodesStatus:      make(map[string]*schedulingapi.CSINodeStatusInfo),
	}

	copy(snapshot.NodeList, sc.NodeList)
	for _, value := range sc.Nodes {
		value.RefreshNumaSchedulerInfoByCrd()
	}

	for _, value := range sc.CSINodesStatus {
		snapshot.CSINodesStatus[value.CSINodeName] = value.Clone()
	}

	for _, value := range sc.Nodes {
		if !value.Ready() {
			continue
		}

		snapshot.Nodes[value.Name] = value.Clone()

		if value.RevocableZone != "" {
			snapshot.RevocableNodes[value.Name] = snapshot.Nodes[value.Name]
		}
	}

	// Snapshot hyperNodes.
	sc.HyperNodesInfo.Lock()
	snapshot.HyperNodes = sc.HyperNodesInfo.HyperNodes()
	snapshot.HyperNodesSetByTier = sc.HyperNodesInfo.HyperNodesSetByTier()
	snapshot.RealNodesSet = sc.HyperNodesInfo.RealNodesSet()
	snapshot.HyperNodesReadyToSchedule = sc.HyperNodesInfo.Ready()
	sc.HyperNodesInfo.Unlock()

	for _, value := range sc.Queues {
		snapshot.Queues[value.UID] = value.Clone()
	}

	var cloneJobLock sync.Mutex
	var wg sync.WaitGroup

	cloneJob := func(value *schedulingapi.JobInfo) {
		defer wg.Done()
		if value.PodGroup != nil {
			value.Priority = sc.defaultPriority

			priName := value.PodGroup.Spec.PriorityClassName
			if priorityClass, found := sc.PriorityClasses[priName]; found {
				value.Priority = priorityClass.Value
			}

			klog.V(4).Infof("The priority of job <%s/%s> is <%s/%d>",
				value.Namespace, value.Name, priName, value.Priority)
		}

		clonedJob := value.Clone()

		cloneJobLock.Lock()
		snapshot.Jobs[value.UID] = clonedJob
		cloneJobLock.Unlock()
	}

	for _, value := range sc.NamespaceCollection {
		info := value.Snapshot()
		snapshot.NamespaceInfo[info.Name] = info
	}

	for _, value := range sc.Jobs {
		// If no scheduling spec, does not handle it.
		if value.PodGroup == nil {
			klog.V(4).Infof("The scheduling spec of Job <%v:%s/%s> is nil, ignore it.",
				value.UID, value.Namespace, value.Name)

			continue
		}

		if _, found := snapshot.Queues[value.Queue]; !found {
			klog.V(3).Infof("The Queue <%v> of Job <%v/%v> does not exist, ignore it.",
				value.Queue, value.Namespace, value.Name)
			continue
		}

		wg.Add(1)
		go cloneJob(value)
	}
	wg.Wait()

	klog.V(3).InfoS("SnapShot for scheduling", "jobNum", len(snapshot.Jobs), "QueueNum",
		len(snapshot.Queues), "NodeNum", len(snapshot.Nodes))
	if klog.V(4).Enabled() {
		klog.InfoS("HyperNode snapShot for scheduling", "tiers", snapshot.HyperNodesSetByTier, "realNodesSet", snapshot.RealNodesSet, "hyperNodesReadyToSchedule", snapshot.HyperNodesReadyToSchedule)
	}
	return snapshot
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

	if len(sc.Jobs) != 0 {
		str += "Jobs:\n"
		for _, job := range sc.Jobs {
			str += fmt.Sprintf("\t %s\n", job)
		}
	}

	if len(sc.NamespaceCollection) != 0 {
		str += "Namespaces:\n"
		for _, ns := range sc.NamespaceCollection {
			info := ns.Snapshot()
			str += fmt.Sprintf("\t Namespace(%s)\n", info.Name)
		}
	}

	if len(sc.NodeList) != 0 {
		str += fmt.Sprintf("NodeList: %v\n", sc.NodeList)
	}

	return str
}

// RecordJobStatusEvent records related events according to job status.
func (sc *SchedulerCache) RecordJobStatusEvent(job *schedulingapi.JobInfo, updatePG bool) {
	pgUnschedulable := job.PodGroup != nil &&
		(job.PodGroup.Status.Phase == scheduling.PodGroupUnknown ||
			job.PodGroup.Status.Phase == scheduling.PodGroupPending ||
			job.PodGroup.Status.Phase == scheduling.PodGroupInqueue)

	fitErrStr := job.FitError()
	// If pending or unschedulable, record unschedulable event.
	if pgUnschedulable {
		msg := fmt.Sprintf("%v/%v tasks in gang unschedulable: %v",
			len(job.TaskStatusIndex[schedulingapi.Pending]),
			len(job.Tasks),
			fitErrStr)
		// TODO: should we skip pod unschedulable event if pod group is unschedulable due to gates to avoid printing too many messages?
		sc.recordPodGroupEvent(job.PodGroup, v1.EventTypeWarning, string(scheduling.PodGroupUnschedulableType), msg)
	} else if updatePG {
		sc.recordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupScheduled), string(scheduling.PodGroupReady))
	}

	baseErrorMessage := fitErrStr
	if baseErrorMessage == "" {
		baseErrorMessage = schedulingapi.AllNodeUnavailableMsg
	}
	// Update podCondition for tasks Allocated and Pending before job discarded
	for _, status := range []schedulingapi.TaskStatus{schedulingapi.Allocated, schedulingapi.Pending, schedulingapi.Pipelined} {
		statusTasks := job.TaskStatusIndex[status]
		taskInfos := make([]*schedulingapi.TaskInfo, 0, len(statusTasks))
		for _, task := range statusTasks {
			taskInfos = append(taskInfos, task)
		}

		workqueue.ParallelizeUntil(context.TODO(), taskUpdaterWorker, len(taskInfos), func(index int) {
			taskInfo := taskInfos[index]

			// The pod of a scheduling gated task is given
			// the ScheduleGated condition by the api-server. Do not change it.
			if taskInfo.SchGated {
				return
			}

			reason, msg, nominatedNodeName := job.TaskSchedulingReason(taskInfo.UID)
			if len(msg) == 0 {
				msg = baseErrorMessage
			}

			if err := sc.taskUnschedulable(taskInfo, reason, msg, nominatedNodeName); err != nil {
				klog.ErrorS(err, "Failed to update unschedulable task status", "task", klog.KRef(taskInfo.Namespace, taskInfo.Name),
					"reason", reason, "message", msg)
			}
		})
	}
}

// UpdateJobStatus update the status of job and its tasks.
func (sc *SchedulerCache) UpdateJobStatus(job *schedulingapi.JobInfo, updatePGStatus, updatePGAnnotations bool) (*schedulingapi.JobInfo, error) {
	if updatePGStatus || updatePGAnnotations {
		if updatePGAnnotations {
			sc.updateJobAnnotations(job)
		}
		pg, err := sc.StatusUpdater.UpdatePodGroup(job.PodGroup)
		if err != nil {
			return nil, err
		}
		job.PodGroup = pg
	}
	sc.RecordJobStatusEvent(job, updatePGStatus)

	return job, nil
}

func (sc *SchedulerCache) updateJobAnnotations(job *schedulingapi.JobInfo) {
	sc.Mutex.Lock()
	sc.Jobs[job.UID].PodGroup.GetAnnotations()[schedulingapi.JobAllocatedHyperNode] = job.PodGroup.GetAnnotations()[schedulingapi.JobAllocatedHyperNode]
	sc.Mutex.Unlock()
}

// UpdateQueueStatus update the status of queue.
func (sc *SchedulerCache) UpdateQueueStatus(queue *schedulingapi.QueueInfo) error {
	return sc.StatusUpdater.UpdateQueueStatus(queue)
}

func (sc *SchedulerCache) recordPodGroupEvent(podGroup *schedulingapi.PodGroup, eventType, reason, msg string) {
	if podGroup == nil {
		return
	}

	pg := &vcv1beta1.PodGroup{}
	if err := schedulingscheme.Scheme.Convert(&podGroup.PodGroup, pg, nil); err != nil {
		klog.Errorf("Error while converting PodGroup to v1alpha1.PodGroup with error: %v", err)
		return
	}
	sc.Recorder.Eventf(pg, eventType, reason, msg)
}

func (sc *SchedulerCache) SetMetricsConf(conf map[string]string) {
	sc.metricsConf = conf
}

func (sc *SchedulerCache) GetMetricsData() {
	metricsType := sc.metricsConf["type"]
	if len(metricsType) == 0 {
		klog.V(3).Infof("The metrics type is not set in the volcano scheduler configmap file. " +
			"As a result, the CPU and memory load information of the node is not collected.")
		return
	}

	client, err := source.NewMetricsClient(sc.restConfig, sc.metricsConf)
	if err != nil {
		klog.Errorf("Error creating client: %v\n", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()
	nodeMetricsMap := make(map[string]*source.NodeMetrics, len(sc.NodeList))
	sc.Mutex.Lock()

	for _, nodeName := range sc.NodeList {
		nodeMetricsMap[nodeName] = &source.NodeMetrics{}
	}
	sc.Mutex.Unlock()

	err = client.NodesMetricsAvg(ctx, nodeMetricsMap)
	if err != nil {
		klog.Errorf("Error getting node metrics: %v\n", err)
		return
	}

	sc.setMetricsData(nodeMetricsMap)
}

func (sc *SchedulerCache) setMetricsData(usageInfo map[string]*source.NodeMetrics) {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	for nodeName, nodeMetric := range usageInfo {
		nodeUsage := &schedulingapi.NodeUsage{
			CPUUsageAvg: make(map[string]float64),
			MEMUsageAvg: make(map[string]float64),
		}
		nodeUsage.MetricsTime = nodeMetric.MetricsTime
		nodeUsage.CPUUsageAvg[source.NODE_METRICS_PERIOD] = nodeMetric.CPU
		nodeUsage.MEMUsageAvg[source.NODE_METRICS_PERIOD] = nodeMetric.Memory

		nodeInfo, ok := sc.Nodes[nodeName]
		if !ok {
			klog.Errorf("The information about node %s cannot be found in the cache.", nodeName)
			continue
		}
		klog.V(5).Infof("node: %s, ResourceUsage: %+v => %+v", nodeName, *nodeInfo.ResourceUsage, nodeUsage)
		nodeInfo.ResourceUsage = nodeUsage
	}
}

// createImageStateSummary returns a summarizing snapshot of the given image's state.
func (sc *SchedulerCache) createImageStateSummary(state *imageState) *k8sframework.ImageStateSummary {
	return &k8sframework.ImageStateSummary{
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
