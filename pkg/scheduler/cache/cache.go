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

	"github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	pmodel "github.com/prometheus/common/model"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	volumescheduling "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingscheme "volcano.sh/apis/pkg/apis/scheduling/scheme"
	vcv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	cpuinformerv1 "volcano.sh/apis/pkg/client/informers/externalversions/nodeinfo/v1alpha1"
	vcinformerv1 "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

const (
	// record name of cpu average usage defined in prometheus rules
	cpuUsageAvg = "cpu_usage_avg"
	// record name of mem average usage defined in prometheus rules
	memUsageAvg = "mem_usage_avg"
	// default interval for sync data from metrics server, the value is 5s
	defaultMetricsInternal = 5000000000
)

func init() {
	schemeBuilder := runtime.SchemeBuilder{
		v1.AddToScheme,
	}

	utilruntime.Must(schemeBuilder.AddToScheme(scheme.Scheme))
}

// New returns a Cache implementation.
func New(config *rest.Config, schedulerName string, defaultQueue string, nodeSelectors []string) Cache {
	return newSchedulerCache(config, schedulerName, defaultQueue, nodeSelectors)
}

// SchedulerCache cache for the kube batch
type SchedulerCache struct {
	sync.Mutex

	kubeClient   *kubernetes.Clientset
	vcClient     *vcclient.Clientset
	defaultQueue string
	// schedulerName is the name for volcano scheduler
	schedulerName      string
	nodeSelectorLabels map[string]string
	metricsConf        map[string]string

	podInformer                infov1.PodInformer
	nodeInformer               infov1.NodeInformer
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
	VolumeBinder   VolumeBinder

	Recorder record.EventRecorder

	Jobs                 map[schedulingapi.JobID]*schedulingapi.JobInfo
	Nodes                map[string]*schedulingapi.NodeInfo
	Queues               map[schedulingapi.QueueID]*schedulingapi.QueueInfo
	PriorityClasses      map[string]*schedulingv1.PriorityClass
	NodeList             []string
	defaultPriorityClass *schedulingv1.PriorityClass
	defaultPriority      int32

	NamespaceCollection map[string]*schedulingapi.NamespaceCollection

	errTasks    workqueue.RateLimitingInterface
	DeletedJobs workqueue.RateLimitingInterface

	informerFactory   informers.SharedInformerFactory
	vcInformerFactory vcinformer.SharedInformerFactory

	BindFlowChannel chan *schedulingapi.TaskInfo
	bindCache       []*schedulingapi.TaskInfo
	batchNum        int
}

type DefaultBinder struct {
	// kubeclient *kubernetes.Clientset
}

//Bind will send bind request to api server
func (db *DefaultBinder) Bind(kubeClient *kubernetes.Clientset, tasks []*schedulingapi.TaskInfo) ([]*schedulingapi.TaskInfo, error) {
	var errTasks []*schedulingapi.TaskInfo
	for _, task := range tasks {
		p := task.Pod
		if err := kubeClient.CoreV1().Pods(p.Namespace).Bind(context.TODO(),
			&v1.Binding{
				ObjectMeta: metav1.ObjectMeta{Namespace: p.Namespace, Name: p.Name, UID: p.UID, Annotations: p.Annotations},
				Target: v1.ObjectReference{
					Kind: "Node",
					Name: task.NodeName,
				},
			},
			metav1.CreateOptions{}); err != nil {
			klog.Errorf("Failed to bind pod <%v/%v> to node %s : %#v", p.Namespace, p.Name, task.NodeName, err)
			errTasks = append(errTasks, task)
		}
	}

	if len(errTasks) > 0 {
		return errTasks, fmt.Errorf("failed to bind pods")
	}

	return nil, nil
}

func NewBinder() *DefaultBinder {
	return &DefaultBinder{}
}

