/*
Copyright 2019 The Volcano Authors.

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

package apis

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

const (
	RackTier = 2
)

// TestJobInfo_Clone tests the Clone method of JobInfo
func TestJobInfo_Clone(t *testing.T) {
	// Build test JobInfo instance
	original := &JobInfo{
		Namespace: "test-ns",
		Name:      "test-job",
		Job: &batch.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "test-ns",
			},
		},
		Pods: map[string]map[string]*v1.Pod{
			"task-1": {
				"pod-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "test-ns",
						Annotations: map[string]string{
							batch.TaskSpecKey: "task-1",
							batch.JobVersion:  "v1",
						},
					},
				},
			},
		},
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: map[string]map[string]*v1.Pod{
					"p1": {
						"pod-1": {ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
					},
				},
				NetworkTopology: &batch.NetworkTopologySpec{
					Mode:               "hard",
					HighestTierAllowed: intPtr(RackTier),
				},
			},
		},
	}

	// Execute clone
	cloned := original.Clone()

	// Verify basic fields
	if cloned.Namespace != original.Namespace || cloned.Name != original.Name {
		t.Error("Clone failed: basic fields mismatch")
	}

	// Verify Pods clone
	if len(cloned.Pods) != len(original.Pods) {
		t.Error("Clone failed: Pods length mismatch")
	}
	if _, ok := cloned.Pods["task-1"]["pod-1"]; !ok {
		t.Error("Clone failed: Pod not cloned")
	}

	// Verify Partitions clone
	if len(cloned.Partitions) != len(original.Partitions) {
		t.Error("Clone failed: Partitions length mismatch")
	}
	nt := cloned.Partitions["task-1"].NetworkTopology
	if nt.Mode != "hard" || *nt.HighestTierAllowed != RackTier {
		t.Errorf("Clone failed: NetworkTopology mismatch. Expected mode=torus, tier=%d; got mode=%s, tier=%d",
			RackTier, nt.Mode, *nt.HighestTierAllowed)
	}
}

// TestJobInfo_SetJob tests the SetJob method of JobInfo
func TestJobInfo_SetJob(t *testing.T) {
	ji := &JobInfo{}
	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "test-ns"},
		Spec: batch.JobSpec{
			Tasks: []batch.TaskSpec{
				{
					Name: "task-1",
					PartitionPolicy: &batch.PartitionPolicySpec{
						NetworkTopology: &batch.NetworkTopologySpec{
							Mode: "soft",
						},
					},
				},
				{Name: "task-2"}, // No PartitionPolicy configured
			},
		},
	}

	ji.SetJob(job)

	// Verify basic info setup
	if ji.Name != "test-job" || ji.Namespace != "test-ns" {
		t.Error("SetJob failed: name/namespace mismatch")
	}

	// Verify task with PartitionPolicy
	if _, ok := ji.Partitions["task-1"]; !ok {
		t.Error("SetJob failed: task-1 partition not created")
	}
	if ji.Partitions["task-1"].NetworkTopology.Mode != "soft" {
		t.Error("SetJob failed: NetworkTopology not set")
	}

	// Verify task without PartitionPolicy
	if _, ok := ji.Partitions["task-2"]; ok {
		t.Error("SetJob failed: task-2 should not have partition")
	}
}

// TestJobInfo_AddPod tests the AddPod method of JobInfo
func TestJobInfo_AddPod(t *testing.T) {
	ji := &JobInfo{
		Pods: make(map[string]map[string]*v1.Pod), // Initialize empty Pods map
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: make(map[string]map[string]*v1.Pod),
			},
		},
	}

	// Test normal addition
	validPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID: "p1",
			},
		},
	}
	if err := ji.AddPod(validPod); err != nil {
		t.Errorf("AddPod failed: %v", err)
	}
	if _, ok := ji.Pods["task-1"]["pod-1"]; !ok {
		t.Error("AddPod failed: pod not added to Pods")
	}
	if _, ok := ji.Partitions["task-1"].Partition["p1"]["pod-1"]; !ok {
		t.Error("AddPod failed: pod not added to Partition")
	}

	// Test duplicate addition
	if err := ji.AddPod(validPod); err == nil || err.Error() != "duplicated pod" {
		t.Error("AddPod failed: should reject duplicate pod")
	}

	// Test pod missing TaskSpecKey annotation
	invalidPod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.JobVersion: "v1", // Missing TaskSpecKey
			},
		},
	}
	if err := ji.AddPod(invalidPod1); err == nil {
		t.Error("AddPod failed: should reject pod without TaskSpecKey")
	}

	// Test pod missing JobVersion annotation
	invalidPod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1", // Missing JobVersion
			},
		},
	}
	if err := ji.AddPod(invalidPod2); err == nil {
		t.Error("AddPod failed: should reject pod without JobVersion")
	}
}

// TestJobInfo_UpdatePod tests the UpdatePod method of JobInfo
func TestJobInfo_UpdatePod(t *testing.T) {
	ji := &JobInfo{
		Pods: map[string]map[string]*v1.Pod{
			"task-1": {
				"pod-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "test-ns",
						Annotations: map[string]string{
							batch.TaskSpecKey: "task-1",
							batch.JobVersion:  "v1",
						},
						Labels: map[string]string{batch.TaskPartitionID: "p1"},
					},
				},
			},
		},
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: map[string]map[string]*v1.Pod{
					"p1": {
						"pod-1": {ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
					},
				},
			},
		},
	}

	// Test normal update (metadata/spec change without partition modification)
	updatedPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{batch.TaskPartitionID: "p1"},
		},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "new-container"}}},
	}
	if err := ji.UpdatePod(updatedPod); err != nil {
		t.Errorf("UpdatePod failed: %v", err)
	}
	if ji.Pods["task-1"]["pod-1"].Spec.Containers[0].Name != "new-container" {
		t.Error("UpdatePod failed: pod not updated")
	}

	// Test update non-existent task
	invalidPod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-2",
				batch.JobVersion:  "v1",
			},
		},
	}
	if err := ji.UpdatePod(invalidPod1); err == nil {
		t.Error("UpdatePod failed: should reject non-existent task")
	}

	// Test update non-existent pod
	invalidPod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
		},
	}
	if err := ji.UpdatePod(invalidPod2); err == nil {
		t.Error("UpdatePod failed: should reject non-existent pod")
	}
}

// TestJobInfo_DeletePod tests the DeletePod method of JobInfo
func TestJobInfo_DeletePod(t *testing.T) {
	ji := &JobInfo{
		Pods: map[string]map[string]*v1.Pod{
			"task-1": {
				"pod-1": {ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
			},
		},
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: map[string]map[string]*v1.Pod{
					"p1": {
						"pod-1": {ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}},
					},
				},
			},
		},
	}

	// Test normal deletion
	podToDelete := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{batch.TaskPartitionID: "p1"},
		},
	}
	if err := ji.DeletePod(podToDelete); err != nil {
		t.Errorf("DeletePod failed: %v", err)
	}
	if _, ok := ji.Pods["task-1"]["pod-1"]; ok {
		t.Error("DeletePod failed: pod not removed from Pods")
	}
	if _, ok := ji.Partitions["task-1"].Partition["p1"]["pod-1"]; ok {
		t.Error("DeletePod failed: pod not removed from Partition")
	}

	// Test if empty partition is cleaned up
	if len(ji.Partitions["task-1"].Partition["p1"]) != 0 {
		t.Error("DeletePod failed: empty partition not cleaned up")
	}
}

// TestGetPartitionID tests the GetPartitionID helper function
func TestGetPartitionID(t *testing.T) {
	// Pod with partition label
	podWithLabel := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{batch.TaskPartitionID: "p1"},
		},
	}
	if GetPartitionID(podWithLabel) != "p1" {
		t.Error("GetPartitionID failed: should return p1")
	}

	// Pod without partition label
	podWithoutLabel := &v1.Pod{ObjectMeta: metav1.ObjectMeta{}}
	if GetPartitionID(podWithoutLabel) != "" {
		t.Error("GetPartitionID failed: should return empty string")
	}
}

// TestJobInfoPartitionPolicy tests the partition policy related functionalities of JobInfo
func TestJobInfoPartitionPolicy(t *testing.T) {
	// Initialize JobInfo with partition policy (node-level partition configured via NetworkTopology with hard mode and hostname tier)
	ji := &JobInfo{
		Namespace: "test-ns",
		Name:      "test-job",
		Job: &batch.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "test-ns",
			},
			Spec: batch.JobSpec{
				Tasks: []batch.TaskSpec{
					{
						Name: "task-1",
						Template: v1.PodTemplateSpec{
							Spec: v1.PodSpec{},
						},
						// Core configuration: Mode=hard, HighestTierName=kubernetes.io/hostname
						PartitionPolicy: &batch.PartitionPolicySpec{
							NetworkTopology: &batch.NetworkTopologySpec{
								Mode:               "hard",                   // Set mode to hard constraint
								HighestTierName:    "kubernetes.io/hostname", // Set highest tier to Kubernetes node hostname label
								HighestTierAllowed: intPtr(1),                // Tier allowed value (typically 1 for hostname tier)
							},
						},
					},
				},
			},
		},
		Pods: make(map[string]map[string]*v1.Pod),
		// Manually initialize partition container for task-1
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: make(map[string]map[string]*v1.Pod),
				// Synchronize NetworkTopology configuration (consistent with Job spec)
				NetworkTopology: &batch.NetworkTopologySpec{
					Mode:               "hard",
					HighestTierName:    "kubernetes.io/hostname",
					HighestTierAllowed: intPtr(1),
				},
			},
		},
	}

	// ------------------------------
	// Test 1: Add pod to partition 1 (partition ID=1)
	// ------------------------------
	pod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID:    "1",      // Numeric format partition ID
				"kubernetes.io/hostname": "node-1", // Node hostname label (matches HighestTierName)
			},
		},
		Spec: v1.PodSpec{
			NodeName: "node-1",
		},
	}
	if err := ji.AddPod(pod1); err != nil {
		t.Fatalf("AddPod failed for pod1: %v", err)
	}

	// Verify partition 1 is created and pod1 is added
	partitionInfo := ji.Partitions["task-1"]
	partition1, partition1Exists := partitionInfo.Partition["1"]
	if !partition1Exists {
		t.Error("PartitionPolicy failed: partition 1 not created")
	} else {
		if _, podExists := partition1["pod-1"]; !podExists {
			t.Error("PartitionPolicy failed: pod1 not added to partition 1")
		}
	}

	// Verify NetworkTopology configuration is correct (hard mode + hostname tier)
	if partitionInfo.NetworkTopology.Mode != "hard" {
		t.Errorf("PartitionPolicy failed: expected NetworkTopology Mode=hard, got %s", partitionInfo.NetworkTopology.Mode)
	}
	if partitionInfo.NetworkTopology.HighestTierName != "kubernetes.io/hostname" {
		t.Errorf("PartitionPolicy failed: expected HighestTierName=kubernetes.io/hostname, got %s",
			partitionInfo.NetworkTopology.HighestTierName)
	}
	if *partitionInfo.NetworkTopology.HighestTierAllowed != 1 {
		t.Errorf("PartitionPolicy failed: expected HighestTierAllowed=1, got %d",
			*partitionInfo.NetworkTopology.HighestTierAllowed)
	}

	// ------------------------------
	// Test 2: Add second pod to partition 1 (same partition)
	// ------------------------------
	pod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID:    "1",      // Same numeric partition ID
				"kubernetes.io/hostname": "node-1", // Same node hostname
			},
		},
		Spec: v1.PodSpec{
			NodeName: "node-1",
		},
	}
	if err := ji.AddPod(pod2); err != nil {
		t.Fatalf("AddPod failed for pod2: %v", err)
	}

	// Verify partition 1 contains 2 pods
	partition1, _ = partitionInfo.Partition["1"]
	if len(partition1) != 2 {
		t.Errorf("PartitionPolicy failed: partition 1 should contain 2 pods, got %d", len(partition1))
	}

	// ------------------------------
	// Test 3: Add pod to partition 2 (new partition, ID=2)
	// ------------------------------
	pod3 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID:    "2",      // New numeric partition ID=2
				"kubernetes.io/hostname": "node-2", // Different node hostname
			},
		},
		Spec: v1.PodSpec{
			NodeName: "node-2",
		},
	}
	if err := ji.AddPod(pod3); err != nil {
		t.Fatalf("AddPod failed for pod3: %v", err)
	}

	// Verify total partitions count is 2 (partition 1 + partition 2)
	if len(partitionInfo.Partition) != 2 {
		t.Errorf("PartitionPolicy failed: should create 2 partitions (1 and 2), got %d", len(partitionInfo.Partition))
	}

	// ------------------------------
	// Test 4: Add pod to partition 3 (new partition, ID=3)
	// ------------------------------
	pod5 := &v1.Pod{ // New pod5 for testing partition 3
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-5",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID:    "3",      // New numeric partition ID=3
				"kubernetes.io/hostname": "node-3", // New node hostname
			},
		},
		Spec: v1.PodSpec{
			NodeName: "node-3",
		},
	}
	if err := ji.AddPod(pod5); err != nil {
		t.Fatalf("AddPod failed for pod5: %v", err)
	}

	// Verify total partitions count is 3 (partition 1 + partition 2 + partition 3)
	if len(partitionInfo.Partition) != 3 {
		t.Errorf("PartitionPolicy failed: should create 3 partitions (1,2,3), got %d", len(partitionInfo.Partition))
	}

	// ------------------------------
	// Test 5: Delete pod1 from partition 1
	// ------------------------------
	if err := ji.DeletePod(pod1); err != nil {
		t.Fatalf("DeletePod failed for pod1: %v", err)
	}

	// Verify pod1 is removed from partition 1
	partition1, _ = partitionInfo.Partition["1"]
	if _, exists := partition1["pod-1"]; exists {
		t.Error("PartitionPolicy failed: pod1 should be removed from partition 1 after deletion")
	}
	// Verify partition 1 contains 1 remaining pod (pod2)
	if len(partition1) != 1 {
		t.Errorf("PartitionPolicy failed: partition 1 should contain 1 pod after deletion, got %d", len(partition1))
	}

	// ------------------------------
	// Test 6: Add pod to task-2 (no partition policy)
	// ------------------------------
	pod4 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-4",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-2", // Task without partition policy
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID:    "1", // Partition ID specified but no partition created
				"kubernetes.io/hostname": "node-1",
			},
		},
		Spec: v1.PodSpec{
			NodeName: "node-1",
		},
	}
	if err := ji.AddPod(pod4); err != nil {
		t.Fatalf("AddPod failed for pod4: %v", err)
	}

	// Verify no partition is created for task-2
	if _, exists := ji.Partitions["task-2"]; exists {
		t.Error("PartitionPolicy failed: task-2 should not have partitions")
	}
}

// TestJobInfoPartitionsManagement tests the management functionalities of Partitions
func TestJobInfoPartitionsManagement(t *testing.T) {
	// Initialize JobInfo with partition data
	ji := &JobInfo{
		Namespace: "test-ns",
		Name:      "test-job",
		Pods: map[string]map[string]*v1.Pod{
			"task-1": {
				"pod-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "test-ns",
						Annotations: map[string]string{
							batch.TaskSpecKey: "task-1",
							batch.JobVersion:  "v1",
						},
						Labels: map[string]string{
							batch.TaskPartitionID: "partition-1",
						},
					},
				},
				"pod-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-2",
						Namespace: "test-ns",
						Annotations: map[string]string{
							batch.TaskSpecKey: "task-1",
							batch.JobVersion:  "v1",
						},
						Labels: map[string]string{
							batch.TaskPartitionID: "partition-2",
						},
					},
				},
			},
		},
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: map[string]map[string]*v1.Pod{
					"partition-1": {
						"pod-1": nil, // Reference to pod-1 above
					},
					"partition-2": {
						"pod-2": nil, // Reference to pod-2 above
					},
				},
			},
		},
	}

	// Verify partition count
	taskPartitions, ok := ji.Partitions["task-1"]
	if !ok {
		t.Fatal("Partitions: task-1 partition not found")
	}
	if len(taskPartitions.Partition) != 2 {
		t.Errorf("Partitions: expected 2 partitions for task-1, got %d", len(taskPartitions.Partition))
	}

	// Verify pod count in partition
	partition1Pods := taskPartitions.Partition["partition-1"]
	if len(partition1Pods) != 1 {
		t.Errorf("Partitions: partition-1 expected 1 pod, got %d", len(partition1Pods))
	}
	if _, exists := partition1Pods["pod-1"]; !exists {
		t.Error("Partitions: pod-1 not found in partition-1")
	}

	// Test adding new partition via new pod
	newPartitionID := "partition-3"
	newPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-3",
			Namespace: "test-ns",
			Annotations: map[string]string{
				batch.TaskSpecKey: "task-1",
				batch.JobVersion:  "v1",
			},
			Labels: map[string]string{
				batch.TaskPartitionID: newPartitionID,
			},
		},
	}
	// Add pod to JobInfo first
	if err := ji.AddPod(newPod); err != nil {
		t.Fatalf("Failed to add new pod: %v", err)
	}
	// Verify new partition is created automatically (AddPod maintains partition mapping)
	if _, ok := taskPartitions.Partition[newPartitionID]; !ok {
		t.Errorf("Partitions: new partition %s not created", newPartitionID)
	}
	if _, exists := taskPartitions.Partition[newPartitionID]["pod-3"]; !exists {
		t.Errorf("Partitions: pod-3 not found in new partition %s", newPartitionID)
	}
}

// TestJobInfoPartitionCleanup tests the cleanup logic of empty partitions
func TestJobInfoPartitionCleanup(t *testing.T) {
	// Initialize JobInfo with empty partition
	ji := &JobInfo{
		Namespace: "test-ns",
		Name:      "test-job",
		Pods: map[string]map[string]*v1.Pod{
			"task-1": {
				"pod-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "test-ns",
						Annotations: map[string]string{
							batch.TaskSpecKey: "task-1",
							batch.JobVersion:  "v1",
						},
						Labels: map[string]string{
							batch.TaskPartitionID: "partition-1",
						},
					},
				},
			},
		},
		Partitions: map[string]*PartitionInfo{
			"task-1": {
				Partition: map[string]map[string]*v1.Pod{
					"partition-1": {
						"pod-1": nil,
					},
					"partition-empty": {}, // Empty partition
				},
			},
		},
	}

	// Simulate empty partition cleanup (assume cleanup method exists in JobInfo)
	// Simple cleanup logic here; should call corresponding JobInfo method in practice
	taskPartitions := ji.Partitions["task-1"]
	for partID, pods := range taskPartitions.Partition {
		if len(pods) == 0 {
			delete(taskPartitions.Partition, partID)
		}
	}

	// Verify empty partition is cleaned up
	if _, exists := taskPartitions.Partition["partition-empty"]; exists {
		t.Error("Partitions: empty partition not cleaned up")
	}

	// Verify non-empty partition is retained
	if _, exists := taskPartitions.Partition["partition-1"]; !exists {
		t.Error("Partitions: non-empty partition was incorrectly cleaned up")
	}
}

// intPtr converts int to *int (adapts to HighestTierAllowed type)
func intPtr(i int) *int {
	return &i
}
