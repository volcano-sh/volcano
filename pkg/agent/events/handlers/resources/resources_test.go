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
