/*
Copyright 2025 The Volcano Authors.

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

package framework

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func newJobWithPodGroupAnnotations(allocatedHyperNode string, annotations map[string]string) *api.JobInfo {
	return &api.JobInfo{
		AllocatedHyperNode: allocatedHyperNode,
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
				},
			},
		},
	}
}

func TestSyncAllocatedHyperNodeAnnotation(t *testing.T) {
	tests := []struct {
		name               string
		allocatedHyperNode string
		annotations        map[string]string
		wantAnnotationSet  bool
		wantAnnotation     string
	}{
		{
			name:               "scale-in shrinks tier3 to tier2",
			allocatedHyperNode: "sn-a",
			annotations:        map[string]string{api.JobAllocatedHyperNode: "root"},
			wantAnnotationSet:  true,
			wantAnnotation:     "sn-a",
		},
		{
			name:               "field already matches annotation",
			allocatedHyperNode: "sn-a",
			annotations:        map[string]string{api.JobAllocatedHyperNode: "sn-a"},
			wantAnnotationSet:  true,
			wantAnnotation:     "sn-a",
		},
		{
			name:               "annotation written when map already present but key missing",
			allocatedHyperNode: "sn-a",
			annotations:        map[string]string{"other": "value"},
			wantAnnotationSet:  true,
			wantAnnotation:     "sn-a",
		},
		{
			name:               "annotation written when annotations map is nil",
			allocatedHyperNode: "sn-a",
			annotations:        nil,
			wantAnnotationSet:  true,
			wantAnnotation:     "sn-a",
		},
		{
			name:               "empty field clears stale annotation",
			allocatedHyperNode: "",
			annotations:        map[string]string{api.JobAllocatedHyperNode: "root"},
			wantAnnotationSet:  false,
		},
		{
			name:               "empty field with nil annotations is a no-op",
			allocatedHyperNode: "",
			annotations:        nil,
			wantAnnotationSet:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := newJobWithPodGroupAnnotations(tt.allocatedHyperNode, tt.annotations)

			syncAllocatedHyperNodeAnnotation(job)

			got, ok := job.PodGroup.GetAnnotations()[api.JobAllocatedHyperNode]
			if ok != tt.wantAnnotationSet {
				t.Fatalf("annotation present = %v, want %v (value=%q)", ok, tt.wantAnnotationSet, got)
			}
			if tt.wantAnnotationSet && got != tt.wantAnnotation {
				t.Fatalf("annotation = %q, want %q", got, tt.wantAnnotation)
			}
		})
	}
}

func TestSyncAllocatedHyperNodeAnnotationNilPodGroup(t *testing.T) {
	job := &api.JobInfo{AllocatedHyperNode: "sn-a"}
	// Should not panic when PodGroup is nil.
	syncAllocatedHyperNodeAnnotation(job)
}
