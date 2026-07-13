package framework

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestCloseSessionCleansMetricsForQueueMissingFromCache(t *testing.T) {
	const queueName = "deleted-during-session"
	t.Cleanup(func() {
		metrics.DeleteQueueMetrics(queueName)
	})

	schedulerCache := cache.NewCustomMockSchedulerCache(
		"metrics-cleanup-test",
		nil,
		nil,
		&util.FakeStatusUpdater{},
		nil,
		nil,
	)
	queueObject := &schedulingv1beta1.Queue{ObjectMeta: metav1.ObjectMeta{Name: queueName}}
	schedulerCache.AddQueueV1beta1(queueObject)
	queue := schedulerCache.Queues[api.QueueID(queueName)].Clone()

	// Reproduce delete -> recreate -> delete for the same queue key while the
	// scheduling session still holds the first incarnation in its snapshot.
	schedulerCache.DeleteQueueV1beta1(queueObject)
	schedulerCache.AddQueueV1beta1(queueObject.DeepCopy())
	schedulerCache.DeleteQueueV1beta1(queueObject)

	ssn := &Session{
		cache:  schedulerCache,
		Jobs:   map[api.JobID]*api.JobInfo{},
		Queues: map[api.QueueID]*api.QueueInfo{queue.UID: queue},
	}

	// Simulate a scheduling cycle writing metrics from its stale snapshot after
	// the queue has already been removed from the scheduler cache.
	metrics.UpdateQueueAllocated(queueName, 100, 1024, nil)
	if _, found := gaugeMetricValue(t, "volcano_queue_allocated_milli_cpu", map[string]string{"queue_name": queueName}); !found {
		t.Fatal("expected stale queue metric before closing the session")
	}

	closeSession(ssn)

	if value, found := gaugeMetricValue(t, "volcano_queue_allocated_milli_cpu", map[string]string{"queue_name": queueName}); found {
		t.Fatalf("expected closeSession to delete stale queue metric, found value %v", value)
	}
}

func TestCloseSessionPreservesMetricsForRecreatedQueueStillInCache(t *testing.T) {
	const queueName = "recreated-during-session"
	t.Cleanup(func() {
		metrics.DeleteQueueMetrics(queueName)
	})

	schedulerCache := cache.NewCustomMockSchedulerCache(
		"metrics-cleanup-active-test",
		nil,
		nil,
		&util.FakeStatusUpdater{},
		nil,
		nil,
	)
	queueObject := &schedulingv1beta1.Queue{ObjectMeta: metav1.ObjectMeta{Name: queueName}}
	schedulerCache.AddQueueV1beta1(queueObject)
	queue := schedulerCache.Queues[api.QueueID(queueName)].Clone()
	schedulerCache.DeleteQueueV1beta1(queueObject)
	schedulerCache.AddQueueV1beta1(queueObject.DeepCopy())

	metrics.UpdateQueueAllocated(queueName, 200, 2048, nil)
	closeSession(&Session{
		cache:  schedulerCache,
		Jobs:   map[api.JobID]*api.JobInfo{},
		Queues: map[api.QueueID]*api.QueueInfo{queue.UID: queue},
	})

	if value, found := gaugeMetricValue(t, "volcano_queue_allocated_milli_cpu", map[string]string{"queue_name": queueName}); !found || value != 200 {
		t.Fatalf("expected recreated queue metric to remain at 200, found=%v value=%v", found, value)
	}
}

func TestSessionCleanMetricsForJobs(t *testing.T) {
	const (
		namespace = "metrics-cleanup-ns"
		jobName   = "metrics-cleanup-job"
		queueName = "metrics-cleanup-queue"
	)
	t.Cleanup(func() {
		metrics.DeleteJobMetrics(jobName, queueName, namespace)
	})

	jobID := api.JobID(namespace + "/" + jobName)
	job := api.NewJobInfo(jobID)
	job.Name = jobName
	job.Namespace = namespace
	job.Queue = api.QueueID(queueName)
	ssn := &Session{Jobs: map[api.JobID]*api.JobInfo{jobID: job}}

	t.Run("delete a job missing from the current cache snapshot", func(t *testing.T) {
		metrics.UpdateJobShare(namespace, jobName, 42)
		metrics.UpdateE2eSchedulingStartTimeByJob(jobName, queueName, namespace, time.Unix(42, 0))
		ssn.cleanMetrics(&api.ClusterInfo{
			Jobs:   map[api.JobID]*api.JobInfo{},
			Queues: map[api.QueueID]*api.QueueInfo{},
		})

		if value, found := gaugeMetricValue(t, "volcano_job_share", map[string]string{"job_ns": namespace, "job_id": jobName}); found {
			t.Fatalf("expected stale job metric to be deleted, found value %v", value)
		}
		if value, found := gaugeMetricValue(t, "volcano_e2e_job_scheduling_start_time", map[string]string{"job_name": jobName, "queue": queueName, "job_namespace": namespace}); found {
			t.Fatalf("expected stale e2e job metric to be deleted, found value %v", value)
		}
	})

	t.Run("preserve a job still present in the current cache snapshot", func(t *testing.T) {
		metrics.UpdateJobShare(namespace, jobName, 84)
		metrics.UpdateE2eSchedulingStartTimeByJob(jobName, queueName, namespace, time.Unix(84, 0))
		ssn.cleanMetrics(&api.ClusterInfo{
			Jobs:   map[api.JobID]*api.JobInfo{jobID: nil},
			Queues: map[api.QueueID]*api.QueueInfo{},
		})

		if value, found := gaugeMetricValue(t, "volcano_job_share", map[string]string{"job_ns": namespace, "job_id": jobName}); !found || value != 84 {
			t.Fatalf("expected active job metric to remain at 84, found=%v value=%v", found, value)
		}
		if value, found := gaugeMetricValue(t, "volcano_e2e_job_scheduling_start_time", map[string]string{"job_name": jobName, "queue": queueName, "job_namespace": namespace}); !found || value != 84 {
			t.Fatalf("expected active e2e job metric to remain at 84, found=%v value=%v", found, value)
		}
	})
}

func gaugeMetricValue(t *testing.T, name string, labels map[string]string) (float64, bool) {
	t.Helper()

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather prometheus metrics: %v", err)
	}

	for _, family := range metricFamilies {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !hasMetricLabels(metric.GetLabel(), labels) || metric.GetGauge() == nil {
				continue
			}
			return metric.GetGauge().GetValue(), true
		}
	}

	return 0, false
}

func hasMetricLabels(metricLabels []*dto.LabelPair, labels map[string]string) bool {
	for name, value := range labels {
		found := false
		for _, label := range metricLabels {
			if label.GetName() == name && label.GetValue() == value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
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
		})
	}
}
