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

package devicetopology

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	e2eutil "volcano.sh/volcano/test/e2e/util"
)

const (
	// gpuResource is the resource the scheduler configuration
	// volcano-scheduler-device-topology-ci.conf gives to the mock adapter.
	gpuResource = v1.ResourceName("nvidia.com/gpu")
	// gpuNodeName is the KWOK node that publishes a GPU inventory.
	gpuNodeName = "kwok-node-0"
	// noInventoryNodeName is a KWOK node with GPUs but no GPU inventory, so
	// its GPU topology is unknown. hack/run-e2e-kind.sh creates both nodes.
	noInventoryNodeName = "kwok-node-1"
	// inventoryAnnotation is the Node annotation the annotation provider
	// reads.
	inventoryAnnotation = "topology.volcano.sh/device-inventory"
	// schedulerName is the scheduler name of the Volcano scheduler.
	schedulerName = "volcano"
	// notScheduledWait is how long a Job must stay unscheduled to count as
	// rejected; the scheduler runs a session every second.
	notScheduledWait = 30 * time.Second
)

// Reasons the device topology plugin reports for a Pod it cannot place.
const (
	dataNotReadyReason        = "DeviceTopologyDataNotReady"
	domainFragmentedReason    = "DeviceTopologyDomainFragmented"
	fabricUnavailableReason   = "DeviceTopologyFabricUnavailable"
	reservationConflictReason = "DeviceTopologyReservationConflict"
	staleReason               = "DeviceTopologyStale"
)

// gpuDomains is the inventory of the GPU node: two GPU domains of one device
// and one domain of two. A Pod that needs two GPUs in one domain fits only in
// domain-2, even when two GPUs are free in total.
var gpuDomains = map[string][]string{
	"domain-0": {"gpu-0"},
	"domain-1": {"gpu-1"},
	"domain-2": {"gpu-2", "gpu-3"},
}

var kwokTolerations = []v1.Toleration{
	{
		Key:      "kwok.x-k8s.io/node",
		Operator: v1.TolerationOpEqual,
		Value:    "fake",
		Effect:   v1.TaintEffectNoSchedule,
	},
}