type defaultEvictor struct {
	kubeclient *kubernetes.Clientset
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
	kubeclient *kubernetes.Clientset
	vcclient   *vcclient.Clientset
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
	klog.V(3).Infof("Updating pod condition for %s/%s to (%s==%s)", pod.Namespace, pod.Name, condition.Type, condition.Status)
	if podutil.UpdatePodCondition(&pod.Status, condition) {
		return su.kubeclient.CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), pod, metav1.UpdateOptions{})
	}
	return pod, nil
}

// UpdatePodGroup will Update pod with podCondition
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

type defaultVolumeBinder struct {
	volumeBinder volumescheduling.SchedulerVolumeBinder
}

// AllocateVolumes allocates volume on the host to the task
func (dvb *defaultVolumeBinder) AllocateVolumes(task *schedulingapi.TaskInfo, hostname string, podVolumes *volumescheduling.PodVolumes) error {
	allBound, err := dvb.volumeBinder.AssumePodVolumes(task.Pod, hostname, podVolumes)
	task.VolumeReady = allBound

	return err
}

// RevertVolumes clean cache generated by AllocateVolumes
func (dvb *defaultVolumeBinder) RevertVolumes(task *schedulingapi.TaskInfo, podVolumes *volumescheduling.PodVolumes) {
	if podVolumes != nil {
		klog.Infof("Revert assumed volumes for task %v/%v on node %s", task.Namespace, task.Name, task.NodeName)
		dvb.volumeBinder.RevertAssumedPodVolumes(podVolumes)
		task.VolumeReady = false
		task.PodVolumes = nil
	}
}

// GetPodVolumes get pod volume on the host
func (dvb *defaultVolumeBinder) GetPodVolumes(task *schedulingapi.TaskInfo,
	node *v1.Node) (podVolumes *volumescheduling.PodVolumes, err error) {
	boundClaims, claimsToBind, unboundClaimsImmediate, err := dvb.volumeBinder.GetPodVolumes(task.Pod)
	if err != nil {
		return nil, err
	}
	if len(unboundClaimsImmediate) > 0 {
		return nil, fmt.Errorf("pod has unbound immediate PersistentVolumeClaims")
	}

	podVolumes, reasons, err := dvb.volumeBinder.FindPodVolumes(task.Pod, boundClaims, claimsToBind, node)
	if err != nil {
		return nil, err
	} else if len(reasons) > 0 {
		var errors []string
		for _, reason := range reasons {
			errors = append(errors, string(reason))
		}
		return nil, fmt.Errorf(strings.Join(errors, ","))
	}

	return podVolumes, err
}

// BindVolumes binds volumes to the task
func (dvb *defaultVolumeBinder) BindVolumes(task *schedulingapi.TaskInfo, podVolumes *volumescheduling.PodVolumes) error {
	// If task's volumes are ready, did not bind them again.
	if task.VolumeReady {
		return nil
	}

	return dvb.volumeBinder.BindPodVolumes(task.Pod, podVolumes)
}

type podgroupBinder struct {
	kubeclient *kubernetes.Clientset
	vcclient   *vcclient.Clientset
}

// Bind will add silo cluster annotaion on pod and podgroup
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

