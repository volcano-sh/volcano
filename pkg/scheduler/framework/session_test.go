package framework

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestSessionEnsureTopologyTrees(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}

	hyperNodes := api.HyperNodeInfoMap{
		"shallow-leaf":      newHyperNode("shallow-leaf", 1),
		"shallow-root":      newHyperNode("shallow-root", 2, "shallow-leaf"),
		"deep-leaf":         newHyperNode("deep-leaf", 1),
		"deep-middle":       newHyperNode("deep-middle", 2, "deep-leaf"),
		"deep-root":         newHyperNode("deep-root", 3, "deep-middle"),
		"single-tier":       newHyperNode("single-tier", 1),
		ClusterTopHyperNode: newHyperNode(ClusterTopHyperNode, 4, "shallow-root", "deep-root", "single-tier"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	ssn := &Session{
		HyperNodes: hyperNodes,
		RealNodesSet: map[string]sets.Set[string]{
			"shallow-leaf": sets.New("shallow-node"),
			"shallow-root": sets.New("shallow-node"),
			"deep-leaf":    sets.New("deep-node"),
			"deep-middle":  sets.New("deep-node"),
			"deep-root":    sets.New("deep-node"),
			"single-tier":  sets.New("single-tier-node"),
		},
	}
	ssn.EnsureTopologyTrees()

	assert.Equal(t, sets.New("shallow-root", "deep-root", "single-tier"), sets.KeySet(ssn.TopologyTrees))
	assert.Equal(t, []int{1, 2}, ssn.TopologyTrees["shallow-root"].Tiers)
	assert.Equal(t, []int{1, 2, 3}, ssn.TopologyTrees["deep-root"].Tiers)
	assert.Equal(t, []int{1}, ssn.TopologyTrees["single-tier"].Tiers)
	assert.Equal(t, sets.New("shallow-root", "shallow-leaf"), ssn.TopologyTrees["shallow-root"].HyperNodes)
	assert.Equal(t, sets.New("deep-root", "deep-middle", "deep-leaf"), ssn.TopologyTrees["deep-root"].HyperNodes)
	assert.Equal(t, sets.New("single-tier"), ssn.TopologyTrees["single-tier"].HyperNodes)
	assert.Equal(t, sets.New("shallow-node"), ssn.TopologyTrees["shallow-root"].RealNodes)
	assert.Equal(t, sets.New("deep-node"), ssn.TopologyTrees["deep-root"].RealNodes)
	assert.Equal(t, sets.New("single-tier-node"), ssn.TopologyTrees["single-tier"].RealNodes)
	assert.Equal(t, "shallow-root", ssn.HyperNodeToTopologyTree["shallow-leaf"])
	assert.Equal(t, "deep-root", ssn.HyperNodeToTopologyTree["deep-middle"])
	assert.Equal(t, "single-tier", ssn.HyperNodeToTopologyTree["single-tier"])
	_, clusterRootIndexed := ssn.HyperNodeToTopologyTree[ClusterTopHyperNode]
	assert.False(t, clusterRootIndexed)
	assert.Equal(t, "shallow-leaf", ssn.FindHyperNodeForNode("shallow-node"))
	assert.Equal(t, "deep-leaf", ssn.FindHyperNodeForNode("deep-node"))
	assert.Equal(t, "single-tier", ssn.FindHyperNodeForNode("single-tier-node"))
	assert.Empty(t, ssn.FindHyperNodeForNode("outside-node"))
}

func TestSessionAddClusterTopHyperNodeUsesNumericBoundary(t *testing.T) {
	newHyperNode := func(name string, tier int) *api.HyperNodeInfo {
		return api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
	}

	ssn := &Session{
		HyperNodes: api.HyperNodeInfoMap{
			"root-a": newHyperNode("root-a", 1),
			"root-b": newHyperNode("root-b", 3),
		},
		HyperNodesSetByTier: map[int]sets.Set[string]{
			1: sets.New[string]("root-a"),
			3: sets.New[string]("root-b"),
		},
		RealNodesList: map[string][]*api.NodeInfo{},
		RealNodesSet:  map[string]sets.Set[string]{},
	}
	nodes := []*api.NodeInfo{{Name: "node-a"}}

	ssn.addClusterTopHyperNode(nodes)

	topHn := ssn.HyperNodes[ClusterTopHyperNode]
	if assert.NotNil(t, topHn) {
		assert.Equal(t, 4, topHn.Tier(), "virtual root must be one tier above the highest real tier")
		assert.Equal(t, sets.New[string]("root-a", "root-b"), topHn.Children)
	}
	assert.Equal(t, ClusterTopHyperNode, ssn.HyperNodes["root-a"].Parent)
	assert.Equal(t, ClusterTopHyperNode, ssn.HyperNodes["root-b"].Parent)
	assert.Equal(t, sets.New[string](ClusterTopHyperNode), ssn.HyperNodesSetByTier[4])
	assert.Equal(t, nodes, ssn.RealNodesList[ClusterTopHyperNode])
	assert.Equal(t, sets.New[string]("node-a"), ssn.RealNodesSet[ClusterTopHyperNode])
}

func TestSessionRecoverAllocatedHyperNodeAcrossMixedTopology(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}

	hyperNodes := api.HyperNodeInfoMap{
		"shallow-hypernode-0": newHyperNode("shallow-hypernode-0", 1),
		"shallow-hypernode-1": newHyperNode("shallow-hypernode-1", 1),
		"shallow-hypercluster": newHyperNode(
			"shallow-hypercluster", 2, "shallow-hypernode-0", "shallow-hypernode-1"),
		"deep-superpod-0": newHyperNode("deep-superpod-0", 1),
		"deep-superpod-1": newHyperNode("deep-superpod-1", 1),
		"deep-hypernode": newHyperNode(
			"deep-hypernode", 2, "deep-superpod-0", "deep-superpod-1"),
		"deep-hypercluster": newHyperNode("deep-hypercluster", 3, "deep-hypernode"),
		ClusterTopHyperNode: newHyperNode(
			ClusterTopHyperNode, 4, "shallow-hypercluster", "deep-hypercluster"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	realNodes := map[string]sets.Set[string]{
		"shallow-hypernode-0": sets.New("shallow-node-0", "shallow-node-1"),
		"shallow-hypernode-1": sets.New("shallow-node-2", "shallow-node-3"),
		"shallow-hypercluster": sets.New(
			"shallow-node-0", "shallow-node-1", "shallow-node-2", "shallow-node-3"),
		"deep-superpod-0": sets.New("deep-node-0", "deep-node-1"),
		"deep-superpod-1": sets.New("deep-node-2", "deep-node-3"),
		"deep-hypernode": sets.New(
			"deep-node-0", "deep-node-1", "deep-node-2", "deep-node-3"),
		"deep-hypercluster": sets.New(
			"deep-node-0", "deep-node-1", "deep-node-2", "deep-node-3"),
		ClusterTopHyperNode: sets.New(
			"shallow-node-0", "shallow-node-1", "shallow-node-2", "shallow-node-3",
			"deep-node-0", "deep-node-1", "deep-node-2", "deep-node-3"),
	}

	const (
		namespace    = "test"
		podGroupName = "mixed-recovery"
		taskName     = "worker"
	)
	jobID := api.JobID(namespace + "/" + podGroupName)
	subGroupSize := int32(2)
	minSubGroups := int32(2)
	job := api.NewJobInfo(jobID)
	job.SetPodGroup(&api.PodGroup{PodGroup: scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: podGroupName, Namespace: namespace},
		Spec: scheduling.PodGroupSpec{
			MinMember: 4,
			NetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:            scheduling.HardNetworkTopologyMode,
				HighestTierName: "volcano.sh/hypercluster",
			},
			SubGroupPolicy: []scheduling.SubGroupPolicySpec{{
				Name:         taskName,
				SubGroupSize: &subGroupSize,
				MinSubGroups: &minSubGroups,
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					batchv1alpha1.TaskSpecKey: taskName,
				}},
				MatchLabelKeys: []string{batchv1alpha1.TaskPartitionID},
				NetworkTopology: &scheduling.NetworkTopologySpec{
					Mode:            scheduling.HardNetworkTopologyMode,
					HighestTierName: "volcano.sh/superpod",
				},
			}},
		},
	}})

	for partition, nodes := range [][]string{
		{"deep-node-0", "deep-node-1"},
		{"deep-node-2", "deep-node-3"},
	} {
		for index, nodeName := range nodes {
			podName := fmt.Sprintf("worker-%d-%d", partition, index)
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      podName,
					Namespace: namespace,
					UID:       types.UID(podName),
					Labels: map[string]string{
						batchv1alpha1.TaskSpecKey:     taskName,
						batchv1alpha1.TaskPartitionID: fmt.Sprint(partition),
					},
					Annotations: map[string]string{
						schedulingv1beta1.KubeGroupNameAnnotationKey: podGroupName,
					},
				},
				Spec:   v1.PodSpec{NodeName: nodeName},
				Status: v1.PodStatus{Phase: v1.PodRunning},
			}
			job.AddTaskInfo(api.NewTaskInfo(pod))
		}
	}

	ssn := &Session{DirtyJobs: sets.New[api.JobID]()}
	ssn.recoverAllocatedHyperNode(job, sets.KeySet(hyperNodes), hyperNodes, realNodes)

	assert.Equal(t, "deep-hypernode", job.AllocatedHyperNode)
	assert.Len(t, job.SubJobs, 2)
	expectedSubGroupHyperNodes := map[int]string{
		0: "deep-superpod-0",
		1: "deep-superpod-1",
	}
	for _, subJob := range job.SubJobs {
		assert.Equal(t, expectedSubGroupHyperNodes[subJob.MatchIndex], subJob.AllocatedHyperNode)
	}
	assert.True(t, ssn.DirtyJobs.Has(jobID))
}