var _ = Describe("Device topology-aware scheduling", Ordered, func() {
	var ctx *e2eutil.TestContext

	BeforeAll(func() {
		waitNodeReady(gpuNodeName)
		waitNodeReady(noInventoryNodeName)
		setNodeGPUs(gpuNodeName, 4)
		setNodeGPUs(noInventoryNodeName, 2)
		setGPUNodeInventory()
		ctx = e2eutil.InitTestContext(e2eutil.Options{})
	})

	AfterAll(func() {
		if ctx != nil {
			e2eutil.CleanupTestContext(ctx)
		}
		clearNodeInventory(gpuNodeName)
		clearNodeGPUs(gpuNodeName)
		clearNodeGPUs(noInventoryNodeName)
	})

	It("keeps a hard Job pending until one GPU domain has room, and places it after a release", func() {
		By("filling the only domain with two GPUs")
		filling := createGPUJob(ctx, gpuJob{name: "fills-domain", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, filling, 1)).To(Succeed())

		By("keeping a second hard Job pending although two GPUs are free in different domains")
		waiting := createGPUJob(ctx, gpuJob{name: "needs-domain", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		expectNotScheduled(ctx, waiting, reservationConflictReason)

		By("placing the second Job after the first one releases its GPUs")
		deleteJob(ctx, filling)
		Expect(e2eutil.WaitTasksReady(ctx, waiting, 1)).To(Succeed())
		deleteJob(ctx, waiting)
	})

	It("places every Pod of a hard gang Job that fits", func() {
		gang := createGPUJob(ctx, gpuJob{name: "gang-fits", pods: 2, gpus: 1, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, gang, 2)).To(Succeed())
		deleteJob(ctx, gang)
	})

	It("places no Pod of a hard gang Job that does not fit, and holds no GPUs for it", func() {
		By("keeping a gang pending when only one of its Pods fits in a GPU domain")
		gang := createGPUJob(ctx, gpuJob{name: "gang-too-big", pods: 2, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		// Each Pod fits on its own, but once the first Pod has the only
		// two-GPU domain, the second Pod fits nowhere.
		expectNotScheduled(ctx, gang, domainFragmentedReason)

		By("placing another Job in the domain the gang could not use")
		single := createGPUJob(ctx, gpuJob{name: "takes-domain", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, single, 1)).To(Succeed())

		deleteJob(ctx, single)
		deleteJob(ctx, gang)
	})

	It("never places a hard Job on a Node without GPU inventory", func() {
		By("keeping a hard Job for the Node without inventory pending")
		hard := createGPUJob(ctx, gpuJob{name: "hard-no-inventory", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode, node: noInventoryNodeName})
		expectNotScheduled(ctx, hard, dataNotReadyReason)

		By("placing the same request without a device topology policy on that Node")
		plain := createGPUJob(ctx, gpuJob{name: "plain-no-inventory", pods: 1, gpus: 2, node: noInventoryNodeName})
		Expect(e2eutil.WaitTasksReady(ctx, plain, 1)).To(Succeed())

		deleteJob(ctx, plain)
		deleteJob(ctx, hard)
	})

	It("places a soft Job when no single GPU domain has room", func() {
		filling := createGPUJob(ctx, gpuJob{name: "fills-domain-soft", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, filling, 1)).To(Succeed())

		preferring := createGPUJob(ctx, gpuJob{name: "prefers-domain", pods: 1, gpus: 2, mode: batchv1alpha1.SoftDeviceTopologyMode, node: gpuNodeName})
		Expect(e2eutil.WaitTasksReady(ctx, preferring, 1)).To(Succeed())

		deleteJob(ctx, preferring)
		deleteJob(ctx, filling)
	})

	It("applies the device topology annotation of a plain Pod", func() {
		By("filling the only domain with two GPUs")
		filling := createGPUJob(ctx, gpuJob{name: "fills-domain-pod", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, filling, 1)).To(Succeed())

		By("copying the annotation into the PodGroup of the Pod")
		pod := createGPUPod(ctx, gpuPodSpec(ctx, "hard-pod", 2, hardPolicyAnnotation(), schedulerName))
		podGroup := podGroupOfPod(ctx, pod)
		Expect(podGroup.Spec.DeviceTopology).To(Equal(hardPolicy()))

		By("keeping the Pod pending although two GPUs are free in different domains")
		expectPodNotScheduled(ctx, pod, reservationConflictReason)

		By("placing the Pod after the domain is released")
		deleteJob(ctx, filling)
		Expect(e2eutil.WaitPodReady(ctx, pod)).To(Succeed())
		deletePod(ctx, pod)
	})

	It("applies the annotation of a Deployment's Pod template to its whole gang", func() {
		deployment := createGPUDeployment(ctx, "hard-deployment", 2, 1, hardPolicyAnnotation())
		Expect(e2eutil.WaitDeploymentReady(ctx, deployment.Name)).To(Succeed())

		pods := deploymentPods(ctx, deployment)
		Expect(pods).To(HaveLen(2))
		podGroup := podGroupOfPod(ctx, &pods[0])
		Expect(pods[1].Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey]).To(Equal(podGroup.Name), "both Pods should share one PodGroup")
		Expect(podGroup.Spec.MinMember).To(Equal(int32(2)))
		Expect(podGroup.Spec.DeviceTopology).To(Equal(hardPolicy()))

		deleteDeployment(ctx, deployment)
	})

	It("applies a task partition policy", func() {
		By("filling the only domain with two GPUs")
		filling := createGPUJob(ctx, gpuJob{name: "fills-domain-partition", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, filling, 1)).To(Succeed())

		By("keeping a Job with a hard partition policy pending")
		spec := gpuJobSpec(gpuJob{name: "partition-policy", pods: 1, gpus: 2})
		spec.Tasks[0].PartitionPolicy = &batchv1alpha1.PartitionPolicySpec{
			TotalPartitions: 1,
			PartitionSize:   1,
			DeviceTopology: &batchv1alpha1.DeviceTopologySpec{
				Requirements: []batchv1alpha1.DeviceTopologyRequirement{{
					Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: gpuResource},
					Mode:     batchv1alpha1.HardDeviceTopologyMode,
				}},
			},
		}
		job := e2eutil.CreateJob(ctx, spec)
		expectNotScheduled(ctx, job, reservationConflictReason)

		podGroup := podGroupOfPod(ctx, e2eutil.GetTasksOfJob(ctx, job)[0])
		Expect(podGroup.Spec.DeviceTopology).To(BeNil())
		Expect(podGroup.Spec.SubGroupPolicy).To(HaveLen(1))
		Expect(podGroup.Spec.SubGroupPolicy[0].DeviceTopology).To(Equal(hardPolicy()))

		By("placing the Job after the domain is released")
		deleteJob(ctx, filling)
		Expect(e2eutil.WaitTasksReady(ctx, job, 1)).To(Succeed())
		deleteJob(ctx, job)
	})

	It("never places a hard Job on an unhealthy GPU", func() {
		By("marking one GPU of the two-GPU domain unhealthy")
		// The extra healthy GPU keeps 4 healthy devices, which matches the
		// Node allocatable, so one inventory update is enough.
		domains := maps.Clone(gpuDomains)
		domains["domain-3"] = []string{"gpu-4"}
		publishGPUInventory(gpuInventory{domains: domains, unhealthy: map[string]bool{"gpu-3": true}})
		DeferCleanup(setGPUNodeInventory)

		job := createGPUJob(ctx, gpuJob{name: "unhealthy-gpu", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		expectNotScheduled(ctx, job, domainFragmentedReason)

		By("placing the Job after the GPU is healthy again")
		setGPUNodeInventory()
		Expect(e2eutil.WaitTasksReady(ctx, job, 1)).To(Succeed())
		deleteJob(ctx, job)
	})

	It("keeps hard Jobs off a stale GPU inventory, but places soft Jobs", func() {
		By("publishing an inventory that is already stale")
		publishGPUInventory(gpuInventory{domains: gpuDomains, stale: true})
		DeferCleanup(setGPUNodeInventory)

		hard := createGPUJob(ctx, gpuJob{name: "hard-stale", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		expectNotScheduled(ctx, hard, staleReason)

		By("placing a soft Job without a topology preference")
		soft := createGPUJob(ctx, gpuJob{name: "soft-stale", pods: 1, gpus: 2, mode: batchv1alpha1.SoftDeviceTopologyMode, node: gpuNodeName})
		Expect(e2eutil.WaitTasksReady(ctx, soft, 1)).To(Succeed())
		deleteJob(ctx, soft)

		By("placing the hard Job after the inventory is refreshed")
		setGPUNodeInventory()
		Expect(e2eutil.WaitTasksReady(ctx, hard, 1)).To(Succeed())
		deleteJob(ctx, hard)
	})

	It("rejects device topology policies at admission", func() {
		By("rejecting a Job policy without a resource name")
		spec := gpuJobSpec(gpuJob{name: "no-resource-policy", pods: 1, gpus: 1, mode: batchv1alpha1.HardDeviceTopologyMode})
		spec.DeviceTopology.Requirements[0].Resource = batchv1alpha1.DeviceResourceSelector{}
		_, err := e2eutil.CreateJobInner(ctx, spec)
		Expect(err).To(MatchError(ContainSubstring("extendedResourceName is required")))

		By("rejecting a Job policy for a resource that no task requests")
		spec = gpuJobSpec(gpuJob{name: "unrequested-gpu", pods: 1, gpus: 1, mode: batchv1alpha1.HardDeviceTopologyMode})
		// Req and Limit are the same map.
		delete(spec.Tasks[0].Req, gpuResource)
		_, err = e2eutil.CreateJobInner(ctx, spec)
		Expect(err).To(MatchError(ContainSubstring("is not requested by any task")))

		By("rejecting a Pod annotation that is not a policy")
		_, err = createGPUPodInner(ctx, gpuPodSpec(ctx, "not-a-policy", 1, "hard", schedulerName))
		Expect(err).To(MatchError(ContainSubstring("must contain a valid device topology policy")))

		By("rejecting a Pod annotation on a Pod that Volcano does not schedule")
		_, err = createGPUPodInner(ctx, gpuPodSpec(ctx, "other-scheduler", 1, hardPolicyAnnotation(), v1.DefaultSchedulerName))
		Expect(err).To(MatchError(ContainSubstring("requires a configured Volcano scheduler")))

		By("rejecting a change to the policy of a Job and of its PodGroup")
		job := createGPUJob(ctx, gpuJob{name: "immutable-policy", pods: 1, gpus: 1, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, job, 1)).To(Succeed())
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, err := e2eutil.VcClient.BatchV1alpha1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			current.Spec.DeviceTopology.Requirements[0].Mode = batchv1alpha1.SoftDeviceTopologyMode
			_, err = e2eutil.VcClient.BatchV1alpha1().Jobs(job.Namespace).Update(context.TODO(), current, metav1.UpdateOptions{})
			return err
		})
		Expect(err).To(MatchError(ContainSubstring("job updates may not change fields")))
		podGroup := podGroupOfPod(ctx, e2eutil.GetTasksOfJob(ctx, job)[0])
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			current, err := e2eutil.VcClient.SchedulingV1beta1().PodGroups(podGroup.Namespace).Get(context.TODO(), podGroup.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			current.Spec.DeviceTopology.Requirements[0].Mode = schedulingv1beta1.SoftDeviceTopologyMode
			_, err = e2eutil.VcClient.SchedulingV1beta1().PodGroups(podGroup.Namespace).Update(context.TODO(), current, metav1.UpdateOptions{})
			return err
		})
		Expect(err).To(MatchError(ContainSubstring("deviceTopology policies are immutable")))
		deleteJob(ctx, job)
	})

	It("places a hard fabric gang across two Nodes only after a fabric joins them", func() {
		By("giving " + noInventoryNodeName + " an inventory with one domain of two GPUs")
		publishGPUInventory(gpuInventory{node: noInventoryNodeName, domains: map[string][]string{"domain-0": {"node-1-gpu-0", "node-1-gpu-1"}}})

		By("keeping a hard fabric gang pending while no fabric joins the Nodes")
		// Each Node has only one domain with two free GPUs, so the gang
		// needs both Nodes.
		gang := createGPUJob(ctx, gpuJob{name: "fabric-gang", pods: 2, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode, fabric: batchv1alpha1.HardDeviceTopologyMode})
		expectNotScheduled(ctx, gang, fabricUnavailableReason)

		By("placing the gang on both Nodes after " + gpuNodeName + " declares a fabric with " + noInventoryNodeName)
		publishGPUInventory(gpuInventory{domains: gpuDomains, fabrics: []inventoryFabric{{
			ID:        "rack-1/fabric-0",
			OwnerNode: gpuNodeName,
			Members: []inventoryFabricMember{
				{Node: gpuNodeName, LocalDomains: []string{"domain-2"}},
				{Node: noInventoryNodeName, LocalDomains: []string{"domain-0"}},
			},
		}}})
		Expect(e2eutil.WaitTasksReady(ctx, gang, 2)).To(Succeed())
		nodes := map[string]bool{}
		for _, pod := range e2eutil.GetTasksOfJob(ctx, gang) {
			nodes[pod.Spec.NodeName] = true
		}
		Expect(nodes).To(Equal(map[string]bool{gpuNodeName: true, noInventoryNodeName: true}))

		deleteJob(ctx, gang)
		setGPUNodeInventory()
		clearNodeInventory(noInventoryNodeName)
	})

	It("places hard Jobs again after the scheduler restarts", func() {
		// The mock adapter keeps its reservations only in scheduler memory,
		// so a restart forgets them. No Job holds GPUs at this point, so the
		// restart cannot hide a used GPU.
		restartScheduler()

		job := createGPUJob(ctx, gpuJob{name: "after-restart", pods: 1, gpus: 2, mode: batchv1alpha1.HardDeviceTopologyMode})
		Expect(e2eutil.WaitTasksReady(ctx, job, 1)).To(Succeed())
		deleteJob(ctx, job)
	})
})

// gpuJob describes a gang Job of identical Pods that request GPUs.
type gpuJob struct {
	name string
	pods int32
	gpus int64
	// mode is the device topology mode; an empty mode means no policy.
	mode batchv1alpha1.DeviceTopologyMode
	// fabric, when set, is the mode of the policy's fabric affinity.
	fabric batchv1alpha1.DeviceTopologyMode
	// node, when set, is the only Node the Pods may run on.
	node string
}

// gpuJobSpec returns the spec of a gang Job whose Pods each request
// job.gpus GPUs and must all be placed together.
func gpuJobSpec(job gpuJob) *e2eutil.JobSpec {
	resources := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("100m"),
		gpuResource:    *resource.NewQuantity(job.gpus, resource.DecimalSI),
	}
	task := e2eutil.TaskSpec{
		Img:         e2eutil.DefaultNginxImage,
		Req:         resources,
		Limit:       resources,
		Min:         job.pods,
		Rep:         job.pods,
		Tolerations: kwokTolerations,
	}
	if job.node != "" {
		task.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      v1.LabelHostname,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{job.node},
					}},
				}},
			},
		}}
	}
	spec := &e2eutil.JobSpec{
		Name:  job.name,
		Min:   job.pods,
		Tasks: []e2eutil.TaskSpec{task},
	}
	if job.mode != "" {
		spec.DeviceTopology = &batchv1alpha1.DeviceTopologySpec{
			Requirements: []batchv1alpha1.DeviceTopologyRequirement{{
				Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: gpuResource},
				Mode:     job.mode,
			}},
		}
		if job.fabric != "" {
			spec.DeviceTopology.Fabric = &batchv1alpha1.DeviceFabricAffinity{Mode: job.fabric}
		}
	}
	return spec
}

