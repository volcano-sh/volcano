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

package resources

import (
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/events/handlers/base"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/file"
)

func TestResourcesHandle_Handle(t *testing.T) {
	tmpDir := t.TempDir()
	containerID1 := "65a6099d"
	containerID2 := "13b017b7"
	tests := []struct {
		name      string
		cgroupMgr cgroup.CgroupManager
		event     interface{}
		prepare   func()
		post      func() map[string]string
		wantErr   bool
		wantVal   map[string]string
	}{
		{
			name:      "illegal pod event, return err",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event:     &framework.NodeResourceEvent{},
			wantErr:   true,
		},
		{
			name:      "set correctly",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event: framework.PodEvent{
				UID:      "uid1",
				QoSLevel: -1,
				QoSClass: "Burstable",
				Pod:      buildPodWithContainerID("p1", "uid1", containerID1, containerID2),
			},
			prepare: func() {
				prepare(t, tmpDir, "uid1", containerID1, containerID2)
			},
			post: func() map[string]string {
				return file.ReadBatchFromFile([]string{
					path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/65a6099d/cpu.shares"),
					path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/65a6099d/cpu.cfs_quota_us"),
					path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/13b017b7/cpu.shares"),
					path.Join(tmpDir, "memory/kubepods/burstable/poduid1/13b017b7/memory.limit_in_bytes"),
					path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/cpu.shares"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				// container1
				path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/65a6099d/cpu.shares"):       "512",
				path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/65a6099d/cpu.cfs_quota_us"): "200000",

				// container2
				path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/13b017b7/cpu.shares"):               "1024",
				path.Join(tmpDir, "memory/kubepods/burstable/poduid1/13b017b7/memory.limit_in_bytes"): "10737418240",

				// pod
				path.Join(tmpDir, "cpu/kubepods/burstable/poduid1/cpu.shares"): "1536",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ResourcesHandle{
				BaseHandle: &base.BaseHandle{
					Name:   string(features.ResourcesFeature),
					Config: nil,
					Active: true,
				},
				cgroupMgr: tt.cgroupMgr,
			}
			if tt.prepare != nil {
				tt.prepare()
			}
			if err := r.Handle(tt.event); (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.post != nil {
				assert.Equal(t, tt.wantVal, tt.post())
			}
		})
	}
}

func TestResourcesHandle_Handle_CgroupV2(t *testing.T) {
	// Set environment variable to force cgroup v2 detection
	originalEnv := os.Getenv("VOLCANO_TEST_CGROUP_VERSION")
	os.Setenv("VOLCANO_TEST_CGROUP_VERSION", "v2")
	defer func() {
		if originalEnv == "" {
			os.Unsetenv("VOLCANO_TEST_CGROUP_VERSION")
		} else {
			os.Setenv("VOLCANO_TEST_CGROUP_VERSION", originalEnv)
		}
	}()

	tmpDir := t.TempDir()
	containerID1 := "65a6099d"
	containerID2 := "13b017b7"
	tests := []struct {
		name      string
		cgroupMgr cgroup.CgroupManager
		event     interface{}
		prepare   func()
		post      func() map[string]string
		wantErr   bool
		wantVal   map[string]string
	}{
		{
			name:      "cgroup v2: set correctly with cpu.weight and cpu.max",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event: framework.PodEvent{
				UID:      "uid2",
				QoSLevel: -1,
				QoSClass: "Burstable",
				Pod:      buildPodWithContainerID("p2", "uid2", containerID1, containerID2),
			},
			prepare: func() {
				prepareV2(t, tmpDir, "uid2", containerID1, containerID2)
			},
			post: func() map[string]string {
				return file.ReadBatchFromFile([]string{
					path.Join(tmpDir, "kubepods/burstable/poduid2/65a6099d/cpu.weight"),
					path.Join(tmpDir, "kubepods/burstable/poduid2/65a6099d/cpu.max"),
					path.Join(tmpDir, "kubepods/burstable/poduid2/13b017b7/cpu.weight"),
					path.Join(tmpDir, "kubepods/burstable/poduid2/13b017b7/memory.max"),
					path.Join(tmpDir, "kubepods/burstable/poduid2/cpu.weight"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				// container1
				path.Join(tmpDir, "kubepods/burstable/poduid2/65a6099d/cpu.weight"): "50",
				path.Join(tmpDir, "kubepods/burstable/poduid2/65a6099d/cpu.max"):    "200000 100000",

				// container2
				path.Join(tmpDir, "kubepods/burstable/poduid2/13b017b7/cpu.weight"): "100",
				path.Join(tmpDir, "kubepods/burstable/poduid2/13b017b7/memory.max"): "10737418240",

				// pod
				path.Join(tmpDir, "kubepods/burstable/poduid2/cpu.weight"): "150",
			},
		},
		{
			name:      "cgroup v2: set correctly with unlimited cpu.max",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event: framework.PodEvent{
				UID:      "uid3",
				QoSLevel: 0,
				QoSClass: "BestEffort",
				Pod:      buildPodWithUnlimitedContainerID("p3", "uid3", containerID1),
			},
			prepare: func() {
				prepareV2(t, tmpDir, "uid3", containerID1, "")
			},
			post: func() map[string]string {
				return file.ReadBatchFromFile([]string{
					path.Join(tmpDir, "kubepods/besteffort/poduid3/65a6099d/cpu.weight"),
					path.Join(tmpDir, "kubepods/besteffort/poduid3/65a6099d/cpu.max"),
					path.Join(tmpDir, "kubepods/besteffort/poduid3/65a6099d/memory.max"),
					path.Join(tmpDir, "kubepods/besteffort/poduid3/cpu.weight"),
					path.Join(tmpDir, "kubepods/besteffort/poduid3/cpu.max"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				// container1
				path.Join(tmpDir, "kubepods/besteffort/poduid3/65a6099d/cpu.weight"): "50",
				path.Join(tmpDir, "kubepods/besteffort/poduid3/65a6099d/cpu.max"):    "max 100000",
				path.Join(tmpDir, "kubepods/besteffort/poduid3/65a6099d/memory.max"): "max",

				// pod
				path.Join(tmpDir, "kubepods/besteffort/poduid3/cpu.weight"): "50",
				path.Join(tmpDir, "kubepods/besteffort/poduid3/cpu.max"):    "max 100000",
			},
		},
		{
			name:      "cgroup v2: mixed resources with partial limits",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", tmpDir, ""),
			event: framework.PodEvent{
				UID:      "uid4",
				QoSLevel: 1,
				QoSClass: "Burstable",
				Pod:      buildPodWithMixedResourcesV2("p4", "uid4", containerID1, containerID2),
			},
			prepare: func() {
				prepareV2(t, tmpDir, "uid4", containerID1, containerID2)
			},
			post: func() map[string]string {
				return file.ReadBatchFromFile([]string{
					path.Join(tmpDir, "kubepods/burstable/poduid4/65a6099d/cpu.weight"),
					path.Join(tmpDir, "kubepods/burstable/poduid4/13b017b7/cpu.weight"),
					path.Join(tmpDir, "kubepods/burstable/poduid4/13b017b7/memory.max"),
					path.Join(tmpDir, "kubepods/burstable/poduid4/cpu.weight"),
				})
			},
			wantErr: false,
			wantVal: map[string]string{
				// container1 (only cpu request, no limits)
				path.Join(tmpDir, "kubepods/burstable/poduid4/65a6099d/cpu.weight"): "50",

				// container2 (cpu request + memory limit)
				path.Join(tmpDir, "kubepods/burstable/poduid4/13b017b7/cpu.weight"): "100",
				path.Join(tmpDir, "kubepods/burstable/poduid4/13b017b7/memory.max"): "5368709120",

				// pod
				path.Join(tmpDir, "kubepods/burstable/poduid4/cpu.weight"): "150",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify we're using cgroup v2
			assert.Equal(t, cgroup.CgroupV2, tt.cgroupMgr.GetCgroupVersion())

			r := &ResourcesHandle{
				BaseHandle: &base.BaseHandle{
					Name:   string(features.ResourcesFeature),
					Config: nil,
					Active: true,
				},
				cgroupMgr: tt.cgroupMgr,
			}
			if tt.prepare != nil {
				tt.prepare()
			}
			if err := r.Handle(tt.event); (err != nil) != tt.wantErr {
				t.Errorf("Handle() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.post != nil {
				assert.Equal(t, tt.wantVal, tt.post())
			}
		})
	}
}

func buildPodWithContainerID(name, uid, containerID1, containerID2 string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(uid),
		},
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name: "container-1",
				Resources: v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						"kubernetes.io/batch-cpu": *resource.NewQuantity(2000, resource.DecimalSI),
					},
					Requests: map[v1.ResourceName]resource.Quantity{
						"kubernetes.io/batch-cpu":    *resource.NewQuantity(500, resource.DecimalSI),
						"kubernetes.io/batch-memory": *resource.NewQuantity(1024, resource.BinarySI),
					},
				},
			},
			{
				Name: "container-2",
				Resources: v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						// limit=10Gi
						"kubernetes.io/batch-memory": *resource.NewQuantity(1024*1024*1024*10, resource.BinarySI),
					},
					Requests: map[v1.ResourceName]resource.Quantity{
						"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
						"kubernetes.io/batch-memory": *resource.NewQuantity(1024*1024*1024*5, resource.BinarySI),
					},
				},
			},
		}},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "container-1",
					ContainerID: fmt.Sprintf("containerd://%s", containerID1),
				},
				{
					Name:        "container-2",
					ContainerID: fmt.Sprintf("docker://%s", containerID2),
				},
			},
		},
	}
}

