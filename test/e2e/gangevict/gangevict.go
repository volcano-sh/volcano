/*
Copyright 2026 The Volcano Authors.

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

package gangevict

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

// Differences from legacy preempt/reclaim e2e tests:
//
//   - Both gangpreempt and gangreclaim tests use the capacity plugin
//     (not proportion) with DeservedResource set on every test queue so
//     that capacity's PreemptiveFn can approve preemption attempts.
//   - The enqueue action is removed; allocate's fallback auto-transitions
//     PodGroups to Inqueue.
//   - Legacy preempt and reclaim are replaced with gangpreempt and
//     gangreclaim respectively -- no mixed legacy/gang actions.
//   - The capacity plugin is placed in tier 1 alongside priority, gang,
//     and conformance so that its UnifiedEvictableFn participates in the
//     same intersection (tier-walking stops at the first tier that
//     produces a non-nil result).
//   - Negative tests use a 60s poll timeout instead of the default
//     5-minute WaitTasksReady timeout, since preemption/reclaim decisions
//     happen within a few scheduler cycles.

// gangEvictConfig is a complete scheduler configuration shared by both
// gangpreempt and gangreclaim test suites. It uses only gang-aware
// actions (gangpreempt, gangreclaim) with the capacity plugin (flat, no
// hierarchy) in tier 1.
const gangEvictConfig = `actions: "allocate, backfill, gangreclaim, gangpreempt"
tiers:
- plugins:
  - name: priority
  - name: gang
    enablePreemptable: false
  - name: conformance
  - name: sla
  - name: capacity
    enableHierarchy: false
- plugins:
  - name: overcommit
  - name: drf
    enablePreemptable: false
  - name: predicates
    arguments:
      predicate.DynamicResourceAllocationEnable: true
  - name: nodeorder
  - name: binpack
  - name: network-topology-aware
`

// applySchedulerConfig returns a ChangeBy-compatible function that
// replaces the scheduler ConfigMap with the given YAML config.
func applySchedulerConfig(config string) func(map[string]string) (bool, map[string]string) {
	return func(data map[string]string) (bool, map[string]string) {
		const key = "volcano-scheduler-ci.conf"
		old := data[key]
		data[key] = config
		return true, map[string]string{key: old}
	}
}

func createGangJob(ctx *e2eutil.TestContext, name, queue, pri string, req v1.ResourceList, rep, minAvail int32, preemptable bool) *batchv1alpha1.Job {
	labels := map[string]string{}
	if preemptable {
		labels[schedulingv1beta1.PodPreemptable] = "true"
	}
	spec := &e2eutil.JobSpec{
		Name:  name,
		Queue: queue,
		Pri:   pri,
		Min:   minAvail,
		Tasks: []e2eutil.TaskSpec{
			{
				Img:    e2eutil.DefaultNginxImage,
				Req:    req,
				Min:    rep,
				Rep:    rep,
				Labels: labels,
			},
		},
	}
	return e2eutil.CreateJob(ctx, spec)
}

// deservedCPU builds a ResourceList with just CPU (in cores) plus matching memory.
func deservedCPU(cpuCores int64) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    *resource.NewQuantity(cpuCores, resource.DecimalSI),
		v1.ResourceMemory: *resource.NewQuantity(cpuCores*1024*1024*1024, resource.BinarySI),
	}
}

// shortPollTimeout is used for assertions where we expect a condition
// NOT to be met. Preemption/reclaim decisions happen within a few
// scheduler cycles (~10-20s), so 60s gives comfortable margin while
// keeping tests fast.
const shortPollTimeout = 60 * time.Second

// waitTasksNotReady waits for the given duration and asserts that the job
// does NOT reach the requested number of ready tasks.
func waitTasksNotReady(ctx *e2eutil.TestContext, job *batchv1alpha1.Job, taskNum int, reason string) {
	err := wait.Poll(100*time.Millisecond, shortPollTimeout, func() (bool, error) {
		pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		ready := 0
		for _, pod := range pods.Items {
			if !metav1.IsControlledBy(&pod, job) {
				continue
			}
			if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
				ready++
			}
		}
		return ready >= taskNum, nil
	})
	Expect(err).To(HaveOccurred(), fmt.Sprintf("%s: job %q should not have reached %d ready tasks", reason, job.Name, taskNum))
}

// ── topology helpers ───────────────────────────────────────────────────

var kwokTolerations = []v1.Toleration{
	{
		Key:      "kwok.x-k8s.io/node",
		Operator: v1.TolerationOpEqual,
		Value:    "fake",
		Effect:   v1.TaintEffectNoSchedule,
	},
}

// tier1Domains maps each tier-1 HyperNode suffix to its leaf KWOK nodes.
var tier1Domains = map[string][]string{
	"s0": {"kwok-node-0", "kwok-node-1"},
	"s1": {"kwok-node-2", "kwok-node-3"},
}

// setupTopoHyperNodes creates a 2-tier HyperNode tree over kwok-node-0..3.
//
//	         {prefix}-s2 (tier 2)
//	        /                   \
//	  {prefix}-s0 (tier 1)   {prefix}-s1 (tier 1)
//	    /        \              /        \
//	kwok-node-0  -1        kwok-node-2  -3
func setupTopoHyperNodes(ctx *e2eutil.TestContext, prefix string) {
	hyperNodes := []struct {
		name    string
		members []string
		tier    int
	}{
		{prefix + "-s0", []string{"kwok-node-0", "kwok-node-1"}, 1},
		{prefix + "-s1", []string{"kwok-node-2", "kwok-node-3"}, 1},
		{prefix + "-s2", []string{prefix + "-s0", prefix + "-s1"}, 2},
	}

	for _, hn := range hyperNodes {
		memberType := topologyv1alpha1.MemberTypeHyperNode
		if hn.tier == 1 {
			memberType = topologyv1alpha1.MemberTypeNode
		}
		spec := &topologyv1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{Name: hn.name},
			Spec: topologyv1alpha1.HyperNodeSpec{
				Tier: hn.tier,
				Members: []topologyv1alpha1.MemberSpec{
					{
						Type: memberType,
						Selector: topologyv1alpha1.MemberSelector{
							ExactMatch: &topologyv1alpha1.ExactMatch{Name: hn.members[0]},
						},
					},
					{
						Type: memberType,
						Selector: topologyv1alpha1.MemberSelector{
							ExactMatch: &topologyv1alpha1.ExactMatch{Name: hn.members[1]},
						},
					},
				},
			},
		}
		err := e2eutil.SetupHyperNode(ctx, spec)
		Expect(err).NotTo(HaveOccurred())
	}

	By("Waiting for HyperNodes to be ready")
	for _, hn := range hyperNodes {
		name := hn.name
		Eventually(func() error {
			_, err := ctx.Vcclient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), name, metav1.GetOptions{})
			return err
		}, 30*time.Second, time.Second).Should(BeNil())
	}
}

func createTopologyGangJob(ctx *e2eutil.TestContext, name, queue, pri string, req v1.ResourceList, rep, minAvail int32, preemptable bool, topo *batchv1alpha1.NetworkTopologySpec) *batchv1alpha1.Job {
	labels := map[string]string{}
	if preemptable {
		labels[schedulingv1beta1.PodPreemptable] = "true"
	}
	spec := &e2eutil.JobSpec{
		Name:            name,
		Queue:           queue,
		Pri:             pri,
		Min:             minAvail,
		NetworkTopology: topo,
		Tasks: []e2eutil.TaskSpec{
			{
				Img:         e2eutil.DefaultNginxImage,
				Req:         req,
				Min:         rep,
				Rep:         rep,
				Labels:      labels,
				Tolerations: kwokTolerations,
			},
		},
	}
	return e2eutil.CreateJob(ctx, spec)
}

// verifyPodsInSameTier1Domain asserts that every pod of the job is
// scheduled to nodes within a single tier-1 HyperNode domain
// (kwok-node-{0,1} or kwok-node-{2,3}).
func verifyPodsInSameTier1Domain(ctx *e2eutil.TestContext, job *batchv1alpha1.Job) {
	pods := e2eutil.GetTasksOfJob(ctx, job)
	Expect(len(pods)).To(BeNumerically(">", 0), "expected at least one pod for job %s", job.Name)

	for _, domainNodes := range tier1Domains {
		nodeSet := map[string]bool{}
		for _, n := range domainNodes {
			nodeSet[n] = true
		}
		allInDomain := true
		for _, pod := range pods {
			if !nodeSet[pod.Spec.NodeName] {
				allInDomain = false
				break
			}
		}
		if allInDomain {
			return
		}
	}

	nodeNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		nodeNames = append(nodeNames, pod.Spec.NodeName)
	}
	Fail(fmt.Sprintf("pods of job %s are spread across tier-1 domains: %v", job.Name, nodeNames))
}

// ── gangpreempt ────────────────────────────────────────────────────────

var _ = Describe("GangPreempt E2E Test", func() {
	const (
		gpHighPri    = "gp-high-pri"
		gpMedPri     = "gp-med-pri"
		gpLowPri     = "gp-low-pri"
		gpHighPriVal = int32(100)
		gpMedPriVal  = int32(50)
		gpLowPriVal  = int32(10)
		gpQueue      = "gp-q"
	)

	var ctx *e2eutil.TestContext
	var cmc *e2eutil.ConfigMapCase

	BeforeEach(func() {
		cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(applySchedulerConfig(gangEvictConfig))
	})

	AfterEach(func() {
		if ctx != nil {
			e2eutil.CleanupTestContext(ctx)
		}
		cmc.UndoChanged()
	})

	// GP-1: Safe-bundle preemption -- surplus tasks evicted, victim gang survives.
	It("GP-1: safe-bundle preemption evicts surplus tasks, victim gang survives", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Creating low-priority gang with surplus (4 tasks, minAvail=2)")
		victim := createGangJob(ctx, "victim-gp1", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 4, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Creating high-priority preemptor gang (2 tasks, minAvail=2)")
		preemptor := createGangJob(ctx, "preemptor-gp1", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting preemptor gang to run")
		err = e2eutil.WaitTasksReady(ctx, preemptor, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Expecting victim gang to keep minAvail=2 tasks running (gang not broken)")
		err = e2eutil.WaitTasksReady(ctx, victim, 2)
		Expect(err).NotTo(HaveOccurred())
	})

	// GP-2: Multiple victim gangs -- safe bundles harvested across gangs before any gang is broken.
	It("GP-2: safe bundles harvested across multiple gangs, zero gangs broken", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Creating two low-priority gangs each with 1 surplus (2 tasks, minAvail=1)")
		victimA := createGangJob(ctx, "victim-a-gp2", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 1, true)
		err := e2eutil.WaitTasksReady(ctx, victimA, 2)
		Expect(err).NotTo(HaveOccurred())

		victimB := createGangJob(ctx, "victim-b-gp2", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 1, true)
		err = e2eutil.WaitTasksReady(ctx, victimB, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Creating high-priority preemptor (2 tasks, minAvail=2)")
		preemptor := createGangJob(ctx, "preemptor-gp2", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting preemptor to run")
		err = e2eutil.WaitTasksReady(ctx, preemptor, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Expecting both victim gangs to retain minAvail=1 (zero gangs broken)")
		err = e2eutil.WaitTasksReady(ctx, victimA, 1)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, victimB, 1)
		Expect(err).NotTo(HaveOccurred())
	})

	// GP-3: Whole-bundle eviction -- gang broken only when safe bundles are insufficient.
	It("GP-3: whole-bundle eviction breaks exactly one gang when no surplus exists", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Creating two tight low-priority gangs (2 tasks each, minAvail=2, no surplus)")
		victimA := createGangJob(ctx, "victim-a-gp3", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victimA, 2)
		Expect(err).NotTo(HaveOccurred())

		victimB := createGangJob(ctx, "victim-b-gp3", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err = e2eutil.WaitTasksReady(ctx, victimB, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Creating high-priority preemptor (2 tasks, minAvail=2)")
		preemptor := createGangJob(ctx, "preemptor-gp3", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting preemptor to run")
		err = e2eutil.WaitTasksReady(ctx, preemptor, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Expecting exactly one victim gang to be fully broken, the other untouched")
		// One gang should have 0 ready tasks (broken), the other should still have 2.
		time.Sleep(10 * time.Second)
		aReady := countReadyTasks(ctx, victimA)
		bReady := countReadyTasks(ctx, victimB)
		Expect((aReady == 0 && bReady == 2) || (aReady == 2 && bReady == 0)).To(BeTrue(),
			"expected exactly one gang broken: victimA=%d, victimB=%d", aReady, bReady)
	})

	// GP-4: Whole-bundle preemption breaks both gangs when needed for large preemptor.
	It("GP-4: whole-bundle eviction breaks both gangs for large preemptor", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Creating two tight low-priority gangs")
		victimA := createGangJob(ctx, "victim-a-gp4", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victimA, 2)
		Expect(err).NotTo(HaveOccurred())

		victimB := createGangJob(ctx, "victim-b-gp4", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err = e2eutil.WaitTasksReady(ctx, victimB, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Creating high-priority preemptor that needs all 4 slots")
		preemptor := createGangJob(ctx, "preemptor-gp4", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 4, 4, false)

		By("Expecting preemptor to run with all 4 tasks")
		err = e2eutil.WaitTasksReady(ctx, preemptor, 4)
		Expect(err).NotTo(HaveOccurred())
	})

	// GP-5: Basic gang preemption succeeds when lower-priority gang is running.
	It("GP-5: basic gang preemption of a lower-priority gang", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Creating low-priority gang filling the cluster (4 tasks, minAvail=4)")
		victim := createGangJob(ctx, "victim-gp5", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 4, 4, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Creating high-priority preemptor (2 tasks, minAvail=2)")
		preemptor := createGangJob(ctx, "preemptor-gp5", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting preemptor to run")
		err = e2eutil.WaitTasksReady(ctx, preemptor, 2)
		Expect(err).NotTo(HaveOccurred())
	})

	// GP-6: Gang preemption does not occur across queues.
	It("GP-6: gang preemption does not cross queue boundaries", func() {
		q1 := "gp6-q1"
		q2 := "gp6-q2"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(2),
				q2: deservedCPU(2),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Making q2 non-reclaimable so gangreclaim cannot interfere")
		e2eutil.SetQueueReclaimable(ctx, []string{q2}, false)

		By("Filling q2 with a low-priority gang")
		victim := createGangJob(ctx, "victim-gp6", q2, gpLowPri, e2eutil.CPU1Mem1, 4, 4, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Creating high-priority preemptor in q1")
		preemptor := createGangJob(ctx, "preemptor-gp6", q1, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting preemptor to NOT run (no same-queue victims)")
		waitTasksNotReady(ctx, preemptor, 2, "gangpreempt is same-queue only")

		By("Expecting victim gang in q2 to remain untouched")
		err = e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())
	})

	// GP-7: Gang preemption respects priority -- equal priority is not preempted.
	It("GP-7: equal-priority gang is not preempted", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpMedPri: gpMedPriVal,
			},
		})

		By("Filling cluster with medium-priority gang")
		victim := createGangJob(ctx, "victim-gp7", gpQueue, gpMedPri, e2eutil.CPU1Mem1, 4, 4, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Creating another medium-priority gang")
		preemptor := createGangJob(ctx, "preemptor-gp7", gpQueue, gpMedPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting preemptor to NOT run (equal priority)")
		waitTasksNotReady(ctx, preemptor, 2, "equal priority should not be preempted")

		By("Expecting victim to remain running")
		err = e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())
	})

	// GP-8: Priority plugin filters victims -- medium preemptor cannot evict high-priority gang.
	It("GP-8: medium-priority preemptor cannot evict high-priority victims", func() {
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{gpQueue},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				gpQueue: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				gpHighPri: gpHighPriVal,
				gpMedPri:  gpMedPriVal,
				gpLowPri:  gpLowPriVal,
			},
		})

		By("Creating high-priority gang A (2 tasks)")
		gangA := createGangJob(ctx, "gang-a-gp8", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, true)
		err := e2eutil.WaitTasksReady(ctx, gangA, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Creating low-priority gang B (2 tasks)")
		gangB := createGangJob(ctx, "gang-b-gp8", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err = e2eutil.WaitTasksReady(ctx, gangB, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Creating medium-priority preemptor C that needs 4 slots (more than gang B can provide)")
		preemptorC := createGangJob(ctx, "preemptor-c-gp8", gpQueue, gpMedPri, e2eutil.CPU1Mem1, 4, 4, false)

		By("Expecting preemptor C to NOT run (can only evict B=2 tasks, needs 4)")
		waitTasksNotReady(ctx, preemptorC, 4, "medium preemptor needing 4 slots should fail when only 2 low-priority slots are available")

		By("Expecting high-priority gang A to remain untouched")
		err = e2eutil.WaitTasksReady(ctx, gangA, 2)
		Expect(err).NotTo(HaveOccurred())
	})

	// ── gangpreempt + network topology ─────────────────────────────────

	Context("with network topology constraints", func() {
		// GP-T1: Hard tier-1 preemption succeeds -- preemptor lands within one tier-1 domain.
		It("GP-T1: hard tier-1 preemption places preemptor within one domain", func() {
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{gpQueue},
				DeservedResource: map[string]v1.ResourceList{
					gpQueue: deservedCPU(4),
				},
				PriorityClasses: map[string]int32{
					gpHighPri: gpHighPriVal,
					gpLowPri:  gpLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "gpt1")

			By("Creating low-priority victim gang (4 tasks, minAvail=2, hard tier 2)")
			victim := createTopologyGangJob(ctx, "victim-gpt1", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 4, 2, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Creating high-priority preemptor gang (2 tasks, minAvail=2, hard tier 1)")
			preemptor := createTopologyGangJob(ctx, "preemptor-gpt1", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				})

			By("Expecting preemptor to run")
			err = e2eutil.WaitTasksReady(ctx, preemptor, 2)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying preemptor pods are within one tier-1 HyperNode")
			verifyPodsInSameTier1Domain(ctx, preemptor)

			By("Expecting victim gang to retain minAvail=2 tasks")
			err = e2eutil.WaitTasksReady(ctx, victim, 2)
			Expect(err).NotTo(HaveOccurred())
		})

		// GP-T2: Hard tier-1 preemption blocked -- preemptor tasks are too large
		// to bin-pack into a 2-node tier-1 domain (2 nodes x 8 CPU = 16 CPU,
		// but 3 tasks x 5 CPU: node0=5 (3 free), node1=5 (3 free), task3
		// needs 5 but max available is 3).
		It("GP-T2: hard tier-1 preemption blocked when domain is too small", func() {
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{gpQueue},
				DeservedResource: map[string]v1.ResourceList{
					gpQueue: deservedCPU(16),
				},
				PriorityClasses: map[string]int32{
					gpHighPri: gpHighPriVal,
					gpLowPri:  gpLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "gpt2")

			By("Creating low-priority victim gang (4 tasks x 1CPU, minAvail=4, hard tier 2)")
			victim := createTopologyGangJob(ctx, "victim-gpt2", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 4, 4, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Creating high-priority preemptor (3 tasks x 5CPU, hard tier 1 -- can't bin-pack into 2x8CPU nodes)")
			preemptor := createTopologyGangJob(ctx, "preemptor-gpt2", gpQueue, gpHighPri, e2eutil.CPU5Mem5, 3, 3, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				})

			By("Expecting preemptor to NOT run")
			waitTasksNotReady(ctx, preemptor, 3, "3x5CPU can't bin-pack into tier-1 domain of 2x8CPU nodes")

			By("Expecting victim to remain running")
			err = e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())
		})

		// GP-T3: Hard tier-2 preemption succeeds at wider domain.
		It("GP-T3: hard tier-2 preemption succeeds at wider domain", func() {
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{gpQueue},
				DeservedResource: map[string]v1.ResourceList{
					gpQueue: deservedCPU(4),
				},
				PriorityClasses: map[string]int32{
					gpHighPri: gpHighPriVal,
					gpLowPri:  gpLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "gpt3")

			By("Creating low-priority victim gang (4 tasks, minAvail=2, hard tier 2)")
			victim := createTopologyGangJob(ctx, "victim-gpt3", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 4, 2, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Creating high-priority preemptor (3 tasks, minAvail=3, hard tier 2)")
			preemptor := createTopologyGangJob(ctx, "preemptor-gpt3", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 3, 3, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})

			By("Expecting preemptor to run (tier-2 domain spans all 4 nodes)")
			err = e2eutil.WaitTasksReady(ctx, preemptor, 3)
			Expect(err).NotTo(HaveOccurred())
		})

		// GP-T4: Hard tier-2 constraint but tier-1 domain suffices --
		// the gradient tries lower tiers first, so the preemptor lands
		// within a single tier-1 domain even though tier 2 is allowed.
		It("GP-T4: hard tier-2 constraint places preemptor in tier-1 domain when sufficient", func() {
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{gpQueue},
				DeservedResource: map[string]v1.ResourceList{
					gpQueue: deservedCPU(4),
				},
				PriorityClasses: map[string]int32{
					gpHighPri: gpHighPriVal,
					gpLowPri:  gpLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "gpt4")

			By("Creating low-priority victim gang (4 tasks, minAvail=2, hard tier 2)")
			victim := createTopologyGangJob(ctx, "victim-gpt4", gpQueue, gpLowPri, e2eutil.CPU1Mem1, 4, 2, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Creating high-priority preemptor (2 tasks, minAvail=2, hard tier 2 -- fits in tier-1)")
			preemptor := createTopologyGangJob(ctx, "preemptor-gpt4", gpQueue, gpHighPri, e2eutil.CPU1Mem1, 2, 2, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})

			By("Expecting preemptor to run")
			err = e2eutil.WaitTasksReady(ctx, preemptor, 2)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying preemptor pods are within one tier-1 HyperNode (gradient prefers lower tier)")
			verifyPodsInSameTier1Domain(ctx, preemptor)

			By("Expecting victim gang to retain minAvail=2 tasks")
			err = e2eutil.WaitTasksReady(ctx, victim, 2)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

// ── gangreclaim ────────────────────────────────────────────────────────

var _ = Describe("GangReclaim E2E Test", func() {
	const (
		grLowPri    = "gr-low-pri"
		grLowPriVal = int32(10)
	)

	var ctx *e2eutil.TestContext
	var cmc *e2eutil.ConfigMapCase

	BeforeEach(func() {
		cmc = e2eutil.NewConfigMapCase("volcano-system", "integration-scheduler-configmap")
		cmc.ChangeBy(applySchedulerConfig(gangEvictConfig))
	})

	AfterEach(func() {
		if ctx != nil {
			e2eutil.CleanupTestContext(ctx)
		}
		cmc.UndoChanged()
	})

	// GR-1: Safe-bundle reclaim -- surplus tasks reclaimed, victim gang survives.
	It("GR-1: safe-bundle reclaim evicts surplus, victim gang survives", func() {
		q1 := "gr1-q1"
		q2 := "gr1-q2"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(2),
				q2: deservedCPU(2),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})

		By("Filling cluster with q2 gang (4 tasks, minAvail=2, 2 surplus), overusing q2 deserved")
		victim := createGangJob(ctx, "victim-gr1", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Submitting gang to q1 (2 tasks, minAvail=2)")
		reclaimer := createGangJob(ctx, "reclaimer-gr1", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting reclaimer to run")
		err = e2eutil.WaitTasksReady(ctx, reclaimer, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Expecting victim gang to retain minAvail=2 (gang not broken)")
		err = e2eutil.WaitTasksReady(ctx, victim, 2)
		Expect(err).NotTo(HaveOccurred())
	})

	// GR-2: Whole-bundle reclaim -- gang broken only when surplus is insufficient.
	It("GR-2: whole-bundle reclaim breaks exactly one gang when no surplus exists", func() {
		q1 := "gr2-q1"
		q2 := "gr2-q2"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(2),
				q2: deservedCPU(2),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})

		By("Creating two tight gangs in q2 (no surplus), overusing q2 deserved")
		victimA := createGangJob(ctx, "victim-a-gr2", q2, grLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victimA, 2)
		Expect(err).NotTo(HaveOccurred())

		victimB := createGangJob(ctx, "victim-b-gr2", q2, grLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err = e2eutil.WaitTasksReady(ctx, victimB, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Submitting gang to q1 (2 tasks, minAvail=2)")
		reclaimer := createGangJob(ctx, "reclaimer-gr2", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting reclaimer to run")
		err = e2eutil.WaitTasksReady(ctx, reclaimer, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Expecting exactly one victim gang broken, the other untouched")
		time.Sleep(10 * time.Second)
		aReady := countReadyTasks(ctx, victimA)
		bReady := countReadyTasks(ctx, victimB)
		Expect((aReady == 0 && bReady == 2) || (aReady == 2 && bReady == 0)).To(BeTrue(),
			"expected exactly one gang broken: victimA=%d, victimB=%d", aReady, bReady)
	})

	// GR-3: Basic gang reclaim from overusing queue.
	It("GR-3: basic gang reclaim from overusing queue", func() {
		q1 := "gr3-q1"
		q2 := "gr3-q2"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(2),
				q2: deservedCPU(2),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})

		By("Filling cluster with q2 gang overusing its deserved")
		victim := createGangJob(ctx, "victim-gr3", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Submitting gang to q1")
		reclaimer := createGangJob(ctx, "reclaimer-gr3", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting reclaimer to run")
		err = e2eutil.WaitTasksReady(ctx, reclaimer, 2)
		Expect(err).NotTo(HaveOccurred())
	})

	// GR-4: Gang reclaim does not reclaim from queues within deserved.
	It("GR-4: no reclaim from queues within their deserved", func() {
		q1 := "gr4-q1"
		q2 := "gr4-q2"
		q3 := "gr4-q3"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2, q3},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(2),
				q2: deservedCPU(2),
				q3: deservedCPU(2),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})

		By("Creating jobs within deserved in q2 and q3")
		job2 := createGangJob(ctx, "job-gr4-q2", q2, grLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err := e2eutil.WaitTasksReady(ctx, job2, 2)
		Expect(err).NotTo(HaveOccurred())

		job3 := createGangJob(ctx, "job-gr4-q3", q3, grLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err = e2eutil.WaitTasksReady(ctx, job3, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Submitting gang to q1")
		reclaimer := createGangJob(ctx, "reclaimer-gr4", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting reclaimer to NOT run (no overusing queues)")
		waitTasksNotReady(ctx, reclaimer, 2, "reclaimer should not run when all queues are within deserved")

		By("Expecting existing jobs to remain untouched")
		err = e2eutil.WaitTasksReady(ctx, job2, 2)
		Expect(err).NotTo(HaveOccurred())
		err = e2eutil.WaitTasksReady(ctx, job3, 2)
		Expect(err).NotTo(HaveOccurred())
	})

	// GR-5: Gang reclaim respects queue reclaimable flag.
	It("GR-5: reclaim blocked when victim queue is not reclaimable", func() {
		q1 := "gr5-q1"
		q2 := "gr5-q2"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(2),
				q2: deservedCPU(2),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})

		By("Filling cluster with q2 gang (overusing)")
		victim := createGangJob(ctx, "victim-gr5", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Setting q2 as non-reclaimable")
		e2eutil.SetQueueReclaimable(ctx, []string{q2}, false)
		defer e2eutil.SetQueueReclaimable(ctx, []string{q2}, true)

		By("Submitting gang to q1")
		reclaimer := createGangJob(ctx, "reclaimer-gr5", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting reclaimer to NOT run (q2 is non-reclaimable)")
		waitTasksNotReady(ctx, reclaimer, 2, "reclaimer should not run when victim queue is non-reclaimable")

		By("Expecting victim to remain untouched")
		err = e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())
	})

	// GR-6: Gang reclaim respects guarantee -- does not reclaim below guarantee.
	It("GR-6: reclaim stops at guarantee boundary", func() {
		q1 := "gr6-q1"
		q2 := "gr6-q2"

		// q2 has guarantee=2CPU, deserved=2CPU, but is using 4CPU.
		// Only 2 tasks (above guarantee) can be reclaimed.
		// q1 needs 4CPU -- not enough reclaimable, so reclaim fails.
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(4),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})
		e2eutil.CreateQueueWithQueueSpec(ctx, &e2eutil.QueueSpec{
			Name:              q2,
			Weight:            1,
			GuaranteeResource: deservedCPU(2),
			DeservedResource:  deservedCPU(2),
		})
		ctx.Queues = append(ctx.Queues, q2)

		By("Filling cluster with q2 gang (4 tasks, minAvail=2), overusing but guaranteed 2CPU")
		victim := createGangJob(ctx, "victim-gr6", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true)
		err := e2eutil.WaitTasksReady(ctx, victim, 4)
		Expect(err).NotTo(HaveOccurred())

		By("Submitting large gang to q1 (4 tasks, minAvail=4)")
		reclaimer := createGangJob(ctx, "reclaimer-gr6", q1, grLowPri, e2eutil.CPU1Mem1, 4, 4, false)

		By("Expecting reclaimer to NOT run (only 2 reclaimable above guarantee, needs 4)")
		waitTasksNotReady(ctx, reclaimer, 4, "only 2 tasks above guarantee are reclaimable")
	})

	// GR-7: Overused queue is skipped as reclaimer.
	It("GR-7: overused queue is skipped as reclaimer", func() {
		q1 := "gr7-q1"
		q2 := "gr7-q2"
		ctx = e2eutil.InitTestContext(e2eutil.Options{
			Queues:             []string{q1, q2},
			NodesNumLimit:      4,
			NodesResourceLimit: e2eutil.CPU1Mem1,
			DeservedResource: map[string]v1.ResourceList{
				q1: deservedCPU(1),
				q2: deservedCPU(3),
			},
			PriorityClasses: map[string]int32{
				grLowPri: grLowPriVal,
			},
		})

		By("q1 uses 2CPU (overusing its 1CPU deserved)")
		job1 := createGangJob(ctx, "job-gr7-q1", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err := e2eutil.WaitTasksReady(ctx, job1, 2)
		Expect(err).NotTo(HaveOccurred())

		By("q2 uses 2CPU (within its 3CPU deserved)")
		job2 := createGangJob(ctx, "job-gr7-q2", q2, grLowPri, e2eutil.CPU1Mem1, 2, 2, true)
		err = e2eutil.WaitTasksReady(ctx, job2, 2)
		Expect(err).NotTo(HaveOccurred())

		By("Submitting another gang to q1 (already overused)")
		reclaimer := createGangJob(ctx, "reclaimer-gr7", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false)

		By("Expecting reclaimer to NOT run (q1 is overused, skipped as reclaimer)")
		waitTasksNotReady(ctx, reclaimer, 2, "overused queue should be skipped as reclaimer")
	})

	// ── gangreclaim + network topology ─────────────────────────────────

	Context("with network topology constraints", func() {
		// GR-T1: Hard tier-1 reclaim succeeds -- reclaimer lands within one tier-1 domain.
		It("GR-T1: hard tier-1 reclaim places reclaimer within one domain", func() {
			q1 := "grt1-q1"
			q2 := "grt1-q2"
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{q1, q2},
				DeservedResource: map[string]v1.ResourceList{
					q1: deservedCPU(2),
					q2: deservedCPU(2),
				},
				PriorityClasses: map[string]int32{
					grLowPri: grLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "grt1")

			By("Filling cluster with q2 gang (4 tasks, minAvail=2, hard tier 2, overusing)")
			victim := createTopologyGangJob(ctx, "victim-grt1", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Submitting reclaimer to q1 (2 tasks, minAvail=2, hard tier 1)")
			reclaimer := createTopologyGangJob(ctx, "reclaimer-grt1", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				})

			By("Expecting reclaimer to run")
			err = e2eutil.WaitTasksReady(ctx, reclaimer, 2)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying reclaimer pods are within one tier-1 HyperNode")
			verifyPodsInSameTier1Domain(ctx, reclaimer)

			By("Expecting victim gang to retain minAvail=2 tasks")
			err = e2eutil.WaitTasksReady(ctx, victim, 2)
			Expect(err).NotTo(HaveOccurred())
		})

		// GR-T2: Hard tier-1 reclaim blocked -- reclaimer tasks are too large
		// to bin-pack into a 2-node tier-1 domain (2 nodes x 8 CPU = 16 CPU,
		// but 3 tasks x 5 CPU can't fit).
		It("GR-T2: hard tier-1 reclaim blocked when domain is too small", func() {
			q1 := "grt2-q1"
			q2 := "grt2-q2"
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{q1, q2},
				DeservedResource: map[string]v1.ResourceList{
					q1: deservedCPU(16),
					q2: deservedCPU(16),
				},
				PriorityClasses: map[string]int32{
					grLowPri: grLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "grt2")

			By("Filling cluster with q2 gang (4 tasks x 1CPU, minAvail=4, hard tier 2, overusing)")
			victim := createTopologyGangJob(ctx, "victim-grt2", q2, grLowPri, e2eutil.CPU1Mem1, 4, 4, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Submitting reclaimer to q1 (3 tasks x 5CPU, hard tier 1 -- can't bin-pack into 2x8CPU nodes)")
			reclaimer := createTopologyGangJob(ctx, "reclaimer-grt2", q1, grLowPri, e2eutil.CPU5Mem5, 3, 3, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				})

			By("Expecting reclaimer to NOT run")
			waitTasksNotReady(ctx, reclaimer, 3, "3x5CPU can't bin-pack into tier-1 domain of 2x8CPU nodes")

			By("Expecting victim to remain running")
			err = e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())
		})

		// GR-T3: Hard tier-2 reclaim succeeds at wider domain (mirrors GP-T3).
		It("GR-T3: hard tier-2 reclaim succeeds at wider domain", func() {
			q1 := "grt3-q1"
			q2 := "grt3-q2"
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{q1, q2},
				DeservedResource: map[string]v1.ResourceList{
					q1: deservedCPU(2),
					q2: deservedCPU(2),
				},
				PriorityClasses: map[string]int32{
					grLowPri: grLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "grt3")

			By("Filling cluster with q2 gang (4 tasks, minAvail=2, hard tier 2, overusing)")
			victim := createTopologyGangJob(ctx, "victim-grt3", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Submitting reclaimer to q1 (3 tasks, minAvail=3, hard tier 2)")
			reclaimer := createTopologyGangJob(ctx, "reclaimer-grt3", q1, grLowPri, e2eutil.CPU1Mem1, 3, 3, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})

			By("Expecting reclaimer to run (tier-2 domain spans all 4 nodes)")
			err = e2eutil.WaitTasksReady(ctx, reclaimer, 3)
			Expect(err).NotTo(HaveOccurred())
		})

		// GR-T4: Hard tier-2 constraint but tier-1 domain suffices --
		// the gradient tries lower tiers first, so the reclaimer lands
		// within a single tier-1 domain even though tier 2 is allowed.
		It("GR-T4: hard tier-2 constraint places reclaimer in tier-1 domain when sufficient", func() {
			q1 := "grt4-q1"
			q2 := "grt4-q2"
			ctx = e2eutil.InitTestContext(e2eutil.Options{
				Queues: []string{q1, q2},
				DeservedResource: map[string]v1.ResourceList{
					q1: deservedCPU(2),
					q2: deservedCPU(2),
				},
				PriorityClasses: map[string]int32{
					grLowPri: grLowPriVal,
				},
			})
			setupTopoHyperNodes(ctx, "grt4")

			By("Filling cluster with q2 gang (4 tasks, minAvail=2, hard tier 2, overusing)")
			victim := createTopologyGangJob(ctx, "victim-grt4", q2, grLowPri, e2eutil.CPU1Mem1, 4, 2, true,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})
			err := e2eutil.WaitTasksReady(ctx, victim, 4)
			Expect(err).NotTo(HaveOccurred())

			By("Submitting reclaimer to q1 (2 tasks, minAvail=2, hard tier 2 -- fits in tier-1)")
			reclaimer := createTopologyGangJob(ctx, "reclaimer-grt4", q1, grLowPri, e2eutil.CPU1Mem1, 2, 2, false,
				&batchv1alpha1.NetworkTopologySpec{
					Mode:               batchv1alpha1.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				})

			By("Expecting reclaimer to run")
			err = e2eutil.WaitTasksReady(ctx, reclaimer, 2)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying reclaimer pods are within one tier-1 HyperNode (gradient prefers lower tier)")
			verifyPodsInSameTier1Domain(ctx, reclaimer)

			By("Expecting victim gang to retain minAvail=2 tasks")
			err = e2eutil.WaitTasksReady(ctx, victim, 2)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

// countReadyTasks returns the number of Running/Succeeded pods for a job.
func countReadyTasks(ctx *e2eutil.TestContext, job *batchv1alpha1.Job) int {
	pods, err := ctx.Kubeclient.CoreV1().Pods(job.Namespace).List(context.TODO(), metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	count := 0
	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, job) {
			continue
		}
		if pod.Status.Phase == v1.PodRunning || pod.Status.Phase == v1.PodSucceeded {
			count++
		}
	}
	return count
}