// createGPUJob creates the Job that gpuJobSpec describes.
func createGPUJob(ctx *e2eutil.TestContext, job gpuJob) *batchv1alpha1.Job {
	return e2eutil.CreateJob(ctx, gpuJobSpec(job))
}

// expectNotScheduled requires that every Pod of a Job exists and that none of
// them is bound to a Node for notScheduledWait. The job controller creates
// the Pods only after the PodGroup leaves Pending, so waiting for them first
// makes sure the scheduler really tried to place them.
func expectNotScheduled(ctx *e2eutil.TestContext, job *batchv1alpha1.Job, reason string) {
	var want int
	for _, task := range job.Spec.Tasks {
		want += int(task.Replicas)
	}
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, e2eutil.TwoMinute, true, func(context.Context) (bool, error) {
		return len(e2eutil.GetTasksOfJob(ctx, job)) == want, nil
	})
	Expect(err).NotTo(HaveOccurred(), "job %q should have %d Pods", job.Name, want)

	expectPodsNotScheduled("job "+job.Name, reason, func() []*v1.Pod {
		return e2eutil.GetTasksOfJob(ctx, job)
	})
}

// expectPodsNotScheduled requires that none of the Pods is bound to a Node
// for notScheduledWait, and that the scheduler reports reason as at least
// one cause in the PodScheduled condition of one of them. The reason shows
// that the device topology plugin, and not something else, kept them
// pending.
func expectPodsNotScheduled(what, reason string, listPods func() []*v1.Pod) {
	reasonSeen := false
	messages := map[string]string{}
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, notScheduledWait, true, func(context.Context) (bool, error) {
		for _, pod := range listPods() {
			if pod.Spec.NodeName != "" {
				return true, fmt.Errorf("pod %s/%s was bound to Node %q", pod.Namespace, pod.Name, pod.Spec.NodeName)
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type != v1.PodScheduled || condition.Status != v1.ConditionFalse {
					continue
				}
				messages[pod.Name] = condition.Message
				if strings.Contains(condition.Message, reason) {
					reasonSeen = true
				}
			}
		}
		return false, nil
	})
	Expect(wait.Interrupted(err)).To(BeTrue(), "%s should stay unscheduled, got %v", what, err)
	Expect(reasonSeen).To(BeTrue(), "%s should be unschedulable because of %q, got PodScheduled messages %v", what, reason, messages)
}

