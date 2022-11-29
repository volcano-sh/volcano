package vqa

import (
	"context"
	"github.com/robfig/cron/v3"
	_ "github.com/robfig/cron/v3"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"math/rand"
	"time"
	autoscalingv1alpha1 "volcano.sh/apis/pkg/apis/autoscaling/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	versionedscheme "volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	scalingInformer "volcano.sh/apis/pkg/client/informers/externalversions/autoscaling/v1alpha1"
	scalingLister "volcano.sh/apis/pkg/client/listers/autoscaling/v1alpha1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/util"
)

const (
	VQAMaxRequeueNum  int           = 3
	VQARetryDuration  time.Duration = time.Second * 5
	nextScheduleDelta               = 100 * time.Millisecond
)

func init() {
	framework.RegisterController(&vqacontroller{})
}

type vqacontroller struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	vqaInformer scalingInformer.VerticalQueueAutoscalerInformer

	vqaLister scalingLister.VerticalQueueAutoscalerLister
	vqaSynced cache.InformerSynced

	// queueLister
	queueLister schedulinglister.QueueLister
	//queueSynced cache.InformerSynced

	// tidalQueue that need to be updated, and it for tidal.
	tidalQueue workqueue.DelayingInterface

	enqueueVQA      func(req *apis.Request)
	enqueueVQAAfter func(req *apis.Request, after time.Duration)

	recorder           record.EventRecorder
	maxTidalRequeueNum int

	// now is a function that returns current time, done to facilitate unit tests
	now func() time.Time
}

func (v *vqacontroller) Name() string {
	return "vqa-controller"
}

func (v *vqacontroller) Initialize(opt *framework.ControllerOption) error {
	v.vcClient = opt.VolcanoClient
	v.kubeClient = opt.KubeClient
	factory := informerfactory.NewSharedInformerFactory(v.vcClient, 0)
	vqaInformer := factory.Autoscaling().V1alpha1().VerticalQueueAutoscalers()
	queueInformer := factory.Scheduling().V1beta1().Queues()

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: v.kubeClient.CoreV1().Events("")})

	v.vqaInformer = vqaInformer
	v.vqaLister = vqaInformer.Lister()
	v.vqaSynced = vqaInformer.Informer().HasSynced

	v.queueLister = queueInformer.Lister()
	//v.queueSynced = queueInformer.Informer().HasSynced

	v.tidalQueue = workqueue.NewNamedDelayingQueue("verticalqueueautoscaler")
	v.recorder = eventBroadcaster.NewRecorder(versionedscheme.Scheme, v1.EventSource{Component: "vc-controller-manager"})
	v.maxTidalRequeueNum = opt.MaxRequeueNum
	if v.maxTidalRequeueNum <= 0 {
		v.maxTidalRequeueNum = VQAMaxRequeueNum
	}

	vqaInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    v.addVQA,
		UpdateFunc: v.updateVQA,
		DeleteFunc: v.deleteVQA,
	})

	v.enqueueVQA = v.enqueue
	v.enqueueVQAAfter = v.enqueueAfter

	v.now = time.Now

	return nil
}

func (v *vqacontroller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer v.tidalQueue.ShutDown()

	klog.Infof("Starting vqa controller.")
	defer klog.Infof("Shutting down vqa controller.")

	go v.vqaInformer.Informer().Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, v.vqaSynced) {
		klog.Errorf("unable to sync caches for vqa controller.")
		return
	}

	go wait.Until(v.worker, 0, stopCh)
	klog.Infof("VQA PodgroupController is running ...... ")
	<-stopCh
}

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done. You may run as many of these in parallel as you wish; the
// workqueue guarantees that they will not end up processing the same `queue`
// at the same time.
func (v *vqacontroller) worker() {
	for v.processNextWorkItem() {

	}
}

func (v *vqacontroller) processNextWorkItem() bool {
	obj, shutdown := v.tidalQueue.Get()
	if shutdown {
		return false
	}
	defer v.tidalQueue.Done(obj)

	req, ok := obj.(*apis.Request)
	if !ok {
		klog.Errorf("%v is not a valid vertical queue autoscaler request struct.", obj)
		return true
	}

	err := v.reconcileRequest(req)
	if err != nil {
		utilruntime.HandleError(err)
	}

	return true
}

func (v *vqacontroller) checkEnqueue(req *apis.Request, vqa *autoscalingv1alpha1.VerticalQueueAutoscaler) {
	if vqa.Spec.Type == autoscalingv1alpha1.VerticalQueueAutoscalerMetricsType || req.VQARetryTime < v.maxTidalRequeueNum {
		req.VQARetryTime++
		v.enqueueAfter(req, VQARetryDuration)
	}
}