func TestSession_adjustNetworkTopologySpec(t *testing.T) {
	tests := []struct {
		name         string
		jobs         map[api.JobID]*api.JobInfo
		nameMap      api.HyperNodeTierNameMap
		expectedJobs map[api.JobID]*api.JobInfo
	}{
		{
			name: "job with highestTierAllowed, no translation",
			jobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									HighestTierName:    "",
									HighestTierAllowed: ptr.To(2),
								},
								SubGroupPolicy: []scheduling.SubGroupPolicySpec{
									{
										NetworkTopology: &scheduling.NetworkTopologySpec{
											HighestTierName:    "",
											HighestTierAllowed: ptr.To(1),
										},
									},
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{
						"test-uid": {
							NetworkTopology: &scheduling.NetworkTopologySpec{
								HighestTierName:    "",
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			},
			nameMap: api.HyperNodeTierNameMap{
				"volcano.sh/hypernode":    1,
				"volcano.sh/hypercluster": 2,
			},
			expectedJobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									HighestTierName:    "",
									HighestTierAllowed: ptr.To(2),
								},
								SubGroupPolicy: []scheduling.SubGroupPolicySpec{
									{
										NetworkTopology: &scheduling.NetworkTopologySpec{
											HighestTierName:    "",
											HighestTierAllowed: ptr.To(1),
										},
									},
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{
						"test-uid": {
							NetworkTopology: &scheduling.NetworkTopologySpec{
								HighestTierName:    "",
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			},
		},
		{
			name: "job with highestTierName is preserved for branch resolution",
			jobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									HighestTierName:    "volcano.sh/hypercluster",
									HighestTierAllowed: nil,
								},
								SubGroupPolicy: []scheduling.SubGroupPolicySpec{
									{
										NetworkTopology: &scheduling.NetworkTopologySpec{
											HighestTierName:    "volcano.sh/hypernode",
											HighestTierAllowed: nil,
										},
									},
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{
						"test-uid": {
							NetworkTopology: &scheduling.NetworkTopologySpec{
								HighestTierName:    "volcano.sh/hypernode",
								HighestTierAllowed: nil,
							},
						},
					},
				},
			},
			nameMap: api.HyperNodeTierNameMap{
				"volcano.sh/hypernode":    1,
				"volcano.sh/hypercluster": 2,
			},
			expectedJobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									HighestTierName:    "volcano.sh/hypercluster",
									HighestTierAllowed: nil,
								},
								SubGroupPolicy: []scheduling.SubGroupPolicySpec{
									{
										NetworkTopology: &scheduling.NetworkTopologySpec{
											HighestTierName:    "volcano.sh/hypernode",
											HighestTierAllowed: nil,
										},
									},
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{
						"test-uid": {
							NetworkTopology: &scheduling.NetworkTopologySpec{
								HighestTierName:    "volcano.sh/hypernode",
								HighestTierAllowed: nil,
							},
						},
					},
				},
			},
		},
		{
			name: "job with highestTierName, failed to translate",
			jobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									HighestTierName:    "volcano.sh/hypercluster-test",
									HighestTierAllowed: nil,
								},
								SubGroupPolicy: []scheduling.SubGroupPolicySpec{
									{
										NetworkTopology: &scheduling.NetworkTopologySpec{
											HighestTierName:    "volcano.sh/hypernode-test",
											HighestTierAllowed: nil,
										},
									},
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{
						"test-uid": {
							NetworkTopology: &scheduling.NetworkTopologySpec{
								HighestTierName:    "volcano.sh/hypernode",
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			},
			nameMap: api.HyperNodeTierNameMap{
				"volcano.sh/hypernode":    1,
				"volcano.sh/hypercluster": 2,
			},
			expectedJobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									HighestTierName:    "volcano.sh/hypercluster-test",
									HighestTierAllowed: nil,
								},
								SubGroupPolicy: []scheduling.SubGroupPolicySpec{
									{
										NetworkTopology: &scheduling.NetworkTopologySpec{
											HighestTierName:    "volcano.sh/hypernode-test",
											HighestTierAllowed: nil,
										},
									},
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{
						"test-uid": {
							NetworkTopology: &scheduling.NetworkTopologySpec{
								HighestTierName:    "volcano.sh/hypernode",
								HighestTierAllowed: ptr.To(1),
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, job := range test.jobs {
				if job.PodGroup != nil && job.NetworkTopology == nil {
					job.NetworkTopology = job.PodGroup.Spec.NetworkTopology.DeepCopy()
				}
			}
			for _, job := range test.expectedJobs {
				if job.PodGroup != nil && job.NetworkTopology == nil {
					job.NetworkTopology = job.PodGroup.Spec.NetworkTopology.DeepCopy()
				}
			}
			ssn := &Session{
				Jobs:                 test.jobs,
				HyperNodeTierNameMap: test.nameMap,
			}
			ssn.adjustNetworkTopologySpec()
			for jobID, expectedJob := range test.expectedJobs {
				gotJob := ssn.Jobs[jobID]
				assert.Equal(t, expectedJob.NetworkTopology.HighestTierName,
					gotJob.NetworkTopology.HighestTierName, "job highestTierName should be equal")
				assert.Equal(t, expectedJob.NetworkTopology.HighestTierAllowed,
					gotJob.NetworkTopology.HighestTierAllowed, "job highestTierAllowed should be equal")
				for subJobID := range expectedJob.SubJobs {
					assert.Equal(t, expectedJob.SubJobs[subJobID].NetworkTopology.HighestTierName,
						gotJob.SubJobs[subJobID].NetworkTopology.HighestTierName, "subJob highestTierName should be equal")
					assert.Equal(t, expectedJob.SubJobs[subJobID].NetworkTopology.HighestTierAllowed,
						gotJob.SubJobs[subJobID].NetworkTopology.HighestTierAllowed, "subJob highestTierAllowed should be equal")
				}
			}
		})
	}
}

func TestAdjustNetworkTopologySpec_DoesNotMutatePodGroupSpec(t *testing.T) {
	maxTier := 4
	topHn := &topologyv1alpha1.HyperNode{}
	topHn.Name = ClusterTopHyperNode
	topHn.Spec.Tier = maxTier

	job := api.NewJobInfo("test-job")
	pg := &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			Spec: scheduling.PodGroupSpec{
				MinMember: 4,
				NetworkTopology: &scheduling.NetworkTopologySpec{
					Mode:            scheduling.SoftNetworkTopologyMode,
					HighestTierName: "volcano.sh/hypercluster",
				},
				SubGroupPolicy: []scheduling.SubGroupPolicySpec{
					{
						Name:         "worker",
						SubGroupSize: ptr.To(int32(4)),
						NetworkTopology: &scheduling.NetworkTopologySpec{
							Mode:            scheduling.SoftNetworkTopologyMode,
							HighestTierName: "volcano.sh/hypernode",
						},
					},
				},
			},
		},
	}
	job.SetPodGroup(pg)
	job.SubJobs["test-job/worker/0"] = api.NewSubJobInfo("test-job/worker", "test-job/worker/0", job.UID, &pg.Spec.SubGroupPolicy[0], []string{"0"})

	originalJobTopology := job.PodGroup.Spec.NetworkTopology.DeepCopy()
	originalSubGroupTopology := job.PodGroup.Spec.SubGroupPolicy[0].NetworkTopology.DeepCopy()

	ssn := &Session{
		Jobs: map[api.JobID]*api.JobInfo{
			job.UID: job,
		},
		HyperNodeTierNameMap: api.HyperNodeTierNameMap{
			"volcano.sh/hypernode":    1,
			"volcano.sh/hypercluster": 2,
		},
		HyperNodes: api.HyperNodeInfoMap{
			ClusterTopHyperNode: api.NewHyperNodeInfo(topHn),
		},
	}

	ssn.adjustNetworkTopologySpec()

	assert.Equal(t, originalJobTopology, job.PodGroup.Spec.NetworkTopology)
	assert.Equal(t, originalSubGroupTopology, job.PodGroup.Spec.SubGroupPolicy[0].NetworkTopology)
}

func TestConvertSoftToHardTopology(t *testing.T) {
	maxTier := 4

	tests := []struct {
		name                     string
		jobNetworkTopology       *scheduling.NetworkTopologySpec
		subGroupPolicies         []scheduling.SubGroupPolicySpec
		wantJobMode              scheduling.NetworkTopologyMode
		wantJobTier              *int
		wantSubGroupPolicyModes  []scheduling.NetworkTopologyMode
		wantSubGroupPolicyTiers  []*int
		wantContainsHardTopology bool
	}{
		{
			name: "job-level soft topology is converted to hard",
			jobNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode: scheduling.SoftNetworkTopologyMode,
			},
			wantJobMode:              scheduling.HardNetworkTopologyMode,
			wantJobTier:              ptr.To(maxTier),
			wantContainsHardTopology: true,
		},
		{
			name: "job-level hard topology is unchanged",
			jobNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.HardNetworkTopologyMode,
				HighestTierAllowed: ptr.To(2),
			},
			wantJobMode:              scheduling.HardNetworkTopologyMode,
			wantJobTier:              ptr.To(2),
			wantContainsHardTopology: true,
		},
		{
			name:                     "nil job topology remains nil",
			jobNetworkTopology:       nil,
			wantContainsHardTopology: false,
		},
		{
			name: "subGroupPolicy-level soft topology is converted to hard",
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:         "worker",
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode: scheduling.SoftNetworkTopologyMode,
					},
				},
			},
			wantSubGroupPolicyModes:  []scheduling.NetworkTopologyMode{scheduling.HardNetworkTopologyMode},
			wantSubGroupPolicyTiers:  []*int{ptr.To(maxTier)},
			wantContainsHardTopology: true,
		},
		{
			name: "subGroupPolicy-level hard topology is unchanged",
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:         "worker",
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode:               scheduling.HardNetworkTopologyMode,
						HighestTierAllowed: ptr.To(2),
					},
				},
			},
			wantSubGroupPolicyModes:  []scheduling.NetworkTopologyMode{scheduling.HardNetworkTopologyMode},
			wantSubGroupPolicyTiers:  []*int{ptr.To(2)},
			wantContainsHardTopology: true,
		},
		{
			name: "mixed: job soft + subGroupPolicy soft both converted",
			jobNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode: scheduling.SoftNetworkTopologyMode,
			},
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:         "worker",
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode: scheduling.SoftNetworkTopologyMode,
					},
				},
			},
			wantJobMode:              scheduling.HardNetworkTopologyMode,
			wantJobTier:              ptr.To(maxTier),
			wantSubGroupPolicyModes:  []scheduling.NetworkTopologyMode{scheduling.HardNetworkTopologyMode},
			wantSubGroupPolicyTiers:  []*int{ptr.To(maxTier)},
			wantContainsHardTopology: true,
		},
		{
			name: "mixed: job hard + subGroupPolicy soft (subgroup bounded by job tier)",
			jobNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.HardNetworkTopologyMode,
				HighestTierAllowed: ptr.To(2),
			},
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:         "worker",
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode: scheduling.SoftNetworkTopologyMode,
					},
				},
			},
			wantJobMode:              scheduling.HardNetworkTopologyMode,
			wantJobTier:              ptr.To(2),
			wantSubGroupPolicyModes:  []scheduling.NetworkTopologyMode{scheduling.HardNetworkTopologyMode},
			wantSubGroupPolicyTiers:  []*int{ptr.To(2)}, // bounded by job's HighestTierAllowed=2
			wantContainsHardTopology: true,
		},
		{
			name: "mixed: job hard tier=3 + multiple subGroupPolicies soft (all bounded by job tier)",
			jobNetworkTopology: &scheduling.NetworkTopologySpec{
				Mode:               scheduling.HardNetworkTopologyMode,
				HighestTierAllowed: ptr.To(3),
			},
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:         "worker",
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode: scheduling.SoftNetworkTopologyMode,
					},
				},
				{
					Name:         "ps",
					SubGroupSize: ptr.To(int32(2)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode: scheduling.SoftNetworkTopologyMode,
					},
				},
			},
			wantJobMode:              scheduling.HardNetworkTopologyMode,
			wantJobTier:              ptr.To(3),
			wantSubGroupPolicyModes:  []scheduling.NetworkTopologyMode{scheduling.HardNetworkTopologyMode, scheduling.HardNetworkTopologyMode},
			wantSubGroupPolicyTiers:  []*int{ptr.To(3), ptr.To(3)}, // both bounded by job's HighestTierAllowed=3
			wantContainsHardTopology: true,
		},
		{
			name: "multiple subGroupPolicies: some soft some hard",
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:         "worker",
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode: scheduling.SoftNetworkTopologyMode,
					},
				},
				{
					Name:         "ps",
					SubGroupSize: ptr.To(int32(2)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode:               scheduling.HardNetworkTopologyMode,
						HighestTierAllowed: ptr.To(1),
					},
				},
			},
			wantSubGroupPolicyModes:  []scheduling.NetworkTopologyMode{scheduling.HardNetworkTopologyMode, scheduling.HardNetworkTopologyMode},
			wantSubGroupPolicyTiers:  []*int{ptr.To(maxTier), ptr.To(1)},
			wantContainsHardTopology: true,
		},
		{
			name: "subGroupPolicy with nil NetworkTopology is unchanged",
			subGroupPolicies: []scheduling.SubGroupPolicySpec{
				{
					Name:            "worker",
					SubGroupSize:    ptr.To(int32(4)),
					NetworkTopology: nil,
				},
			},
			wantContainsHardTopology: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build JobInfo with PodGroup
			job := api.NewJobInfo("test-job")
			pg := &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					Spec: scheduling.PodGroupSpec{
						MinMember:       4,
						NetworkTopology: tt.jobNetworkTopology,
						SubGroupPolicy:  tt.subGroupPolicies,
					},
				},
			}
			job.SetPodGroup(pg)

			// Create SubJobs based on SubGroupPolicy
			for _, policy := range tt.subGroupPolicies {
				policyCopy := policy
				subJobID := api.SubJobID(fmt.Sprintf("test-job/%s/0", policy.Name))
				gid := api.SubJobGID(fmt.Sprintf("test-job/%s", policy.Name))
				job.SubJobs[subJobID] = api.NewSubJobInfo(gid, subJobID, "test-job", &policyCopy, []string{"0"})
			}
			// Create default SubJob if no SubGroupPolicy
			if len(tt.subGroupPolicies) == 0 {
				defaultSubJobID := job.DefaultSubJobID()
				defaultPolicy := &scheduling.SubGroupPolicySpec{
					SubGroupSize: ptr.To(int32(4)),
				}
				if tt.jobNetworkTopology != nil {
					defaultPolicy.NetworkTopology = tt.jobNetworkTopology.DeepCopy()
				}
				gid := api.SubJobGID(string(job.UID))
				job.SubJobs[defaultSubJobID] = api.NewSubJobInfo(gid, defaultSubJobID, job.UID, defaultPolicy, nil)
			}

			// Call the function under test
			convertSoftToHardTopology(job, maxTier)

			// Verify job-level NetworkTopology
			if tt.jobNetworkTopology != nil {
				assert.NotNil(t, job.NetworkTopology)
				assert.Equal(t, tt.wantJobMode, job.NetworkTopology.Mode,
					"job-level mode mismatch")
				if tt.wantJobTier != nil {
					assert.NotNil(t, job.NetworkTopology.HighestTierAllowed)
					assert.Equal(t, *tt.wantJobTier, *job.NetworkTopology.HighestTierAllowed,
						"job-level tier mismatch")
				}
			} else {
				assert.Nil(t, job.NetworkTopology,
					"job-level topology should remain nil")
			}

			// Verify SubJob-level NetworkTopology derived from SubGroupPolicy.
			for i, policy := range tt.subGroupPolicies {
				if i < len(tt.wantSubGroupPolicyModes) && policy.NetworkTopology != nil {
					subJobID := api.SubJobID(fmt.Sprintf("test-job/%s/0", policy.Name))
					subJob := job.SubJobs[subJobID]
					assert.NotNil(t, subJob)
					assert.Equal(t, tt.wantSubGroupPolicyModes[i], subJob.NetworkTopology.Mode,
						"SubJob derived from SubGroupPolicy[%d] mode mismatch", i)
					if tt.wantSubGroupPolicyTiers[i] != nil {
						assert.NotNil(t, subJob.NetworkTopology.HighestTierAllowed)
						assert.Equal(t, *tt.wantSubGroupPolicyTiers[i], *subJob.NetworkTopology.HighestTierAllowed,
							"SubJob derived from SubGroupPolicy[%d] tier mismatch", i)
					}
				}
			}

			// Verify ContainsHardTopology
			assert.Equal(t, tt.wantContainsHardTopology, job.ContainsHardTopology(),
				"ContainsHardTopology mismatch")

			// Verify SubJob-level topology conversion
			for _, subJob := range job.SubJobs {
				if subJob.WithNetworkTopology() {
					isHard, tier := subJob.IsHardTopologyMode()
					assert.True(t, isHard,
						"SubJob %s should be hard mode after conversion", subJob.UID)
					assert.True(t, tier > 0,
						"SubJob %s should have a valid tier", subJob.UID)
					assert.False(t, subJob.IsSoftTopologyMode(),
						"SubJob %s should not be soft mode after conversion", subJob.UID)
				}
			}
		})
	}
}