// deleteJob deletes a Job and waits until its Pods are gone, so the
// scheduler has released their GPUs before the next step.
func deleteJob(ctx *e2eutil.TestContext, job *batchv1alpha1.Job) {
	e2eutil.DeleteJob(ctx, job)
	Expect(e2eutil.WaitJobCleanedUp(ctx, job)).To(Succeed())
}

// hardPolicy is a hard local GPU policy.
func hardPolicy() *schedulingv1beta1.DeviceTopologySpec {
	return &schedulingv1beta1.DeviceTopologySpec{
		Requirements: []schedulingv1beta1.DeviceTopologyRequirement{{
			Resource: schedulingv1beta1.DeviceResourceSelector{ExtendedResourceName: gpuResource},
			Mode:     schedulingv1beta1.HardDeviceTopologyMode,
		}},
	}
}

// hardPolicyAnnotation is hardPolicy as a volcano.sh/device-topology value.
func hardPolicyAnnotation() string {
	raw, err := json.Marshal(hardPolicy())
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

// gpuPodTemplate returns a Pod template that requests gpus GPUs and carries
// policy in the volcano.sh/device-topology annotation.
func gpuPodTemplate(gpus int64, policy, scheduler string) v1.PodTemplateSpec {
	resources := v1.ResourceList{
		v1.ResourceCPU: resource.MustParse("100m"),
		gpuResource:    *resource.NewQuantity(gpus, resource.DecimalSI),
	}
	return v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{schedulingv1beta1.DeviceTopologyAnnotationKey: policy},
		},
		Spec: v1.PodSpec{
			SchedulerName: scheduler,
			Tolerations:   kwokTolerations,
			Containers: []v1.Container{{
				Name:      "main",
				Image:     e2eutil.DefaultNginxImage,
				Resources: v1.ResourceRequirements{Requests: resources, Limits: resources},
			}},
		},
	}
}