func (v *vqacontroller) reconcileRequest(req *apis.Request) (err error) {
	vqa, err := v.vqaLister.Get(req.VQAName)
	if errors.IsNotFound(err) {
		klog.Warningf("Vertical Queue Autoscaler %s has been deleted ", req.VQAName)
		return nil
	}
	if err != nil {
		return err
	}
	_, err = v.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), req.QueueName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		klog.Warningf("Queue %s has been deleted ", req.QueueName)
		return nil
	}
	if err != nil {
		// need checkEnqueue
		v.checkEnqueue(req, vqa)
		return err
	}
	if vqa.Spec.Type == autoscalingv1alpha1.VerticalQueueAutoscalerTidalType {
		err := v.reconcileTidal(req, vqa)
		if err != nil {
			metrics.UpdateVqaFailMetrics(vqa.Name, vqa.Labels[metrics.TenantKey])
			v.checkEnqueue(req, vqa)
		} else {
			metrics.UpdateQueuePendingTaskNumber("tm", "tm", "tm", 10000)
			metrics.UpdateVqaSuccessMetrics(vqa.Name, vqa.Labels[metrics.TenantKey])
		}
		return err
	} else {
		return v.reconcileMetrics(req, vqa)
	}
}

// 1. find the last tidal task that need to be schedule from last schedule time to current time
// 2. schedule the last tidal task if it's existed
// 3. find the next lastest tidal task that need to be schedule after current time
// 4. enqueue the next lastest tidal task in to tidal queue after (next time sub current time)
func (v *vqacontroller) reconcileTidal(req *apis.Request, vqa *autoscalingv1alpha1.VerticalQueueAutoscaler) error {
	defer v.updateStatus(vqa)
	now := v.now()
	nowV1Time := metav1.NewTime(now)
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	randomCount := random.Intn(10)
	// schedule capacity in this reconcile
	var thisScheduleTidal autoscalingv1alpha1.Tidal
	// schedule capacity in this reconcile
	var nextScheduleTidal autoscalingv1alpha1.Tidal
	// schedule time in this reconcile
	var thisScheduleWaterline int64 = 1<<63 - 1 //-1 << 63
	// next schedule time
	var nextScheduleWaterline int64 = 1<<63 - 1
	for _, tidal := range vqa.Spec.TidalSpec.Tidal {
		// random milliseconds for '*/1 * * * *' and '*/2 * * * *'
		quartzParser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		sched, err := quartzParser.Parse(formatSchedule(vqa, tidal.Schedule, v.recorder))
		if err != nil {
			// this is likely a user error in defining the spec value
			klog.V(0).ErrorS(err, "Unparseable schedule for vqa", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue, "tidal", tidal.Schedule)
			v.recorder.Eventf(vqa, v1.EventTypeWarning, "UnParseableVQASchedule", "Unparseable schedule expression: %s", tidal.Schedule)
			// but may be next schedule is normal
			continue
		}
		scheduledTime, err := getNextScheduleTime(vqa, now, sched, v.recorder)
		if err != nil {
			// this is likely a user error in defining the spec value
			// we should log the error and not reconcile this cronjob until an update to spec
			klog.V(0).ErrorS(err, "Find next schedule time failed", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue, "tidal", tidal.Schedule)
			v.recorder.Eventf(vqa, v1.EventTypeWarning, "FindNextScheduleTimeError", "Find next schedule time failed: %q : %s", tidal.Schedule, err)
			// may be next schedule is normal
			continue
		}
		if scheduledTime != nil {
			tooLate := false
			if vqa.Spec.TidalSpec.StartingDeadlineSeconds != nil {
				tooLate = scheduledTime.Add(time.Second * time.Duration(*vqa.Spec.TidalSpec.StartingDeadlineSeconds)).Before(now)
			}
			if tooLate {
				klog.V(4).InfoS("Missed starting window", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue, "tidal", tidal.Schedule)
				v.recorder.Eventf(vqa, v1.EventTypeWarning, "MissSchedule", "Missed scheduled time to start a job: %s", scheduledTime.UTC().Format(time.RFC1123Z))
				continue
			}
			thisScheduleDuration := now.Sub(*scheduledTime)
			// find lastest scheduler time before now
			if (thisScheduleDuration.Milliseconds() + int64(randomCount)) < thisScheduleWaterline {
				thisScheduleWaterline = thisScheduleDuration.Milliseconds() + int64(randomCount)
				thisScheduleTidal = tidal
			}
			klog.V(2).InfoS("schedule time", "tidal", tidal.Schedule, "thisScheduleDuration", thisScheduleDuration.Milliseconds()+int64(randomCount))
		} else {
			klog.V(1).InfoS("No tidal meet in this reconcile", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue)
		}

		// The only time this should happen is if queue is filled after restart.
		// Otherwise, the queue is always suppose to trigger sync function at the time of
		// the scheduled time, that will give atleast 1 unmet time schedule
		nextScheduleDuration := nextScheduledTimeDuration(vqa, sched, now)
		// find MostRecentScheduleTime
		if nextScheduleDuration.Milliseconds() < nextScheduleWaterline {
			nextScheduleWaterline = nextScheduleDuration.Milliseconds()
			nextScheduleTidal = tidal
		}
	}

	// if last tidal task existed, we need scale the queue
	if thisScheduleWaterline != 1<<63-1 {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			queue, err := v.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), vqa.Spec.Queue, metav1.GetOptions{})
			oldqueue := queue.DeepCopy()
			if err != nil {
				if errors.IsNotFound(err) {
					klog.V(0).ErrorS(err, "queue not found", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue)
					v.recorder.Eventf(vqa, v1.EventTypeWarning, "QueueNotFound", "queue not found: %s", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue)
					return nil
				}
				return err
			}
			queue.Spec.Capability = thisScheduleTidal.Capability
			queue.Spec.Guarantee.Resource = thisScheduleTidal.Guarantee
			queue.Spec.Weight = thisScheduleTidal.Weight
			_, err = v.vcClient.SchedulingV1beta1().Queues().Update(context.TODO(), queue, metav1.UpdateOptions{})
			if err == nil {
				vqa.Status.LastScaleTime = &nowV1Time
				vqa.Status.LastScaleSuccessTime = &nowV1Time
				klog.V(0).InfoS("scale queue success", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue,
					"originalCapacity", util.V1ResourceListToString(oldqueue.Spec.Capability), "originalGuarantee", util.V1ResourceListToString(oldqueue.Spec.Guarantee.Resource),
					"originalWeight", oldqueue.Spec.Weight, "currentCapacity", util.V1ResourceListToString(queue.Spec.Capability),
					"currentGuarantee", util.V1ResourceListToString(queue.Spec.Guarantee.Resource), "currentWeight", queue.Spec.Weight)
				v.recorder.Eventf(vqa, v1.EventTypeNormal, "ScaleQueueSuccess", "originalCapacity: %s; originalGuarantee: %s; "+
					"originalWeight: %+v; currentCapacity: %s; currentGuarantee: %s; currentWeight: %+v;", util.V1ResourceListToString(oldqueue.Spec.Capability),
					util.V1ResourceListToString(oldqueue.Spec.Guarantee.Resource), oldqueue.Spec.Weight, util.V1ResourceListToString(queue.Spec.Capability),
					util.V1ResourceListToString(queue.Spec.Guarantee.Resource), queue.Spec.Weight)
				v.recorder.Eventf(queue, v1.EventTypeNormal, "QueueScaleSuccess", "VQA: %s; originalCapacity: %s; originalGuarantee: %s; "+
					"originalWeight: %+v; currentCapacity: %s; currentGuarantee: %s; currentWeight: %+v;", vqa.GetName(), util.V1ResourceListToString(oldqueue.Spec.Capability),
					util.V1ResourceListToString(oldqueue.Spec.Guarantee.Resource), oldqueue.Spec.Weight, util.V1ResourceListToString(queue.Spec.Capability),
					util.V1ResourceListToString(queue.Spec.Guarantee.Resource), queue.Spec.Weight)
				return nil
			}
			return err
		})
		if err != nil {
			vqa.Status.LastScaleTime = &nowV1Time
			klog.V(0).ErrorS(err, "scale queue failed", vqa.GetName(), "queue", vqa.Spec.Queue)
			v.recorder.Eventf(vqa, v1.EventTypeWarning, "ScaleQueueFailed", "error: %s;", err.Error())
		}
	}
	if nextScheduleWaterline != 1<<63-1 {
		klog.V(0).InfoS("schedule after", "vqa", vqa.GetName(), "queue", vqa.Spec.Queue,
			"tidal schedule cron", nextScheduleTidal.Schedule, "Duration", time.Duration(nextScheduleWaterline)*time.Millisecond)
		v.enqueueAfter(req, time.Duration(nextScheduleWaterline)*time.Millisecond)
	}
	return nil
}

func (v *vqacontroller) reconcileMetrics(req *apis.Request, vqa *autoscalingv1alpha1.VerticalQueueAutoscaler) error {
	klog.Warningln("Metrics Type is not support now!")
	return nil
}