func TestConvertSoftToHardTopology_NilPodGroup(t *testing.T) {
	job := api.NewJobInfo("test-job")
	// PodGroup is nil, should not panic
	convertSoftToHardTopology(job, 4)
	assert.Nil(t, job.PodGroup, "PodGroup should remain nil")
}

func TestAdjustNetworkTopologySpec_SoftToHardConversion(t *testing.T) {
	// This test verifies that adjustNetworkTopologySpec converts soft mode while
	// preserving hard tier names for branch-local resolution.
	maxTier := 4 // ClusterTopHyperNode tier will be max(existing tiers) + 1 = 3 + 1 = 4

	topHn := &topologyv1alpha1.HyperNode{}
	topHn.Name = ClusterTopHyperNode
	topHn.Spec.Tier = maxTier

	tests := []struct {
		name        string
		jobs        map[api.JobID]*api.JobInfo
		nameMap     api.HyperNodeTierNameMap
		hyperNodes  api.HyperNodeInfoMap
		wantJobMode scheduling.NetworkTopologyMode
		wantJobTier *int
		wantJobName string
	}{
		{
			name: "soft topology with tierName is converted to an unrestricted numeric boundary",
			jobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									Mode:            scheduling.SoftNetworkTopologyMode,
									HighestTierName: "volcano.sh/hypercluster",
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{},
				},
			},
			nameMap: api.HyperNodeTierNameMap{
				"volcano.sh/hypernode":    1,
				"volcano.sh/hypercluster": 2,
			},
			hyperNodes: api.HyperNodeInfoMap{
				ClusterTopHyperNode: api.NewHyperNodeInfo(topHn),
			},
			wantJobMode: scheduling.HardNetworkTopologyMode,
			wantJobTier: ptr.To(maxTier),
		},
		{
			name: "pure soft topology without tierName: converted with maxTier",
			jobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									Mode: scheduling.SoftNetworkTopologyMode,
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{},
				},
			},
			nameMap: api.HyperNodeTierNameMap{},
			hyperNodes: api.HyperNodeInfoMap{
				ClusterTopHyperNode: api.NewHyperNodeInfo(topHn),
			},
			wantJobMode: scheduling.HardNetworkTopologyMode,
			wantJobTier: ptr.To(maxTier),
		},
		{
			name: "hard topology with tierName is preserved",
			jobs: map[api.JobID]*api.JobInfo{
				"test-uid": {
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								NetworkTopology: &scheduling.NetworkTopologySpec{
									Mode:            scheduling.HardNetworkTopologyMode,
									HighestTierName: "volcano.sh/hypernode",
								},
							},
						},
					},
					SubJobs: map[api.SubJobID]*api.SubJobInfo{},
				},
			},
			nameMap: api.HyperNodeTierNameMap{
				"volcano.sh/hypernode":    1,
				"volcano.sh/hypercluster": 2,
			},
			hyperNodes: api.HyperNodeInfoMap{
				ClusterTopHyperNode: api.NewHyperNodeInfo(topHn),
			},
			wantJobMode: scheduling.HardNetworkTopologyMode,
			wantJobTier: nil,
			wantJobName: "volcano.sh/hypernode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, job := range tt.jobs {
				if job.PodGroup != nil && job.NetworkTopology == nil {
					job.NetworkTopology = job.PodGroup.Spec.NetworkTopology.DeepCopy()
				}
			}
			ssn := &Session{
				Jobs:                 tt.jobs,
				HyperNodeTierNameMap: tt.nameMap,
				HyperNodes:           tt.hyperNodes,
			}
			ssn.adjustNetworkTopologySpec()

			gotJob := ssn.Jobs["test-uid"]
			assert.Equal(t, tt.wantJobMode, gotJob.NetworkTopology.Mode, "job mode mismatch")
			assert.Equal(t, tt.wantJobTier, gotJob.NetworkTopology.HighestTierAllowed, "job tier mismatch")
			assert.Equal(t, tt.wantJobName, gotJob.NetworkTopology.HighestTierName, "job tier name mismatch")
		})
	}
}