// gpuPodSpec returns a plain Pod with no owner that requests GPUs.
func gpuPodSpec(ctx *e2eutil.TestContext, name string, gpus int64, policy, scheduler string) *v1.Pod {
	template := gpuPodTemplate(gpus, policy, scheduler)
	template.ObjectMeta.Name = name
	template.ObjectMeta.Namespace = ctx.Namespace
	return &v1.Pod{ObjectMeta: template.ObjectMeta, Spec: template.Spec}
}

func createGPUPodInner(ctx *e2eutil.TestContext, pod *v1.Pod) (*v1.Pod, error) {
	return e2eutil.KubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
}

func createGPUPod(ctx *e2eutil.TestContext, pod *v1.Pod) *v1.Pod {
	created, err := createGPUPodInner(ctx, pod)
	Expect(err).NotTo(HaveOccurred(), "failed to create Pod %s", pod.Name)
	return created
}

// deletePod deletes a Pod and waits until it is gone, so the scheduler has
// released its GPUs before the next step.
func deletePod(ctx *e2eutil.TestContext, pod *v1.Pod) {
	Expect(e2eutil.KubeClient.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})).To(Succeed())
	Expect(e2eutil.WaitPodGone(ctx, pod.Name, pod.Namespace)).To(Succeed())
}

