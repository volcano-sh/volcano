package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	kubev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	kuberesource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	schedcachemock "volcano.sh/volcano/pkg/scheduler/cache/cachemock"
)

const (
	schedulerConf = `
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: overcommit
  - name: drf
  - name: predicates
    arguments:
      predicate.PodTopologySpreadEnable: false
      predicate.RegisteredDevices: []
  - name: proportion
  - name: nodeorder
    arguments:
      balancedresource.weight: 0
`
	schedulePeriod = time.Second
)

var (
	defaultNamespace             = "default"
	defaultPodGroup              = "default"
	defaultQueue                 = "default"
	defaultNodeCPUCapability     = strconv.Itoa(96*1000) + "m"
	defaultNodeMemCapability     = strconv.Itoa(256*1024) + "Mi"
	defaultNodePodCapability     = strconv.Itoa(100)
	defaultNodeStorageCapability = strconv.Itoa(1600*1024*1024) + "Mi"
	defaultPodCPURequest         = strconv.Itoa(4*1000) + "m"
	defaultPodMemRequest         = strconv.Itoa(8*1024) + "Mi"
)

// a copy from pkg/scheduler/cache/cache.go
type imageState struct {
	// Size of the image
	size int64
	// A set of node names for nodes having this image present
	nodes sets.String
}

// schedulerCacheMock is used as a cache directly provided to the scheduler, offering several methods for setting and clearing data during scheduler runtime.
type schedulerCacheMock struct {
	*schedcachemock.MockCache

	kubeClient           kubernetes.Interface
	informer             informers.SharedInformerFactory
	Mutex                sync.Mutex
	ClusterInfo          *schedulingapi.ClusterInfo
	defaultPriorityClass *schedulingv1.PriorityClass
	defaultPriority      int32
	NamespaceCollection  map[string]*schedulingapi.NamespaceCollection
	errTasks             workqueue.RateLimitingInterface
	DeletedJobs          workqueue.RateLimitingInterface
	BindFlowChannel      chan *schedulingapi.TaskInfo
	bindCache            []*schedulingapi.TaskInfo
	batchNum             int
	imageStates          map[string]*imageState
}