func TestAdjustNetworkTopologySpecRecordsSoftConversionProvenance(t *testing.T) {
	maxTier := 2
	topHn := &topologyv1alpha1.HyperNode{}
	topHn.Name = ClusterTopHyperNode
	topHn.Spec.Tier = maxTier

	job := api.NewJobInfo("soft-provenance")
	job.PodGroup = &api.PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
		NetworkTopology: &scheduling.NetworkTopologySpec{Mode: scheduling.SoftNetworkTopologyMode},
	}}}
	job.NetworkTopology = job.PodGroup.Spec.NetworkTopology.DeepCopy()
	job.SubJobs[api.SubJobID("soft-provenance/default")] = &api.SubJobInfo{
		UID:             api.SubJobID("soft-provenance/default"),
		NetworkTopology: &scheduling.NetworkTopologySpec{Mode: scheduling.SoftNetworkTopologyMode},
	}

	ssn := &Session{
		Jobs: map[api.JobID]*api.JobInfo{job.UID: job},
		HyperNodes: api.HyperNodeInfoMap{
			ClusterTopHyperNode: api.NewHyperNodeInfo(topHn),
		},
	}
	ssn.adjustNetworkTopologySpec()

	assert.True(t, job.IsSoftTopologyConverted())
	assert.True(t, job.SubJobs[api.SubJobID("soft-provenance/default")].IsSoftTopologyConverted())
}