// createGPUDeployment creates a Deployment whose Pods request gpus GPUs each
// and carry policy in their template. scheduling.volcano.sh/group-min-member
// is on the Deployment, which copies it to its ReplicaSet, where the PodGroup
// controller reads it.
func createGPUDeployment(ctx *e2eutil.TestContext, name string, replicas int32, gpus int64, policy string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	template := gpuPodTemplate(gpus, policy, schedulerName)
	template.ObjectMeta.Labels = labels
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   ctx.Namespace,
			Annotations: map[string]string{schedulingv1beta1.VolcanoGroupMinMemberAnnotationKey: fmt.Sprint(replicas)},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: template,
		},
	}
	created, err := e2eutil.KubeClient.AppsV1().Deployments(ctx.Namespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create Deployment %s", name)
	return created
}

func deploymentPods(ctx *e2eutil.TestContext, deployment *appsv1.Deployment) []v1.Pod {
	selector := metav1.FormatLabelSelector(deployment.Spec.Selector)
	pods, err := e2eutil.KubeClient.CoreV1().Pods(deployment.Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: selector})
	Expect(err).NotTo(HaveOccurred())
	return pods.Items
}

// deleteDeployment deletes a Deployment and waits until its Pods are gone.
func deleteDeployment(ctx *e2eutil.TestContext, deployment *appsv1.Deployment) {
	Expect(e2eutil.DeleteDeployment(ctx, deployment.Name)).To(Succeed())
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, e2eutil.TwoMinute, true, func(context.Context) (bool, error) {
		return len(deploymentPods(ctx, deployment)) == 0, nil
	})
	Expect(err).NotTo(HaveOccurred(), "Pods of Deployment %s were not deleted", deployment.Name)
}