func newSchedulerCacheMock(t *testing.T) *schedulerCacheMock {
	mockCtl := gomock.NewController(&testing.T{})
	cache := schedcachemock.NewMockCache(mockCtl)

	sc := &schedulerCacheMock{
		ClusterInfo: &schedulingapi.ClusterInfo{
			Jobs:           make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
			Nodes:          make(map[string]*schedulingapi.NodeInfo),
			Queues:         make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
			NamespaceInfo:  make(map[schedulingapi.NamespaceName]*schedulingapi.NamespaceInfo),
			RevocableNodes: make(map[string]*schedulingapi.NodeInfo),
			CSINodesStatus: make(map[string]*schedulingapi.CSINodeStatusInfo),
		},
		kubeClient: kubefake.NewSimpleClientset(),
	}

	// 初始化cache mock依赖的一些缓存数据

	// 初始化各个cache mock方法的预期输入输出
	cache.EXPECT().Run(gomock.Any())
	cache.EXPECT().Snapshot().Return(sc.ClusterInfo)
	cache.EXPECT().WaitForCacheSync(gomock.Any())

	// AddBindTask只对节点和任务做更新，一旦成功，可以确认调度流程已完成
	cache.EXPECT().AddBindTask(gomock.Any()).Do(func(taskInfo *schedulingapi.TaskInfo) {
		sc.Mutex.Lock()
		defer sc.Mutex.Unlock()

		job := sc.ClusterInfo.Jobs[taskInfo.Job]
		task := job.Tasks[taskInfo.UID]

		originalStatus := task.Status
		if err := job.UpdateTaskStatus(task, schedulingapi.Binding); err != nil {
			t.Errorf("failed to update task status: %v", err)
		}

		node := sc.ClusterInfo.Nodes[taskInfo.NodeName]
		if err := node.AddTask(task); err != nil {
			if err := job.UpdateTaskStatus(task, originalStatus); err != nil {
				t.Errorf("failed to update task status: %v", err)
			}
		}
	}).AnyTimes()

	// Client 返回 mock kubernetes client
	cache.EXPECT().Client().Return(sc.kubeClient).AnyTimes()

	// todo: fix
	informerFactory := informers.NewSharedInformerFactory(sc.kubeClient, 0)
	cache.EXPECT().SharedInformerFactory().Return(informerFactory).AnyTimes()

	// skipped in this case
	cache.EXPECT().UpdateJobStatus(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	cache.EXPECT().RecordJobStatusEvent(gomock.Any(), gomock.Any()).AnyTimes()
	cache.EXPECT().UpdateQueueStatus(gomock.Any()).Return(nil).AnyTimes()
	cache.EXPECT().GetPodVolumes(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	cache.EXPECT().AllocateVolumes(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	cache.EXPECT().BindVolumes(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	cache.EXPECT().RevertVolumes(gomock.Any(), gomock.Any()).AnyTimes()
	cache.EXPECT().UpdateSchedulerNumaInfo(gomock.Any()).Return(nil).AnyTimes()
	cache.EXPECT().EventRecorder().Return(nil).AnyTimes()
	cache.EXPECT().SetMetricsConf(gomock.Any()).AnyTimes()

	// not supported in this case
	cache.EXPECT().Evict(gomock.Any(), gomock.Any()).Return(fmt.Errorf("evict is not supported in this test case")).AnyTimes()

	// not used in code
	cache.EXPECT().BindPodGroup(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	cache.EXPECT().ClientConfig().Return(nil).AnyTimes()

	sc.MockCache = cache

	return sc
}

func getJobID(pg *schedulingapi.PodGroup) schedulingapi.JobID {
	return schedulingapi.JobID(fmt.Sprintf("%s/%s", pg.Namespace, pg.Name))
}

// newJob is used to quickly initialize a test job
func newJob(t *testing.T, queue, name, cpuRequest, memRequest string, n int) *schedulingapi.JobInfo {
	pg := &schedulingapi.PodGroup{
		Version: schedulingapi.PodGroupVersionV1Beta1,
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				UID:       kubetypes.UID(name),
				Name:      name,
				Namespace: defaultNamespace,
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 0,
				Queue:     queue,
			},
		},
	}

	ji := schedulingapi.NewJobInfo(getJobID(pg))
	ji.SetPodGroup(pg)
	for i := 0; i < n; i++ {
		resourceReqeust := kubev1.ResourceList{
			kubev1.ResourceCPU:    kuberesource.MustParse(cpuRequest),
			kubev1.ResourceMemory: kuberesource.MustParse(memRequest),
		}

		podName := fmt.Sprintf("pod-%s-%d", name, i)
		pod := &kubev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:       kubetypes.UID(podName),
				Name:      podName,
				Namespace: defaultNamespace,
				Annotations: map[string]string{
					v1beta1.KubeGroupNameAnnotationKey: name,
				},
			},
			Spec: kubev1.PodSpec{
				Containers: []kubev1.Container{
					{
						Name: "container",
						Resources: kubev1.ResourceRequirements{
							Requests: resourceReqeust,
						},
					},
				},
			},
			Status: kubev1.PodStatus{
				Phase: kubev1.PodPending,
			},
		}

		ji.AddTaskInfo(schedulingapi.NewTaskInfo(pod))
	}

	return ji
}

// addNodes is used to quickly add a test node
func (sc *schedulerCacheMock) addNodes(t *testing.T, n int, cpuCapacity, memCapacity string) {
	for i := len(sc.ClusterInfo.Nodes); i < int(n); i++ {
		nodeName := fmt.Sprintf("node-%d", i)
		resource := kubev1.ResourceList{
			kubev1.ResourceCPU:              kuberesource.MustParse(cpuCapacity),
			kubev1.ResourceMemory:           kuberesource.MustParse(memCapacity),
			kubev1.ResourcePods:             kuberesource.MustParse(defaultNodePodCapability),
			kubev1.ResourceEphemeralStorage: kuberesource.MustParse(defaultNodeStorageCapability),
		}

		// add node to fack client
		kubeNode := kubev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
			Status: kubev1.NodeStatus{
				Allocatable: resource,
				Capacity:    resource,
				Phase:       kubev1.NodeRunning,
			},
		}
		cli := sc.Client()
		if _, err := cli.CoreV1().Nodes().Create(
			context.Background(),
			&kubeNode,
			metav1.CreateOptions{},
		); err != nil {
			t.Errorf("failed to create node: %v", err)
		}

		// add node to cache
		sc.ClusterInfo.Nodes[nodeName] = schedulingapi.NewNodeInfo(&kubeNode)
		sc.ClusterInfo.NodeList = append(sc.ClusterInfo.NodeList, nodeName)
	}
}

