package framework

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

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
			name: "job with highestTierName, need translation",
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
			ssn := &Session{
				Jobs:                 test.jobs,
				HyperNodeTierNameMap: test.nameMap,
			}
			ssn.adjustNetworkTopologySpec()
			for jobID, expectedJob := range test.expectedJobs {
				gotJob := ssn.Jobs[jobID]
				assert.Equal(t, expectedJob.PodGroup.Spec.NetworkTopology.HighestTierName,
					gotJob.PodGroup.Spec.NetworkTopology.HighestTierName, "job highestTierName should be equal")
				assert.Equal(t, expectedJob.PodGroup.Spec.NetworkTopology.HighestTierAllowed,
					gotJob.PodGroup.Spec.NetworkTopology.HighestTierAllowed, "job highestTierAllowed should be equal")
				for index := range expectedJob.PodGroup.Spec.SubGroupPolicy {
					assert.Equal(t, expectedJob.PodGroup.Spec.SubGroupPolicy[index].NetworkTopology.HighestTierName,
						gotJob.PodGroup.Spec.SubGroupPolicy[index].NetworkTopology.HighestTierName, "subGroupPolicy highestTierName should be equal")
					assert.Equal(t, expectedJob.PodGroup.Spec.SubGroupPolicy[index].NetworkTopology.HighestTierAllowed,
						gotJob.PodGroup.Spec.SubGroupPolicy[index].NetworkTopology.HighestTierAllowed, "subGroupPolicy highestTierAllowed should be equal")
				}
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
				assert.NotNil(t, job.PodGroup.Spec.NetworkTopology)
				assert.Equal(t, tt.wantJobMode, job.PodGroup.Spec.NetworkTopology.Mode,
					"job-level mode mismatch")
				if tt.wantJobTier != nil {
					assert.NotNil(t, job.PodGroup.Spec.NetworkTopology.HighestTierAllowed)
					assert.Equal(t, *tt.wantJobTier, *job.PodGroup.Spec.NetworkTopology.HighestTierAllowed,
						"job-level tier mismatch")
				}
			} else {
				assert.Nil(t, job.PodGroup.Spec.NetworkTopology,
					"job-level topology should remain nil")
			}

			// Verify SubGroupPolicy-level NetworkTopology
			for i, policy := range job.PodGroup.Spec.SubGroupPolicy {
				if i < len(tt.wantSubGroupPolicyModes) {
					if policy.NetworkTopology != nil {
						assert.Equal(t, tt.wantSubGroupPolicyModes[i], policy.NetworkTopology.Mode,
							"SubGroupPolicy[%d] mode mismatch", i)
						if tt.wantSubGroupPolicyTiers[i] != nil {
							assert.NotNil(t, policy.NetworkTopology.HighestTierAllowed)
							assert.Equal(t, *tt.wantSubGroupPolicyTiers[i], *policy.NetworkTopology.HighestTierAllowed,
								"SubGroupPolicy[%d] tier mismatch", i)
						}
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
	// This test verifies that adjustNetworkTopologySpec performs both tier name translation
	// and soft→hard conversion in the same place.
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
	}{
		{
			name: "soft topology with tierName: both translated and converted",
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
			// tierName is translated first (HighestTierAllowed=2), then soft→hard uses that tier
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
			name: "hard topology with tierName: only translated, not re-converted",
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
			wantJobTier: ptr.To(1), // translated from tierName, not overwritten by maxTier
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := &Session{
				Jobs:                 tt.jobs,
				HyperNodeTierNameMap: tt.nameMap,
				HyperNodes:           tt.hyperNodes,
			}
			ssn.adjustNetworkTopologySpec()

			gotJob := ssn.Jobs["test-uid"]
			assert.Equal(t, tt.wantJobMode, gotJob.PodGroup.Spec.NetworkTopology.Mode, "job mode mismatch")
			assert.Equal(t, tt.wantJobTier, gotJob.PodGroup.Spec.NetworkTopology.HighestTierAllowed, "job tier mismatch")
		})
	}
}