// podGroupOfPod waits until the controllers give a Pod its PodGroup, then
// returns that PodGroup.
func podGroupOfPod(ctx *e2eutil.TestContext, pod *v1.Pod) *schedulingv1beta1.PodGroup {
	var podGroup *schedulingv1beta1.PodGroup
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, e2eutil.TwoMinute, true, func(context.Context) (bool, error) {
		current, err := e2eutil.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		name := current.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey]
		if name == "" {
			return false, nil
		}
		podGroup, err = e2eutil.VcClient.SchedulingV1beta1().PodGroups(pod.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		return err == nil, nil
	})
	Expect(err).NotTo(HaveOccurred(), "Pod %s/%s has no PodGroup", pod.Namespace, pod.Name)
	return podGroup
}

// expectPodNotScheduled is expectNotScheduled for a Pod that already has a
// PodGroup.
func expectPodNotScheduled(ctx *e2eutil.TestContext, pod *v1.Pod, reason string) {
	expectPodsNotScheduled("Pod "+pod.Name, reason, func() []*v1.Pod {
		current, err := e2eutil.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return []*v1.Pod{current}
	})
}

// waitNodeReady waits until KWOK reports a Node Ready. The scheduler can bind
// a Pod to the Node before the KWOK controller runs, and KWOK then never
// starts that Pod. Pulling the KWOK controller image can take minutes.
func waitNodeReady(nodeName string) {
	By("waiting for " + nodeName + " to be Ready")
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second, 10*time.Minute, true, func(context.Context) (bool, error) {
		node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		for _, condition := range node.Status.Conditions {
			if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(), "Node %s did not become Ready", nodeName)
}

// setNodeGPUs gives a Node a GPU capacity.
func setNodeGPUs(nodeName string, gpus int64) {
	updateNodeStatus(nodeName, func(node *v1.Node) {
		quantity := *resource.NewQuantity(gpus, resource.DecimalSI)
		node.Status.Capacity[gpuResource] = quantity
		node.Status.Allocatable[gpuResource] = quantity
	})
}

// clearNodeGPUs removes the GPU capacity of a Node.
func clearNodeGPUs(nodeName string) {
	updateNodeStatus(nodeName, func(node *v1.Node) {
		delete(node.Status.Capacity, gpuResource)
		delete(node.Status.Allocatable, gpuResource)
	})
}

// gpuInventory is a GPU inventory of a Node. The Node allocatable must match
// its number of healthy devices.
type gpuInventory struct {
	// node is the Node of the inventory; an empty node means the GPU node.
	node string
	// domains maps each GPU domain to its devices. Device IDs must be unique
	// across Nodes.
	domains map[string][]string
	// unhealthy holds the IDs of the unhealthy devices.
	unhealthy map[string]bool
	// stale makes the inventory expire an hour before it is published.
	stale bool
	// fabrics are the cross-Node GPU fabrics this Node declares as owner.
	fabrics []inventoryFabric
}

// inventoryFabric is a cross-Node GPU fabric in an inventory annotation.
type inventoryFabric struct {
	ID        string                  `json:"id"`
	OwnerNode string                  `json:"ownerNode"`
	Members   []inventoryFabricMember `json:"members"`
}

// inventoryFabricMember is one member Node of a fabric and its GPU domains.
type inventoryFabricMember struct {
	Node         string   `json:"node"`
	LocalDomains []string `json:"localDomains"`
}

// inventoryGeneration is the generation of the last published inventory. The
// provider accepts an inventory only when its generation is higher.
var inventoryGeneration int64

// setGPUNodeInventory publishes the normal GPU domains of the GPU node.
// setNodeGPUs runs first, so the allocatable matches its 4 healthy devices.
func setGPUNodeInventory() {
	publishGPUInventory(gpuInventory{domains: gpuDomains})
}

// publishGPUInventory publishes an inventory of a Node through the annotation
// provider. The scheduler reads it within a second, and a Job's Pods appear
// only after a scheduling session, so a Job created next already sees it.
func publishGPUInventory(gpus gpuInventory) {
	nodeName := gpus.node
	if nodeName == "" {
		nodeName = gpuNodeName
	}
	By("publishing a GPU inventory of " + nodeName)
	type device struct {
		ID           string `json:"id"`
		ResourceName string `json:"resourceName"`
		LocalDomain  string `json:"localDomain"`
		Health       string `json:"health"`
	}
	type localDomain struct {
		ID      string   `json:"id"`
		Devices []string `json:"devices"`
	}
	now := time.Now().UTC()
	inventoryGeneration = max(inventoryGeneration+1, now.Unix())
	observedAt, freshUntil := now, now.Add(24*time.Hour)
	if gpus.stale {
		observedAt, freshUntil = now.Add(-2*time.Hour), now.Add(-time.Hour)
	}
	inventory := struct {
		APIVersion   string            `json:"apiVersion"`
		Generation   int64             `json:"generation"`
		ObservedAt   time.Time         `json:"observedAt"`
		FreshUntil   time.Time         `json:"freshUntil"`
		Devices      []device          `json:"devices"`
		LocalDomains []localDomain     `json:"localDomains"`
		Fabrics      []inventoryFabric `json:"fabrics,omitempty"`
	}{
		APIVersion: "topology.volcano.sh/v1alpha1",
		Generation: inventoryGeneration,
		ObservedAt: observedAt,
		FreshUntil: freshUntil,
		Fabrics:    gpus.fabrics,
	}
	for _, domainID := range slices.Sorted(maps.Keys(gpus.domains)) {
		inventory.LocalDomains = append(inventory.LocalDomains, localDomain{ID: domainID, Devices: gpus.domains[domainID]})
		for _, deviceID := range gpus.domains[domainID] {
			health := "Healthy"
			if gpus.unhealthy[deviceID] {
				health = "Unhealthy"
			}
			inventory.Devices = append(inventory.Devices, device{ID: deviceID, ResourceName: string(gpuResource), LocalDomain: domainID, Health: health})
		}
	}
	raw, err := json.Marshal(inventory)
	Expect(err).NotTo(HaveOccurred())
	updateNode(nodeName, func(node *v1.Node) {
		if node.Annotations == nil {
			node.Annotations = map[string]string{}
		}
		node.Annotations[inventoryAnnotation] = string(raw)
	})
}

// clearNodeInventory removes the GPU inventory of a Node.
func clearNodeInventory(nodeName string) {
	updateNode(nodeName, func(node *v1.Node) {
		delete(node.Annotations, inventoryAnnotation)
	})
}

func updateNode(nodeName string, change func(*v1.Node)) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		change(node)
		_, err = e2eutil.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
		return err
	})
	Expect(err).NotTo(HaveOccurred(), "failed to update Node %s", nodeName)
}