func prepare(t *testing.T, tmpDir, podUID, containerID1, containerID2 string) {
	containers := []string{containerID1, containerID2}
	cgroupPaths := []string{"cpu.cfs_quota_us", "cpu.shares", "memory.limit_in_bytes"}
	subSystems := []string{"cpu", "memory"}
	for _, c := range containers {
		for _, ss := range subSystems {
			podDir := path.Join(tmpDir, ss, "kubepods", "burstable", "pod"+podUID)
			containerDir := path.Join(podDir, c)
			err := os.MkdirAll(containerDir, 0644)
			assert.NoError(t, err)
			for _, cgrouPath := range cgroupPaths {
				// create pod level cgroup.
				_, err := os.OpenFile(path.Join(podDir, cgrouPath), os.O_RDWR|os.O_CREATE, 0644)
				assert.NoError(t, err)
				// create container level cgroup.
				_, err = os.OpenFile(path.Join(containerDir, cgrouPath), os.O_RDWR|os.O_CREATE, 0644)
				assert.NoError(t, err)
			}
		}
	}
}

// buildPodWithUnlimitedContainerID builds a pod with unlimited resources for cgroup v2 testing
func buildPodWithUnlimitedContainerID(name, uid, containerID string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(uid),
		},
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name: "container-1",
				Resources: v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						// Unlimited CPU and memory limits for cgroup v2 "max" testing
						"kubernetes.io/batch-cpu":    *resource.NewQuantity(0, resource.DecimalSI),
						"kubernetes.io/batch-memory": *resource.NewQuantity(0, resource.BinarySI),
					},
					Requests: map[v1.ResourceName]resource.Quantity{
						"kubernetes.io/batch-cpu":    *resource.NewQuantity(500, resource.DecimalSI),
						"kubernetes.io/batch-memory": *resource.NewQuantity(1024, resource.BinarySI),
					},
				},
			},
		}},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "container-1",
					ContainerID: fmt.Sprintf("containerd://%s", containerID),
				},
			},
		},
	}
}