func newSchedulerCache(config *rest.Config, schedulerName string, defaultQueue string, nodeSelectors []string) *SchedulerCache {
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

	// create default queue
	reclaimable := true
	defaultQue := vcv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultQueue,
		},
		Spec: vcv1beta1.QueueSpec{
			Reclaimable: &reclaimable,
			Weight:      1,
		},
	}
	if _, err := vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), &defaultQue, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		panic(fmt.Sprintf("failed init default queue, with err: %v", err))
	}

	sc := &SchedulerCache{
		Jobs:                make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		Nodes:               make(map[string]*schedulingapi.NodeInfo),
		Queues:              make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
		PriorityClasses:     make(map[string]*schedulingv1.PriorityClass),
		errTasks:            workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		DeletedJobs:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		kubeClient:          kubeClient,
		vcClient:            vcClient,
		defaultQueue:        defaultQueue,
		schedulerName:       schedulerName,
		nodeSelectorLabels:  make(map[string]string),
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),

		NodeList: []string{},
	}
	if len(nodeSelectors) > 0 {
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
			sc.nodeSelectorLabels[key] = ""
		}
	}
	// Prepare event clients.
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: eventClient.CoreV1().Events("")})
	sc.Recorder = broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: schedulerName})

	sc.BindFlowChannel = make(chan *schedulingapi.TaskInfo, 5000)
	sc.Binder = GetBindMethod()

	var batchNum int
	batchNum, err = strconv.Atoi(os.Getenv("BATCH_BIND_NUM"))
	if err == nil && batchNum > 0 {
		sc.batchNum = batchNum
	} else {
		sc.batchNum = 1
	}

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

	informerFactory := informers.NewSharedInformerFactory(sc.kubeClient, 0)
	sc.informerFactory = informerFactory
	mySchedulerPodName, c := getMultiSchedulerInfo()

	// create informer for node information
	sc.nodeInformer = informerFactory.Core().V1().Nodes()
	sc.nodeInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				node, ok := obj.(*v1.Node)
				if !ok {
					klog.Errorf("Cannot convert to *v1.Node: %v", obj)
					return false
				}
				if !responsibleForNode(node.Name, mySchedulerPodName, c) {
					return false
				}
				if len(sc.nodeSelectorLabels) == 0 {
					return true
				}
				for labelName, labelValue := range node.Labels {
					key := labelName + ":" + labelValue
					if _, ok := sc.nodeSelectorLabels[key]; ok {
						return true
					}
				}
				klog.Infof("node %s ignore add/update/delete into schedulerCache", node.Name)
				return false
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddNode,
				UpdateFunc: sc.UpdateNode,
				DeleteFunc: sc.DeleteNode,
			},
		},
		0,
	)

	sc.podInformer = informerFactory.Core().V1().Pods()
	sc.pvcInformer = informerFactory.Core().V1().PersistentVolumeClaims()
	sc.pvInformer = informerFactory.Core().V1().PersistentVolumes()
	sc.scInformer = informerFactory.Storage().V1().StorageClasses()
	sc.csiNodeInformer = informerFactory.Storage().V1().CSINodes()
	sc.csiDriverInformer = informerFactory.Storage().V1().CSIDrivers()
	sc.csiStorageCapacityInformer = informerFactory.Storage().V1beta1().CSIStorageCapacities()

	var capacityCheck *volumescheduling.CapacityCheck
	if options.ServerOpts.EnableCSIStorage {
		capacityCheck = &volumescheduling.CapacityCheck{
			CSIDriverInformer:          sc.csiDriverInformer,
			CSIStorageCapacityInformer: sc.csiStorageCapacityInformer,
		}
	} else {
		capacityCheck = nil
	}

	sc.VolumeBinder = &defaultVolumeBinder{
		volumeBinder: volumescheduling.NewVolumeBinder(
			sc.kubeClient,
			sc.podInformer,
			sc.nodeInformer,
			sc.csiNodeInformer,
			sc.pvcInformer,
			sc.pvInformer,
			sc.scInformer,
			capacityCheck,
			30*time.Second,
		),
	}

	// create informer for pod information
	sc.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch v := obj.(type) {
				case *v1.Pod:
					if !responsibleForPod(v, schedulerName, mySchedulerPodName, c) {
						if len(v.Spec.NodeName) == 0 {
							return false
						}
						if !responsibleForNode(v.Spec.NodeName, mySchedulerPodName, c) {
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

	if options.ServerOpts.EnablePriorityClass {
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

	vcinformers := vcinformer.NewSharedInformerFactory(sc.vcClient, 0)
	sc.vcInformerFactory = vcinformers

	// create informer for PodGroup(v1beta1) information
	sc.podGroupInformerV1beta1 = vcinformers.Scheduling().V1beta1().PodGroups()
	sc.podGroupInformerV1beta1.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch v := obj.(type) {
				case *vcv1beta1.PodGroup:
					return responsibleForPodGroup(v, mySchedulerPodName, c)
				default:
					return false
				}
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

	sc.cpuInformer = vcinformers.Nodeinfo().V1alpha1().Numatopologies()
	sc.cpuInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    sc.AddNumaInfoV1alpha1,
		UpdateFunc: sc.UpdateNumaInfoV1alpha1,
		DeleteFunc: sc.DeleteNumaInfoV1alpha1,
	})
	return sc
}

// Run  starts the schedulerCache
func (sc *SchedulerCache) Run(stopCh <-chan struct{}) {
	sc.informerFactory.Start(stopCh)
	sc.vcInformerFactory.Start(stopCh)
	// Re-sync error tasks.
	go wait.Until(sc.processResyncTask, 0, stopCh)

	// Cleanup jobs.
	go wait.Until(sc.processCleanupJob, 0, stopCh)

	go wait.Until(sc.processBindTask, time.Millisecond*20, stopCh)

	// Get metrics data
	interval, err := time.ParseDuration(sc.metricsConf["interval"])
	if err != nil || interval <= 0 {
		interval = time.Duration(defaultMetricsInternal)
	}
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
		return fmt.Errorf("failed to bind Task %v to host %v, host does not exist",
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
	if err := schedulingscheme.Scheme.Convert(&job.PodGroup.PodGroup, podgroup, nil); err != nil {
		klog.Errorf("Error while converting PodGroup to v1alpha1.PodGroup with error: %v", err)
		return err
	}
	sc.Recorder.Eventf(podgroup, v1.EventTypeNormal, "Evict", reason)
	return nil
}

// Bind binds task to the target host.
func (sc *SchedulerCache) Bind(tasks []*schedulingapi.TaskInfo) error {
	go func(taskArray []*schedulingapi.TaskInfo) {
		tmp := time.Now()
		errTasks, err := sc.Binder.Bind(sc.kubeClient, taskArray)
		if err == nil {
			klog.V(3).Infof("bind ok, latency %v", time.Since(tmp))
			for _, task := range tasks {
				sc.Recorder.Eventf(task.Pod, v1.EventTypeNormal, "Scheduled", "Successfully assigned %v/%v to %v",
					task.Namespace, task.Name, task.NodeName)
			}
		} else {
			for _, task := range errTasks {
				klog.V(2).Infof("resyncTask task %s", task.Name)
				sc.VolumeBinder.RevertVolumes(task, task.PodVolumes)
				sc.resyncTask(task)
			}
		}
	}(tasks)
	return nil
}

// BindPodGroup binds job to silo cluster
func (sc *SchedulerCache) BindPodGroup(job *schedulingapi.JobInfo, cluster string) error {
	if _, err := sc.PodGroupBinder.Bind(job, cluster); err != nil {
		klog.Errorf("Bind job <%s> to cluster <%s> failed: %v", job.Name, cluster, err)
		return err
	}
	return nil
}

// GetPodVolumes get pod volume on the host
func (sc *SchedulerCache) GetPodVolumes(task *schedulingapi.TaskInfo, node *v1.Node) (*volumescheduling.PodVolumes, error) {
	return sc.VolumeBinder.GetPodVolumes(task, node)
}

// AllocateVolumes allocates volume on the host to the task
func (sc *SchedulerCache) AllocateVolumes(task *schedulingapi.TaskInfo, hostname string, podVolumes *volumescheduling.PodVolumes) error {
	return sc.VolumeBinder.AllocateVolumes(task, hostname, podVolumes)
}

// BindVolumes binds volumes to the task
func (sc *SchedulerCache) BindVolumes(task *schedulingapi.TaskInfo, podVolumes *volumescheduling.PodVolumes) error {
	return sc.VolumeBinder.BindVolumes(task, podVolumes)
}

// RevertVolumes clean cache generated by AllocateVolumes
func (sc *SchedulerCache) RevertVolumes(task *schedulingapi.TaskInfo, podVolumes *volumescheduling.PodVolumes) {
	sc.VolumeBinder.RevertVolumes(task, podVolumes)
}

// Client returns the kubernetes clientSet
func (sc *SchedulerCache) Client() kubernetes.Interface {
	return sc.kubeClient
}

// SharedInformerFactory returns the scheduler SharedInformerFactory
func (sc *SchedulerCache) SharedInformerFactory() informers.SharedInformerFactory {
	return sc.informerFactory
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
func (sc *SchedulerCache) taskUnschedulable(task *schedulingapi.TaskInfo, reason, message string) error {
	pod := task.Pod

	condition := &v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  reason, // Add more reasons in order to distinguish more specific scenario of pending tasks
		Message: message,
	}

	if podConditionHaveUpdate(&pod.Status, condition) {
		pod = pod.DeepCopy()

		// The reason field in 'Events' should be "FailedScheduling", there is not constants defined for this in
		// k8s core, so using the same string here.
		// The reason field in PodCondition can be "Unschedulable"
		sc.Recorder.Eventf(pod, v1.EventTypeWarning, "FailedScheduling", message)
		if _, err := sc.StatusUpdater.UpdatePodCondition(pod, condition); err != nil {
			return err
		}
	} else {
		klog.V(4).Infof("task unscheduleable %s/%s, message: %s, skip by no condition update", pod.Namespace, pod.Name, message)
	}

	return nil
}

func (sc *SchedulerCache) deleteJob(job *schedulingapi.JobInfo) {
	klog.V(3).Infof("Try to delete Job <%v:%v/%v>", job.UID, job.Namespace, job.Name)

	sc.DeletedJobs.AddRateLimited(job)
}

func (sc *SchedulerCache) processCleanupJob() {
	obj, shutdown := sc.DeletedJobs.Get()
	if shutdown {
		return
	}

	defer sc.DeletedJobs.Done(obj)

	job, found := obj.(*schedulingapi.JobInfo)
	if !found {
		klog.Errorf("Failed to convert <%v> to *JobInfo", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if schedulingapi.JobTerminated(job) {
		delete(sc.Jobs, job.UID)
		metrics.DeleteJobMetrics(job.Name, string(job.Queue), job.Namespace)
		klog.V(3).Infof("Job <%v:%v/%v> was deleted.", job.UID, job.Namespace, job.Name)
	} else {
		// Retry
		sc.deleteJob(job)
	}
}

func (sc *SchedulerCache) resyncTask(task *schedulingapi.TaskInfo) {
	sc.errTasks.AddRateLimited(task)
}

func (sc *SchedulerCache) processResyncTask() {
	obj, shutdown := sc.errTasks.Get()
	if shutdown {
		return
	}

	defer sc.errTasks.Done(obj)

	task, ok := obj.(*schedulingapi.TaskInfo)
	if !ok {
		klog.Errorf("failed to convert %v to *schedulingapi.TaskInfo", obj)
		return
	}

	if err := sc.syncTask(task); err != nil {
		klog.Errorf("Failed to sync pod <%v/%v>, retry it.", task.Namespace, task.Name)
		sc.resyncTask(task)
	}
}

func (sc *SchedulerCache) AddBindTask(taskInfo *schedulingapi.TaskInfo) error {
	klog.V(5).Infof("add bind task %v/%v", taskInfo.Namespace, taskInfo.Name)
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	job, task, err := sc.findJobAndTask(taskInfo)
	if err != nil {
		return err
	}

	node, found := sc.Nodes[taskInfo.NodeName]
	if !found {
		return fmt.Errorf("failed to bind Task %v to host %v, host does not exist",
			task.UID, taskInfo.NodeName)
	}

	originalStatus := task.Status
	if err := job.UpdateTaskStatus(task, schedulingapi.Binding); err != nil {
		return err
	}

	err = taskInfo.SetPodResourceDecision()
	if err != nil {
		return fmt.Errorf("set task %v/%v resource decision failed, err %v", task.Namespace, task.Name, err)
	}
	task.NumaInfo = taskInfo.NumaInfo.Clone()

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

	sc.BindFlowChannel <- taskInfo

	return nil
}

func (sc *SchedulerCache) processBindTask() {
	for {
		select {
		case taskInfo, ok := <-sc.BindFlowChannel:
			if !ok {
				return
			}

			sc.bindCache = append(sc.bindCache, taskInfo)
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

func (sc *SchedulerCache) BindTask() {
	klog.V(5).Infof("batch bind task count %d", len(sc.bindCache))
	for _, task := range sc.bindCache {
		if err := sc.VolumeBinder.BindVolumes(task, task.PodVolumes); err != nil {
			klog.Errorf("task %s/%s bind Volumes failed: %#v", task.Namespace, task.Name, err)
			sc.VolumeBinder.RevertVolumes(task, task.PodVolumes)
			sc.resyncTask(task)
			return
		}
	}

	bindTasks := make([]*schedulingapi.TaskInfo, len(sc.bindCache))
	copy(bindTasks, sc.bindCache)
	if err := sc.Bind(bindTasks); err != nil {
		return
	}

	for _, task := range sc.bindCache {
		metrics.UpdateTaskScheduleDuration(metrics.Duration(task.Pod.CreationTimestamp.Time))
	}

	sc.bindCache = sc.bindCache[0:0]
}

// Snapshot returns the complete snapshot of the cluster from cache
func (sc *SchedulerCache) Snapshot() *schedulingapi.ClusterInfo {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &schedulingapi.ClusterInfo{
		Nodes:          make(map[string]*schedulingapi.NodeInfo),
		Jobs:           make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		Queues:         make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
		NamespaceInfo:  make(map[schedulingapi.NamespaceName]*schedulingapi.NamespaceInfo),
		RevocableNodes: make(map[string]*schedulingapi.NodeInfo),
		NodeList:       make([]string, len(sc.NodeList)),
	}

	copy(snapshot.NodeList, sc.NodeList)
	for _, value := range sc.Nodes {
		value.RefreshNumaSchedulerInfoByCrd()
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
		klog.V(4).Infof("Namespace %s has weight %v",
			value.Name, info.GetWeight())
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

	klog.V(3).Infof("There are <%d> Jobs, <%d> Queues and <%d> Nodes in total for scheduling.",
		len(snapshot.Jobs), len(snapshot.Queues), len(snapshot.Nodes))

	return snapshot
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
			str += fmt.Sprintf("\t Namespace(%s) Weight(%v)\n",
				info.Name, info.Weight)
		}
	}

	if len(sc.NodeList) != 0 {
		str += fmt.Sprintf("NodeList: %v\n", sc.NodeList)
	}

	return str
}

// RecordJobStatusEvent records related events according to job status.
func (sc *SchedulerCache) RecordJobStatusEvent(job *schedulingapi.JobInfo) {
	pgUnschedulable := job.PodGroup != nil &&
		(job.PodGroup.Status.Phase == scheduling.PodGroupUnknown ||
			job.PodGroup.Status.Phase == scheduling.PodGroupPending ||
			job.PodGroup.Status.Phase == scheduling.PodGroupInqueue)

	// If pending or unschedulable, record unschedulable event.
	if pgUnschedulable {
		msg := fmt.Sprintf("%v/%v tasks in gang unschedulable: %v",
			len(job.TaskStatusIndex[schedulingapi.Pending]),
			len(job.Tasks),
			job.FitError())
		sc.recordPodGroupEvent(job.PodGroup, v1.EventTypeWarning, string(scheduling.PodGroupUnschedulableType), msg)
	} else {
		sc.recordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupScheduled), string(scheduling.PodGroupReady))
	}

	baseErrorMessage := job.JobFitErrors
	if baseErrorMessage == "" {
		baseErrorMessage = schedulingapi.AllNodeUnavailableMsg
	}
	// Update podCondition for tasks Allocated and Pending before job discarded
	for _, status := range []schedulingapi.TaskStatus{schedulingapi.Allocated, schedulingapi.Pending, schedulingapi.Pipelined} {
		for _, taskInfo := range job.TaskStatusIndex[status] {
			reason, msg := job.TaskSchedulingReason(taskInfo.UID)
			if len(msg) == 0 {
				msg = baseErrorMessage
			}
			if err := sc.taskUnschedulable(taskInfo, reason, msg); err != nil {
				klog.Errorf("Failed to update unschedulable task status <%s/%s>: %v",
					taskInfo.Namespace, taskInfo.Name, err)
			}
		}
	}
}

// UpdateJobStatus update the status of job and its tasks.
func (sc *SchedulerCache) UpdateJobStatus(job *schedulingapi.JobInfo, updatePG bool) (*schedulingapi.JobInfo, error) {
	if updatePG {
		pg, err := sc.StatusUpdater.UpdatePodGroup(job.PodGroup)
		if err != nil {
			return nil, err
		}
		job.PodGroup = pg
	}

	sc.RecordJobStatusEvent(job)

	return job, nil
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
	address := sc.metricsConf["address"]
	if len(address) == 0 {
		return
	}
	klog.V(4).Infof("Get metrics from Prometheus: %s", address)
	client, err := api.NewClient(api.Config{
		Address: address,
	})
	if err != nil {
		klog.Errorf("Error creating client: %v\n", err)
		return
	}
	v1api := prometheusv1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()
	nodeUsageMap := make(map[string]*schedulingapi.NodeUsage)
	sc.Mutex.Lock()
	for k := range sc.Nodes {
		nodeUsageMap[k] = &schedulingapi.NodeUsage{
			CPUUsageAvg: make(map[string]float64),
			MEMUsageAvg: make(map[string]float64),
		}
	}
	sc.Mutex.Unlock()

	supportedPeriods := []string{"5m"}
	supportedMetrics := []string{cpuUsageAvg, memUsageAvg}
	for node := range nodeUsageMap {
		for _, period := range supportedPeriods {
			for _, metric := range supportedMetrics {
				queryStr := fmt.Sprintf("%s_%s{instance=\"%s\"}", metric, period, node)
				klog.V(4).Infof("Query prometheus by %s", queryStr)
				res, warnings, err := v1api.Query(ctx, queryStr, time.Now())
				if err != nil {
					klog.Errorf("Error querying Prometheus: %v", err)
				}
				if len(warnings) > 0 {
					klog.V(3).Infof("Warning querying Prometheus: %v", warnings)
				}
				if res == nil || res.String() == "" {
					klog.Warningf("Warning querying Prometheus: no data found for %s", queryStr)
					continue
				}
				// plugin.usage only need type pmodel.ValVector in Prometheus.rulues
				if res.Type() != pmodel.ValVector {
					continue
				}
				// only method res.String() can get data, dataType []pmodel.ValVector, eg: "{k1:v1, ...} => #[value] @#[timespace]\n {k2:v2, ...} => ..."
				firstRowValVector := strings.Split(res.String(), "\n")[0]
				rowValues := strings.Split(strings.TrimSpace(firstRowValVector), "=>")
				value := strings.Split(strings.TrimSpace(rowValues[1]), " ")
				switch metric {
				case cpuUsageAvg:
					cpuUsage, _ := strconv.ParseFloat(value[0], 64)
					nodeUsageMap[node].CPUUsageAvg[period] = cpuUsage
					klog.V(4).Infof("node: %v, CpuUsageAvg: %v, period:%v", node, cpuUsage, period)
				case memUsageAvg:
					memUsage, _ := strconv.ParseFloat(value[0], 64)
					nodeUsageMap[node].MEMUsageAvg[period] = memUsage
					klog.V(4).Infof("node: %v, MemUsageAvg: %v, period:%v", node, memUsage, period)
				}
			}
		}
	}
	sc.setMetricsData(nodeUsageMap)
}

func (sc *SchedulerCache) setMetricsData(usageInfo map[string]*schedulingapi.NodeUsage) {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	for k := range usageInfo {
		nodeInfo, ok := sc.Nodes[k]
		if ok {
			klog.V(3).Infof("node: %s, ResourceUsage: %+v => %+v", k, *nodeInfo.ResourceUsage, *usageInfo[k])
			nodeInfo.ResourceUsage = usageInfo[k]
		}
	}
}