func updateNodeStatus(nodeName string, change func(*v1.Node)) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		node, err := e2eutil.KubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		change(node)
		_, err = e2eutil.KubeClient.CoreV1().Nodes().UpdateStatus(context.TODO(), node, metav1.UpdateOptions{})
		return err
	})
	Expect(err).NotTo(HaveOccurred(), "failed to update the status of Node %s", nodeName)
}

// restartScheduler deletes the running scheduler Pods and waits for a new
// one to be ready, so the scheduler starts again from an empty cache.
func restartScheduler() {
	By("restarting the scheduler")
	namespace := os.Getenv("VOLCANO_E2E_NAMESPACE")
	if namespace == "" {
		namespace = "volcano-system"
	}
	selector := metav1.ListOptions{LabelSelector: "app=volcano-scheduler"}
	pods, err := e2eutil.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), selector)
	Expect(err).NotTo(HaveOccurred())
	Expect(pods.Items).NotTo(BeEmpty(), "no scheduler Pod in namespace %s", namespace)
	old := map[string]struct{}{}
	for _, pod := range pods.Items {
		old[string(pod.UID)] = struct{}{}
		Expect(e2eutil.KubeClient.CoreV1().Pods(namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})).To(Succeed())
	}

	err = wait.PollUntilContextTimeout(context.TODO(), time.Second, e2eutil.TwoMinute, true, func(context.Context) (bool, error) {
		pods, err := e2eutil.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), selector)
		if err != nil {
			return false, nil
		}
		for _, pod := range pods.Items {
			if _, stale := old[string(pod.UID)]; stale || pod.DeletionTimestamp != nil {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred(), "no new scheduler Pod became ready")
}
