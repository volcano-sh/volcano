package cache

import (
	"math"
	"os"
	"strconv"

	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	fakevcClient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// NewCustomMockSchedulerCache returns a mock scheduler cache with custom interface
func NewCustomMockSchedulerCache(schedulerName string,
	binder Binder,
	evictor Evictor,
	statusUpdater StatusUpdater,
	PodGroupBinder BatchBinder,
	volumeBinder VolumeBinder,
	recorder record.EventRecorder,
) *SchedulerCache {
	msc := newMockSchedulerCache(schedulerName)
	// add all events handlers
	msc.addEventHandler()
	msc.Recorder = recorder
	msc.Binder = binder
	msc.Evictor = evictor
	msc.StatusUpdater = statusUpdater
	msc.PodGroupBinder = PodGroupBinder
	// use custom volume binder
	msc.VolumeBinder = volumeBinder
	checkAndSetDefaultInterface(msc)
	return msc
}

// NewDefaultMockSchedulerCache returns a mock scheduler cache with interface mocked with default fake clients
// Notes that default events recorder's buffer only has a length 100;
// when use it do performance test, should use a &FakeRecorder{} without length limit to avoid block
func NewDefaultMockSchedulerCache(schedulerName string) *SchedulerCache {
	msc := newMockSchedulerCache(schedulerName)
	// add all events handlers
	msc.addEventHandler()
	checkAndSetDefaultInterface(msc)
	return msc
}

func checkAndSetDefaultInterface(sc *SchedulerCache) {
	if sc.Recorder == nil {
		sc.Recorder = record.NewFakeRecorder(100) // to avoid blocking, we can pass in &FakeRecorder{} to NewCustomMockSchedulerCache
	}
	if sc.Binder == nil {
		sc.Binder = &DefaultBinder{
			kubeclient: sc.kubeClient,
			recorder:   sc.Recorder,
		}
	}
	if sc.Evictor == nil {
		sc.Evictor = &defaultEvictor{
			kubeclient: sc.kubeClient,
			recorder:   sc.Recorder,
		}
	}
	if sc.StatusUpdater == nil {
		sc.StatusUpdater = &defaultStatusUpdater{
			kubeclient: sc.kubeClient,
			vcclient:   sc.vcClient,
		}
	}
	if sc.PodGroupBinder == nil {
		sc.PodGroupBinder = &podgroupBinder{
			kubeclient: sc.kubeClient,
			vcclient:   sc.vcClient,
		}
	}
	// finally, init default fake volume binder which has dependencies on other informers
	if sc.VolumeBinder == nil {
		sc.setDefaultVolumeBinder()
	}
}

func getNodeWorkers() uint32 {
	if options.ServerOpts != nil && options.ServerOpts.NodeWorkerThreads > 0 {
		return options.ServerOpts.NodeWorkerThreads
	}
	threads, err := strconv.Atoi(os.Getenv("NODE_WORKER_THREADS"))
	if err == nil && threads > 0 && threads <= math.MaxUint32 {
		return uint32(threads)
	}
	return 2 //default 2
}

// newMockSchedulerCache init the mock scheduler cache structure
func newMockSchedulerCache(schedulerName string) *SchedulerCache {
	msc := &SchedulerCache{
		Jobs:                make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		Nodes:               make(map[string]*schedulingapi.NodeInfo),
		Queues:              make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
		PriorityClasses:     make(map[string]*schedulingv1.PriorityClass),
		errTasks:            workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
		nodeQueue:           workqueue.NewTypedRateLimitingQueue[string](workqueue.DefaultTypedControllerRateLimiter[string]()),
		DeletedJobs:         workqueue.NewTypedRateLimitingQueue[*schedulingapi.JobInfo](workqueue.DefaultTypedControllerRateLimiter[*schedulingapi.JobInfo]()),
		kubeClient:          fake.NewSimpleClientset(),
		vcClient:            fakevcClient.NewSimpleClientset(),
		restConfig:          nil,
		defaultQueue:        "default",
		schedulerNames:      []string{schedulerName},
		nodeSelectorLabels:  make(map[string]sets.Empty),
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),
		CSINodesStatus:      make(map[string]*schedulingapi.CSINodeStatusInfo),
		imageStates:         make(map[string]*imageState),

		NodeList: []string{},
	}
	if options.ServerOpts != nil && len(options.ServerOpts.NodeSelector) > 0 {
		msc.updateNodeSelectors(options.ServerOpts.NodeSelector)
	}
	msc.setBatchBindParallel()
	msc.nodeWorkers = getNodeWorkers()

	return msc
}