// buildPodWithMixedResourcesV2 builds a pod with mixed resource scenarios for cgroup v2 testing
func buildPodWithMixedResourcesV2(name, uid, containerID1, containerID2 string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(uid),
		},
		Spec: v1.PodSpec{Containers: []v1.Container{
			{
				Name: "container-1",
				// Only CPU request, no limits
				Resources: v1.ResourceRequirements{
					Requests: map[v1.ResourceName]resource.Quantity{
						"kubernetes.io/batch-cpu": *resource.NewQuantity(500, resource.DecimalSI),
					},
				},
			},
			{
				Name: "container-2",
				Resources: v1.ResourceRequirements{
					Limits: map[v1.ResourceName]resource.Quantity{
						// Only memory limit, no CPU limit
						"kubernetes.io/batch-memory": *resource.NewQuantity(1024*1024*1024*5, resource.BinarySI), // 5Gi
					},
					Requests: map[v1.ResourceName]resource.Quantity{
						"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
						"kubernetes.io/batch-memory": *resource.NewQuantity(1024*1024*1024*2, resource.BinarySI), // 2Gi
					},
				},
			},
		}},
		Status: v1.PodStatus{
			ContainerStatuses: []v1.ContainerStatus{
				{
					Name:        "container-1",
					ContainerID: fmt.Sprintf("containerd://%s", containerID1),
				},
				{
					Name:        "container-2",
					ContainerID: fmt.Sprintf("docker://%s", containerID2),
				},
			},
		},
	}
}

// prepareV2 creates cgroup v2 directory structure and files for testing
func prepareV2(t *testing.T, tmpDir, podUID, containerID1, containerID2 string) {
	containers := []string{}
	if containerID1 != "" {
		containers = append(containers, containerID1)
	}
	if containerID2 != "" {
		containers = append(containers, containerID2)
	}

	cgroupPaths := []string{"cpu.weight", "cpu.max", "memory.max"}

	// Create pod directory structure for different QoS classes
	qosClasses := []string{"burstable", "besteffort", "guaranteed"}

	for _, qos := range qosClasses {
		podDir := path.Join(tmpDir, "kubepods", qos, "pod"+podUID)
		err := os.MkdirAll(podDir, 0755)
		assert.NoError(t, err)

		// create pod level cgroup files
		for _, cgroupPath := range cgroupPaths {
			_, err := os.OpenFile(path.Join(podDir, cgroupPath), os.O_RDWR|os.O_CREATE, 0644)
			assert.NoError(t, err)
		}

		// create container level cgroup files
		for _, c := range containers {
			containerDir := path.Join(podDir, c)
			err := os.MkdirAll(containerDir, 0755)
			assert.NoError(t, err)

			for _, cgroupPath := range cgroupPaths {
				_, err = os.OpenFile(path.Join(containerDir, cgroupPath), os.O_RDWR|os.O_CREATE, 0644)
				assert.NoError(t, err)
			}
		}
	}
}