func TestGetPodGroupPhase(t *testing.T) {
	newJob := func(minMember int32, currentPhase scheduling.PodGroupPhase, tasks ...*api.TaskInfo) *api.JobInfo {
		job := api.NewJobInfo("test-job", tasks...)
		job.PodGroup = &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					MinMember: minMember,
				},
				Status: scheduling.PodGroupStatus{
					Phase: currentPhase,
				},
			},
		}
		return job
	}
	newTask := func(name string, status api.TaskStatus, nodeName string) *api.TaskInfo {
		return &api.TaskInfo{
			UID: api.TaskID(name),
			TransactionContext: api.TransactionContext{
				Status:   status,
				NodeName: nodeName,
			},
			Resreq: api.EmptyResource(),
		}
	}

	tests := []struct {
		name          string
		job           *api.JobInfo
		unschedulable bool
		expected      scheduling.PodGroupPhase
	}{
		{
			name: "single pod terminating keeps Running",
			job: newJob(1, scheduling.PodGroupRunning,
				newTask("task-1", api.Releasing, "node-1")),
			expected: scheduling.PodGroupRunning,
		},
		{
			name: "multi pod partial terminating keeps Running",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Releasing, "node-2")),
			expected: scheduling.PodGroupRunning,
		},
		{
			name: "all pods terminating keeps Running",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Releasing, "node-1"),
				newTask("task-2", api.Releasing, "node-2")),
			expected: scheduling.PodGroupRunning,
		},
		{
			name: "never-scheduled pending pod deleted stays Pending",
			job: newJob(2, scheduling.PodGroupPending,
				newTask("task-1", api.Releasing, ""),
				newTask("task-2", api.Pending, "")),
			expected: scheduling.PodGroupPending,
		},
		{
			name: "all scheduled tasks completed",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Succeeded, "node-1"),
				newTask("task-2", api.Succeeded, "node-2")),
			expected: scheduling.PodGroupCompleted,
		},
		{
			name: "scheduled releasing tasks below minMember fall to Pending",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Releasing, "node-1")),
			expected: scheduling.PodGroupPending,
		},
		{
			name: "running and unschedulable is Unknown",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Pending, "")),
			unschedulable: true,
			expected:      scheduling.PodGroupUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getPodGroupPhase(tc.job, tc.unschedulable)
			assert.Equal(t, tc.expected, got)
		})
	}
}