// addJobs is used to add a test job
func (sc *schedulerCacheMock) addJobs(t *testing.T, ji *schedulingapi.JobInfo) {
	// add to k8s
	for _, task := range ji.Tasks {
		if _, err := sc.Client().CoreV1().Pods(defaultNamespace).Create(
			context.Background(),
			task.Pod,
			metav1.CreateOptions{},
		); err != nil {
			t.Errorf("failed to create node: %v", err)
		}
	}

	// add to cache
	sc.ClusterInfo.Jobs[ji.UID] = ji
}

// addQueue is used to add a test queue
func (sc *schedulerCacheMock) addQueue(t *testing.T, name string) {
	queue := schedulingapi.NewQueueInfo(&scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			UID:  kubetypes.UID(name),
			Name: name,
		},
		Spec: scheduling.QueueSpec{
			Weight: 1,
		},
	})

	// add to cache
	sc.ClusterInfo.Queues[queue.UID] = queue
}

// clearNodes is used to clear nodes between test cases
func (sc *schedulerCacheMock) clearNodes(t *testing.T) {
	for nodeName := range sc.ClusterInfo.Nodes {
		if err := sc.Client().CoreV1().Nodes().Delete(
			context.Background(),
			nodeName,
			metav1.DeleteOptions{},
		); err != nil {
			t.Error(err)
		}
	}
	sc.ClusterInfo.Nodes = make(map[string]*schedulingapi.NodeInfo)
}

// clearJobs is used to clear nodes between test cases
func (sc *schedulerCacheMock) clearJobs(t *testing.T) {
	for jobName := range sc.ClusterInfo.Jobs {
		for _, task := range sc.ClusterInfo.Jobs[jobName].Tasks {
			if err := sc.Client().CoreV1().Pods(defaultNamespace).Delete(
				context.Background(),
				task.Pod.Name,
				metav1.DeleteOptions{},
			); err != nil {
				t.Error(err)
			}
		}
	}
	sc.ClusterInfo.Jobs = make(map[schedulingapi.JobID]*schedulingapi.JobInfo)
}

type schedulerCase struct {
	nodeNums int
	nodeCPU  string
	nodeMem  string

	podNums int
	podCPU  string
	podMem  string
}

func TestRunOnce(t *testing.T) {
	cases := []schedulerCase{
		{
			nodeNums: 1000,
			nodeCPU:  defaultNodeCPUCapability,
			nodeMem:  defaultNodeMemCapability,

			podNums: 3000,
			podCPU:  defaultPodCPURequest,
			podMem:  defaultPodMemRequest,
		},
	}

	// init config（先保持为空，后续调整）
	options.ServerOpts = &options.ServerOption{}

	sc := newSchedulerCacheMock(t)

	actions, plugins, configurations, metricsConf, err := unmarshalSchedulerConf(schedulerConf)
	if err != nil {
		t.Fatal(err)
	}

	s := &Scheduler{
		cache:          sc.MockCache,
		schedulePeriod: schedulePeriod,
		dumper:         schedcache.Dumper{Cache: sc.MockCache},
		actions:        actions,
		plugins:        plugins,
		configurations: configurations,
		metricsConf:    metricsConf,
	}

	for _, schedulerCase := range cases {
		// gc
		// schedulerCacheMock.clearNodes(t)
		// schedulerCacheMock.clearJobs(t)

		// prepare env
		sc.addQueue(t, defaultQueue)
		sc.addNodes(
			t,
			schedulerCase.nodeNums,
			schedulerCase.nodeCPU,
			schedulerCase.nodeMem,
		)

		// add job
		ji := newJob(
			t,
			defaultQueue,
			defaultPodGroup,
			schedulerCase.podCPU,
			schedulerCase.podMem,
			schedulerCase.podNums,
		)
		sc.addJobs(t, ji)
		t.Logf("%d nodes added", schedulerCase.nodeNums)

		// start to run scheduler and calulate the time
		startTime := time.Now()
		s.runOnce()
		d := time.Since(startTime)
		t.Logf("Total time to allocate %d pods in %v, %f per second",
			schedulerCase.podNums,
			d,
			float64(schedulerCase.podNums)/d.Seconds())
	}
}
