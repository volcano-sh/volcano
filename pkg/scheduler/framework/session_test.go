package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
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
