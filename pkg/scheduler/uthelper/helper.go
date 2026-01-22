/*
Copyright 2024 The Volcano Authors.

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

package uthelper

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	fakek8s "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"volcano.sh/apis/pkg/apis/scheduling"
	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	metrics.InitKubeSchedulerRelatedMetrics()
}

// RegisterPlugins plugins
func RegisterPlugins(plugins map[string]framework.PluginBuilder) {
	for name, plugin := range plugins {
		framework.RegisterPluginBuilder(name, plugin)
	}
}

// TestCommonStruct is the most common used resource when do UT
// others can wrap it in a new struct
type TestCommonStruct struct {
	// Name test case name
	Name string
	// Plugins plugins for each case
	Plugins map[string]framework.PluginBuilder
	// Resource objects that need to be added to schedulercache
	Pods                      []*v1.Pod
	Nodes                     []*v1.Node
	HyperNodesSetByTier       map[int]sets.Set[string]
	HyperNodes                map[string]sets.Set[string]
	HyperNodesMap             map[string]*api.HyperNodeInfo
	HyperNodesTierNameMap     api.HyperNodeTierNameMap
	RealNodesList             map[string][]*api.NodeInfo
	HyperNodesReadyToSchedule bool
	PodGroups                 []*vcapisv1.PodGroup
	Queues                    []*vcapisv1.Queue
	PriClass                  []*schedulingv1.PriorityClass
	ResourceQuotas            []*v1.ResourceQuota
	// IgnoreProvisioners is the provisioners that need to be ignored
	IgnoreProvisioners sets.Set[string]
	PVs                []*v1.PersistentVolume
	PVCs               []*v1.PersistentVolumeClaim
	SCs                []*storagev1.StorageClass
	// DRA related resources
	ResourceSlices []*resourcev1.ResourceSlice
	DeviceClasses  []*resourcev1.DeviceClass
	ResourceClaims []*resourcev1.ResourceClaim
	// ExpectBindMap the expected bind results.
	// bind results: ns/podName -> nodeName
	ExpectBindMap map[string]string
	// ExpectPipeLined the expected pipelined results.
	// pipelined results: map[jobID][]{nodeName}
	ExpectPipeLined map[string][]string
	// ExpectEvicted the expected evicted results.
	// evicted pods list of ns/podName
	ExpectEvicted []string
	// ExpectStatus the expected final podgroup status.
	ExpectStatus map[api.JobID]scheduling.PodGroupPhase
	// ExpectTaskStatusNums represents the expected number map of various TaskStatuses in podgroup
	ExpectTaskStatusNums map[api.JobID]map[schedulingapi.TaskStatus]int
	// ExpectBindsNum the expected bind events numbers.
	ExpectBindsNum int
	// ExpectEvictNum the expected evict events numbers, include preempted and reclaimed evict events
	ExpectEvictNum int

	ExpectBindNumsInHyperNode []int

	// MinimalBindCheck true will only check both bind num, false by default.
	MinimalBindCheck bool

	// CacheSyncTimeout is the timeout for waiting for storage cache sync.
	// If not set, defaults to 3 seconds.
	CacheSyncTimeout time.Duration
	T                *testing.T

	ssn            *framework.Session // store opened session
	cache          *cache.SchedulerCache
	tiers          []conf.Tier
	configurations []conf.Configuration
	stop           chan struct{}
	binder         cache.Binder
	evictor        cache.Evictor
	stsUpdator     cache.StatusUpdater
}

var _ Interface = &TestCommonStruct{}

// RegisterSession open session with tiers and configuration, and mock schedulerCache with self-defined FakeBinder and FakeEvictor
func (test *TestCommonStruct) RegisterSession(tiers []conf.Tier, config []conf.Configuration) *framework.Session {
	test.cache = test.createSchedulerCache()
	test.waitForCacheSyncPolling(test.cache)
	RegisterPlugins(test.Plugins)
	test.tiers = tiers
	test.configurations = config
	test.ssn = framework.OpenSession(test.cache, tiers, config)

	time.Sleep(200 * time.Millisecond)

	return test.ssn
}

func (test *TestCommonStruct) waitForCacheSyncPolling(schedulerCache *cache.SchedulerCache) {
	// Wait for all nodes and pods to be synced into the cache with their correct status
	timeout := 2 * time.Second
	if test.CacheSyncTimeout > 0 {
		timeout = test.CacheSyncTimeout
	}

	err := wait.PollImmediate(10*time.Millisecond, timeout, func() (bool, error) {
		schedulerCache.Mutex.Lock()
		defer schedulerCache.Mutex.Unlock()

		for _, node := range test.Nodes {
			cacheNode, ok := schedulerCache.Nodes[node.Name]
			if !ok {
				return false, nil
			}
			if cacheNode.Node == nil {
				return false, nil
			}
			if len(cacheNode.Node.Status.Allocatable) == 0 && len(node.Status.Allocatable) > 0 {
				return false, nil
			}
		}

		for _, pod := range test.Pods {
			if pod.UID == "" {
				pod.UID = types.UID(pod.Namespace + "/" + pod.Name)
			}
			ti := schedulingapi.NewTaskInfo(pod)
			job, ok := schedulerCache.Jobs[ti.Job]
			if !ok {
				return false, nil
			}
			found := false
			for _, t := range job.Tasks {
				if t.Name == pod.Name && t.Namespace == pod.Namespace {
					if pod.Status.Phase == v1.PodRunning && t.Status != schedulingapi.Running {
						return false, nil
					}
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}

		for _, sc := range test.SCs {
			_, err := schedulerCache.SharedInformerFactory().Storage().V1().StorageClasses().Lister().Get(sc.Name)
			if err != nil {
				return false, nil
			}
		}
		for _, pv := range test.PVs {
			_, err := schedulerCache.SharedInformerFactory().Core().V1().PersistentVolumes().Lister().Get(pv.Name)
			if err != nil {
				return false, nil
			}
		}
		for _, pvc := range test.PVCs {
			_, err := schedulerCache.SharedInformerFactory().Core().V1().PersistentVolumeClaims().Lister().PersistentVolumeClaims(pvc.Namespace).Get(pvc.Name)
			if err != nil {
				return false, nil
			}
		}

		for _, dc := range test.DeviceClasses {
			_, err := schedulerCache.SharedInformerFactory().Resource().V1().DeviceClasses().Lister().Get(dc.Name)
			if err != nil {
				return false, nil
			}
		}
		for _, rc := range test.ResourceClaims {
			_, err := schedulerCache.SharedInformerFactory().Resource().V1().ResourceClaims().Lister().ResourceClaims(rc.Namespace).Get(rc.Name)
			if err != nil {
				return false, nil
			}
		}
		for _, rs := range test.ResourceSlices {
			_, err := schedulerCache.SharedInformerFactory().Resource().V1().ResourceSlices().Lister().Get(rs.Name)
			if err != nil {
				return false, nil
			}
		}

		return true, nil
	})

	if err != nil {
		if test.T != nil {
			test.T.Errorf("Error: cache sync polling timed out: %v", err)
		} else {
			fmt.Printf("Error: cache sync polling timed out: %v\n", err)
		}
		schedulerCache.Mutex.Lock()
		defer schedulerCache.Mutex.Unlock()
		fmt.Printf("Current Cache Nodes: %v\n", len(schedulerCache.Nodes))
		for name, n := range schedulerCache.Nodes {
			fmt.Printf("Node %s: object=%v\n", name, n.Node != nil)
		}
		fmt.Printf("Current Cache Jobs: %v\n", len(schedulerCache.Jobs))
		for id, j := range schedulerCache.Jobs {
			fmt.Printf("Job %s: tasks=%d\n", id, len(j.Tasks))
			for _, t := range j.Tasks {
				fmt.Printf("  Task %s: status=%v\n", t.Name, t.Status)
			}
		}
	}
}

// createSchedulerCache create scheduler cache
func (test *TestCommonStruct) createSchedulerCache() *cache.SchedulerCache {
	binder := util.NewFakeBinder(0)
	evictor := util.NewFakeEvictor(0)
	test.stsUpdator = &util.FakeStatusUpdater{}
	test.binder = binder
	test.evictor = evictor
	test.stop = make(chan struct{})
	// Create scheduler cache with self-defined binder and evictor
	schedulerCache := cache.NewCustomMockSchedulerCache("utmock-scheduler", binder, evictor, test.stsUpdator, nil, nil)

	schedulerCache.IgnoredCSIProvisioners = test.IgnoreProvisioners

	ready := new(atomic.Bool)
	ready.Store(true)
	for _, hni := range test.HyperNodesMap {
		if hni.HyperNode == nil {
			continue
		}
		for _, member := range hni.HyperNode.Spec.Members {
			if member.Type != topologyv1alpha1.MemberTypeHyperNode {
				continue
			}
			if member.Selector.ExactMatch == nil {
				continue
			}
			child := member.Selector.ExactMatch.Name
			hni.Children.Insert(child)
			if childInfo, found := test.HyperNodesMap[child]; found {
				childInfo.Parent = hni.Name
			}
		}
	}
	schedulerCache.HyperNodesInfo = schedulingapi.NewHyperNodesInfoWithCache(test.HyperNodesMap, test.HyperNodesSetByTier, test.HyperNodes, ready)

	// Initial provisioning resources
	kubeClient := schedulerCache.Client()
	for _, sc := range test.SCs {
		kubeClient.StorageV1().StorageClasses().Create(context.Background(), sc, metav1.CreateOptions{})
	}

	fakeClient := kubeClient.(*fakek8s.Clientset)

	fakeClient.PrependReactor("update", "persistentvolumes", func(action ktesting.Action) (bool, runtime.Object, error) {
		updateAction := action.(ktesting.UpdateAction)
		return true, updateAction.GetObject().(*v1.PersistentVolume).DeepCopy(), nil
	})
	fakeClient.PrependReactor("update", "persistentvolumeclaims", func(action ktesting.Action) (bool, runtime.Object, error) {
		updateAction := action.(ktesting.UpdateAction)
		return true, updateAction.GetObject().(*v1.PersistentVolumeClaim).DeepCopy(), nil
	})

	for _, pv := range test.PVs {
		pvCopy := pv.DeepCopy()
		if pvCopy.UID == "" {
			pvCopy.UID = types.UID(pvCopy.Name)
		}
		p, err := kubeClient.CoreV1().PersistentVolumes().Create(context.Background(), pvCopy, metav1.CreateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error creating PV %s: %v", pvCopy.Name, err)
			} else {
				fmt.Printf("Error creating PV %s: %v\n", pvCopy.Name, err)
			}
		}
		p.Status = pvCopy.Status
		_, err = kubeClient.CoreV1().PersistentVolumes().UpdateStatus(context.Background(), p, metav1.UpdateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error updating status of PV %s: %v", pvCopy.Name, err)
			} else {
				fmt.Printf("Error updating status of PV %s: %v\n", pvCopy.Name, err)
			}
		}
	}
	for _, pvc := range test.PVCs {
		pvcCopy := pvc.DeepCopy()
		if pvcCopy.UID == "" {
			pvcCopy.UID = types.UID(pvcCopy.Name)
		}
		p, err := kubeClient.CoreV1().PersistentVolumeClaims(pvcCopy.Namespace).Create(context.Background(), pvcCopy, metav1.CreateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error creating PVC %s: %v", pvcCopy.Name, err)
			} else {
				fmt.Printf("Error creating PVC %s: %v\n", pvcCopy.Name, err)
			}
		}
		p.Status = pvcCopy.Status
		_, err = kubeClient.CoreV1().PersistentVolumeClaims(pvcCopy.Namespace).UpdateStatus(context.Background(), p, metav1.UpdateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error updating status of PVC %s: %v", pvcCopy.Name, err)
			} else {
				fmt.Printf("Error updating status of PVC %s: %v\n", pvcCopy.Name, err)
			}
		}
	}
	for _, dc := range test.DeviceClasses {
		kubeClient.ResourceV1().DeviceClasses().Create(context.Background(), dc, metav1.CreateOptions{})
	}
	for _, rc := range test.ResourceClaims {
		kubeClient.ResourceV1().ResourceClaims(rc.Namespace).Create(context.Background(), rc, metav1.CreateOptions{})
	}
	for _, rs := range test.ResourceSlices {
		kubeClient.ResourceV1().ResourceSlices().Create(context.Background(), rs, metav1.CreateOptions{})
	}

	for _, node := range test.Nodes {
		n, err := kubeClient.CoreV1().Nodes().Create(context.Background(), node.DeepCopy(), metav1.CreateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error creating Node %s: %v", node.Name, err)
			} else {
				fmt.Printf("Error creating Node %s: %v\n", node.Name, err)
			}
		}
		n.Status = node.Status
		_, err = kubeClient.CoreV1().Nodes().UpdateStatus(context.Background(), n, metav1.UpdateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error updating status of Node %s: %v", node.Name, err)
			} else {
				fmt.Printf("Error updating status of Node %s: %v\n", node.Name, err)
			}
		}
	}
	for _, pg := range test.PodGroups {
		schedulerCache.AddPodGroupV1beta1(pg)
	}
	for _, queue := range test.Queues {
		schedulerCache.AddQueueV1beta1(queue)
	}
	for _, pc := range test.PriClass {
		schedulerCache.AddPriorityClass(pc)
	}
	for _, rq := range test.ResourceQuotas {
		schedulerCache.AddResourceQuota(rq)
	}

	schedulerCache.Run(test.stop)
	schedulerCache.WaitForCacheSync(test.stop)

	for _, pod := range test.Pods {
		if pod.Spec.SchedulerName == "" {
			pod.Spec.SchedulerName = "utmock-scheduler"
		}
		if pod.UID == "" {
			pod.UID = types.UID(pod.Namespace + "/" + pod.Name)
		}
		p, err := kubeClient.CoreV1().Pods(pod.Namespace).Create(context.Background(), pod, metav1.CreateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error creating Pod %s: %v", pod.Name, err)
			} else {
				fmt.Printf("Error creating Pod %s: %v\n", pod.Name, err)
			}
		}
		p.Status = pod.Status
		_, err = kubeClient.CoreV1().Pods(pod.Namespace).UpdateStatus(context.Background(), p, metav1.UpdateOptions{})
		if err != nil {
			if test.T != nil {
				test.T.Errorf("Error updating status of Pod %s: %v", pod.Name, err)
			} else {
				fmt.Printf("Error updating status of Pod %s: %v\n", pod.Name, err)
			}
		}
	}

	return schedulerCache
}

// Run choose to run passed in actions; if no actions provided, will panic
func (test *TestCommonStruct) Run(actions []framework.Action) {
	if len(actions) == 0 {
		panic("no actions provided, please specify a list of actions to execute")
	}

	// registry actions in conf variables
	conf.EnabledActionMap = make(map[string]bool, len(actions))
	for _, action := range actions {
		conf.EnabledActionMap[action.Name()] = true
	}

	for _, action := range actions {
		action.Initialize()
		action.Execute(test.ssn)
		action.UnInitialize()
	}
}

// Close do release resource and clean up
func (test *TestCommonStruct) Close() {
	framework.CloseSession(test.ssn)
	framework.CleanupPluginBuilders()
	close(test.stop)
}

// CheckAll checks all the need status
func (test *TestCommonStruct) CheckAll(caseIndex int) (err error) {
	// update all jobs' status
	ju := framework.NewJobUpdater(test.ssn)
	ju.UpdateAll()

	if err = test.CheckBind(caseIndex); err != nil {
		return
	}
	if err = test.CheckEvict(caseIndex); err != nil {
		return
	}
	if err = test.CheckPipelined(caseIndex); err != nil {
		return
	}
	if err = test.CheckTaskStatusNums(caseIndex); err != nil {
		return
	}
	return test.CheckPGStatus(caseIndex)
}

// CheckBind check expected bind result
func (test *TestCommonStruct) CheckBind(caseIndex int) error {
	if test.ExpectBindsNum != len(test.ExpectBindMap) && !test.MinimalBindCheck {
		return fmt.Errorf("invalid setting for binding check: want bind count %d, want bind result length %d", test.ExpectBindsNum, len(test.ExpectBindMap))
	}

	binder := test.binder.(*util.FakeBinder)
	for i := 0; i < test.ExpectBindsNum; i++ {
		select {
		case <-binder.Channel:
		case <-time.After(300 * time.Millisecond):
			return fmt.Errorf("failed to get Bind request in case %d(%s)", caseIndex, test.Name)
		}
	}

	// in case expected test.BindsNum is 0, but actually there is a binding and wait the binding goroutine to run
	select {
	case <-time.After(300 * time.Millisecond):
	case key := <-binder.Channel:
		return fmt.Errorf("unexpect binding %s in case %d(%s)", key, caseIndex, test.Name)
	}

	if test.MinimalBindCheck {
		return nil
	}

	binds := binder.Binds()
	if len(test.ExpectBindMap) != len(binds) {
		return fmt.Errorf("case %d(%s) check bind: \nwant: %v\n got %v ", caseIndex, test.Name, test.ExpectBindMap, binds)
	}
	for key, value := range test.ExpectBindMap {
		got := binds[key]
		if value != got {
			return fmt.Errorf("case %d(%s)  check bind: \nwant: %v->%v\n got: %v->%v ", caseIndex, test.Name, key, value, key, got)
		}
	}
	return nil
}

// CheckEvict check the evicted result
func (test *TestCommonStruct) CheckEvict(caseIndex int) error {
	if test.ExpectEvictNum != len(test.ExpectEvicted) {
		return fmt.Errorf("invalid setting for evicting check: want evict count %d, want evict result length %d", test.ExpectEvictNum, len(test.ExpectEvicted))
	}
	evictor := test.evictor.(*util.FakeEvictor)
	for i := 0; i < test.ExpectEvictNum; i++ {
		select {
		case <-evictor.Channel:
		case <-time.After(300 * time.Millisecond):
			return fmt.Errorf("failed to get Evict request in case %d(%s)", caseIndex, test.Name)
		}
	}

	// in case expected test.EvictNum is 0, but actually there is an evicting and wait the evicting goroutine to run
	select {
	case <-time.After(50 * time.Millisecond):
	case key := <-evictor.Channel:
		return fmt.Errorf("unexpect evicted %s in case %d(%s)", key, caseIndex, test.Name)
	}

	evicts := evictor.Evicts()
	if len(test.ExpectEvicted) != len(evicts) {
		return fmt.Errorf("case %d(%s) check evict: \nwant: %v\n got %v ", caseIndex, test.Name, test.ExpectEvicted, evicts)
	}

	expect := map[string]int{} // evicted number
	got := map[string]int{}
	for _, v := range test.ExpectEvicted {
		expect[v]++
	}
	for _, v := range evicts {
		got[v]++
	}

	if !equality.Semantic.DeepEqual(expect, got) {
		return fmt.Errorf("case %d(%s) check evict: \nwant: %v\n got: %v ", caseIndex, test.Name, expect, got)
	}
	return nil
}

func (test *TestCommonStruct) CheckTaskStatusNums(caseIndex int) error {
	ssn := test.ssn
	for jobID, taskStatusMap := range test.ExpectTaskStatusNums {
		job := ssn.Jobs[jobID]
		if job == nil {
			return fmt.Errorf("case %d(%s) check podgroup status, job <%v> doesn't exist in session", caseIndex, test.Name, jobID)
		}
		for status, expectNum := range taskStatusMap {
			if expectNum != len(job.TaskStatusIndex[status]) {
				return fmt.Errorf("case %d(%s) check podgroup <%v> task status %v: want %d, got %d", caseIndex, test.Name, jobID, status, expectNum, len(job.TaskStatusIndex[status]))
			}
		}
	}
	return nil
}

// CheckPGStatus check job's podgroups status
func (test *TestCommonStruct) CheckPGStatus(caseIndex int) error {
	ssn := test.ssn
	for jobID, phase := range test.ExpectStatus {
		job := ssn.Jobs[jobID]
		if job == nil {
			return fmt.Errorf("case %d(%s) check podgroup status, job <%v> doesn't exist in session", caseIndex, test.Name, jobID)
		}
		got := job.PodGroup.Status.Phase
		if phase != got {
			return fmt.Errorf("case %d(%s) check podgroup <%v> status:\n want: %v, got: %v", caseIndex, test.Name, jobID, phase, got)
		}
	}
	return nil
}

// CheckPipelined checks pipeline results
func (test *TestCommonStruct) CheckPipelined(caseIndex int) error {
	ssn := test.ssn
	for jobID, nodes := range test.ExpectPipeLined {
		job := ssn.Jobs[api.JobID(jobID)]
		if job == nil {
			return fmt.Errorf("case %d(%s) check pipeline, job <%v> doesn't exist in session", caseIndex, test.Name, jobID)
		}
		pipeLined := job.TaskStatusIndex[api.Pipelined]
		if len(pipeLined) == 0 {
			return fmt.Errorf("case %d(%s) check pipeline, want pipelined job: %v, actually, no tasks pipelined to nodes %v", caseIndex, test.Name, jobID, nodes)
		}
		for _, task := range pipeLined {
			if !Contains(nodes, task.NodeName) {
				return fmt.Errorf("case %d(%s) check pipeline: actual: %v->%v, want: %v->%v", caseIndex, test.Name, task.Name, task.NodeName, task.Name, nodes)
			}
		}
	}
	return nil
}

// CheckBind check expected bind result
func (test *TestCommonStruct) CheckBindInHyperNode(caseIndex int) error {
	binder := test.binder.(*util.FakeBinder)
	for i := 0; i < test.ExpectBindsNum; i++ {
		select {
		case <-binder.Channel:
		case <-time.After(300 * time.Millisecond):
			return fmt.Errorf("failed to get Bind request in case %d(%s)", caseIndex, test.Name)
		}
	}

	// in case expected test.BindsNum is 0, but actually there is a binding and wait the binding goroutine to run
	select {
	case <-time.After(300 * time.Millisecond):
	case key := <-binder.Channel:
		return fmt.Errorf("unexpect binding %s in case %d(%s)", key, caseIndex, test.Name)
	}

	if test.MinimalBindCheck {
		return nil
	}

	binds := binder.Binds()

	if test.ExpectBindsNum != len(binds) {
		return fmt.Errorf("invalid setting for binding check: want bind count %d, want bind result length %d", test.ExpectBindsNum, len(binds))
	}

	realHyperNode := make(map[string]int, 0)
	for _, node := range binds {
		hyperNode := util.FindHyperNodeForNode(node, test.ssn.RealNodesList, test.ssn.HyperNodesTiers, test.ssn.HyperNodesSetByTier)
		realHyperNode[hyperNode] += 1
	}

	actualBindNums := make([]int, 0, len(realHyperNode))
	for _, value := range realHyperNode {
		actualBindNums = append(actualBindNums, value)
	}
	sort.Ints(actualBindNums)

	if !slices.Equal(actualBindNums, test.ExpectBindNumsInHyperNode) {
		return fmt.Errorf("case %d(%s) check bind: \nwant: %v\n got: %v ", caseIndex, test.Name, test.ExpectBindNumsInHyperNode, actualBindNums)
	}

	return nil
}
