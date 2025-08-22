package cputhrottle

import (
	"os"
	"path"
	"strconv"
	"sync"
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
	"volcano.sh/volcano/pkg/config"
)

const (
	cpuQuotaFileName = "cpu.cfs_quota_us"
)

func TestCPUThrottleHandler_Handle(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		event         interface{}
		pods          []*v1.Pod
		initialQuotas map[string]int64
		stepPercent   int
		minPercent    int
		prepare       func(t *testing.T, tmpDir string, pods []*v1.Pod, quotas map[string]int64)
		validate      func(t *testing.T, tmpDir string, handler *CPUThrottleHandler, pods []*v1.Pod)
		wantErr       bool
	}{
		{
			name: "non-cpu resource event",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceMemory,
				Action:   "start",
			},
			wantErr: false,
		},
		{
			name: "start throttling - step down CPU quota",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceCPU,
				Action:   "start",
				Usage:    80,
			},
			pods: []*v1.Pod{
				buildThrottleablePod("pod1", "uid1", "2", "1", v1.PodQOSBurstable),       // 2核limit, 1核request
				buildThrottleablePod("pod2", "uid2", "1", "", v1.PodQOSBestEffort),       // 1核limit, no request
				buildNonThrottleablePod("pod3", "uid3", "1", "500m", v1.PodQOSBurstable), // QoS level >= 0
			},
			initialQuotas: map[string]int64{
				"uid1": 200000, // 2 cores
				"uid2": 100000, // 1 core
				"uid3": 100000, // 1 core
			},
			stepPercent: 10,
			minPercent:  20,
			prepare:     prepareCgroupFiles,
			validate: func(t *testing.T, tmpDir string, handler *CPUThrottleHandler, pods []*v1.Pod) {
				// pod1: should be throttled from 200000 to 180000 (90%)
				quota1 := readCgroupQuota(t, tmpDir, pods[0])
				assert.Equal(t, int64(180000), quota1, "pod1 should be throttled to 90% of original")
				assert.True(t, handler.throttlingActive[string(pods[0].UID)], "pod1 should be marked as throttled")

				// pod2: should be throttled from 100000 to 90000 (90%)
				quota2 := readCgroupQuota(t, tmpDir, pods[1])
				assert.Equal(t, int64(90000), quota2, "pod2 should be throttled to 90% of original")
				assert.True(t, handler.throttlingActive[string(pods[1].UID)], "pod2 should be marked as throttled")

				// pod3: should not be throttled (QoS level >= 0)
				quota3 := readCgroupQuota(t, tmpDir, pods[2])
				assert.Equal(t, int64(100000), quota3, "pod3 should not be throttled")
				assert.False(t, handler.throttlingActive[string(pods[2].UID)], "pod3 should not be marked as throttled")
			},
			wantErr: false,
		},
		{
			name: "multiple throttling steps to minimum quota",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceCPU,
				Action:   "start",
			},
			pods: []*v1.Pod{
				buildThrottleablePod("pod1", "uid1", "1", "", v1.PodQOSBestEffort), // 1 core, no request
			},
			initialQuotas: map[string]int64{
				"uid1": 30000, // Already throttled to 30% of original 100000
			},
			stepPercent: 40,
			minPercent:  20,
			prepare:     prepareCgroupFiles,
			validate: func(t *testing.T, tmpDir string, handler *CPUThrottleHandler, pods []*v1.Pod) {
				// Should hit minimum quota (20% of 100000 = 20000)
				quota := readCgroupQuota(t, tmpDir, pods[0])
				expectedMin := int64(20000) // 20% of original 100000
				assert.Equal(t, expectedMin, quota, "should hit minimum quota protection")
				assert.True(t, handler.throttlingActive[string(pods[0].UID)], "should be marked as throttled")
			},
			wantErr: false,
		},
		{
			name: "stop throttling - restore original quota",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceCPU,
				Action:   "stop",
			},
			pods: []*v1.Pod{
				buildThrottleablePod("pod1", "uid1", "2", "1", v1.PodQOSBurstable),
				buildThrottleablePod("pod2", "uid2", "1", "", v1.PodQOSBestEffort),
			},
			initialQuotas: map[string]int64{
				"uid1": 150000, // Currently throttled
				"uid2": 80000,  // Currently throttled
			},
			stepPercent: 10,
			minPercent:  20,
			prepare: func(t *testing.T, tmpDir string, pods []*v1.Pod, quotas map[string]int64) {
				prepareCgroupFiles(t, tmpDir, pods, quotas)
				// Pre-mark pods as throttled
			},
			validate: func(t *testing.T, tmpDir string, handler *CPUThrottleHandler, pods []*v1.Pod) {
				// Both pods should be restored to original quota from Pod spec
				quota1 := readCgroupQuota(t, tmpDir, pods[0])
				assert.Equal(t, int64(200000), quota1, "pod1 should be restored to original 2 cores")
				assert.False(t, handler.throttlingActive[string(pods[0].UID)], "pod1 should not be marked as throttled")

				quota2 := readCgroupQuota(t, tmpDir, pods[1])
				assert.Equal(t, int64(100000), quota2, "pod2 should be restored to original 1 core")
				assert.False(t, handler.throttlingActive[string(pods[1].UID)], "pod2 should not be marked as throttled")
			},
			wantErr: false,
		},
		{
			name: "unknown action",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceCPU,
				Action:   "unknown",
			},
			wantErr: true,
		},
		{
			name: "handle cgroup file not exist gracefully",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceCPU,
				Action:   "start",
			},
			pods: []*v1.Pod{
				buildThrottleablePod("pod1", "uid1", "1", "", v1.PodQOSBestEffort),
			},
			initialQuotas: map[string]int64{},
			stepPercent:   10,
			minPercent:    20,
			prepare:       func(t *testing.T, tmpDir string, pods []*v1.Pod, quotas map[string]int64) {}, // No preparation
			validate: func(t *testing.T, tmpDir string, handler *CPUThrottleHandler, pods []*v1.Pod) {
				// Should not crash and should not mark as throttled
				assert.False(t, handler.throttlingActive[string(pods[0].UID)], "should not be marked as throttled when cgroup missing")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create handler
			cfg := &config.Configuration{}
			cgroupMgr := cgroup.NewCgroupManager("cgroupfs", tmpDir, "")

			handler := &CPUThrottleHandler{
				BaseHandle: &base.BaseHandle{
					Name:   string(features.CPUThrottleFeature),
					Config: cfg,
				},
				mutex:               sync.RWMutex{},
				cgroupMgr:           cgroupMgr,
				getPodsFunc:         func() ([]*v1.Pod, error) { return tt.pods, nil },
				throttlingActive:    make(map[string]bool),
				throttleStepPercent: tt.stepPercent,
				minCPUQuotaPercent:  tt.minPercent,
			}

			// Prepare test environment
			if tt.prepare != nil {
				tt.prepare(t, tmpDir, tt.pods, tt.initialQuotas)
			}

			// Pre-mark pods as throttled for stop tests
			if tt.event.(framework.NodeCPUThrottleEvent).Action == "stop" {
				for _, pod := range tt.pods {
					if isThrottleablePod(pod) {
						handler.throttlingActive[string(pod.UID)] = true
					}
				}
			}

			// Execute test
			err := handler.Handle(tt.event)

			// Validate result
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, tmpDir, handler, tt.pods)
				}
			}
		})
	}
}

func TestCalculateMinCPUQuota(t *testing.T) {
	tests := []struct {
		name        string
		pod         *v1.Pod
		minPercent  int
		expectedMin int64
		description string
	}{
		{
			name:        "pod with only CPU limits",
			pod:         buildThrottleablePod("pod1", "uid1", "2", "", v1.PodQOSBestEffort),
			minPercent:  20,
			expectedMin: 40000, // 20% of 2 cores = 40000
			description: "should use percentage of CPU limits",
		},
		{
			name:        "pod without resource specs",
			pod:         buildPodWithoutResources("pod1", "uid1", v1.PodQOSBestEffort),
			minPercent:  20,
			expectedMin: 20000, // 20% of default 1 core = 20000
			description: "should use percentage of default quota",
		},
		{
			name:        "ensure absolute minimum",
			pod:         buildThrottleablePod("pod1", "uid1", "100m", "", v1.PodQOSBestEffort),
			minPercent:  20,
			expectedMin: 20000, // absolute minimum (20% of 1 core)
			description: "should enforce absolute minimum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &CPUThrottleHandler{
				minCPUQuotaPercent: tt.minPercent,
			}

			result := handler.calculateMinCPUQuota(tt.pod)
			assert.Equal(t, tt.expectedMin, result, tt.description)
		})
	}
}

// Helper functions

func buildThrottleablePod(name, uid, cpuLimit, cpuRequest string, qosClass v1.PodQOSClass) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(uid),
			Annotations: map[string]string{
				"volcano.sh/qos-level": "BE", // Makes QoS level < 0
			},
		},
		Status: v1.PodStatus{
			QOSClass: qosClass,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "container1",
					Resources: v1.ResourceRequirements{
						Limits:   v1.ResourceList{},
						Requests: v1.ResourceList{},
					},
				},
			},
		},
	}

	if cpuLimit != "" {
		pod.Spec.Containers[0].Resources.Limits[v1.ResourceCPU] = resource.MustParse(cpuLimit)
	}
	if cpuRequest != "" {
		pod.Spec.Containers[0].Resources.Requests[v1.ResourceCPU] = resource.MustParse(cpuRequest)
	}

	return pod
}

func buildNonThrottleablePod(name, uid, cpuLimit, cpuRequest string, qosClass v1.PodQOSClass) *v1.Pod {
	pod := buildThrottleablePod(name, uid, cpuLimit, cpuRequest, qosClass)
	// Remove the BE annotation to make QoS level >= 0
	delete(pod.Annotations, "volcano.sh/qos-level")
	return pod
}

func buildPodWithoutResources(name, uid string, qosClass v1.PodQOSClass) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  types.UID(uid),
			Annotations: map[string]string{
				"volcano.sh/qos-level": "BE",
			},
		},
		Status: v1.PodStatus{
			QOSClass: qosClass,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:      "container1",
					Resources: v1.ResourceRequirements{},
				},
			},
		},
	}
}

func isThrottleablePod(pod *v1.Pod) bool {
	return pod.Annotations != nil && pod.Annotations["volcano.sh/qos-level"] == "BE"
}

func prepareCgroupFiles(t *testing.T, tmpDir string, pods []*v1.Pod, quotas map[string]int64) {
	for _, pod := range pods {
		quota, exists := quotas[string(pod.UID)]
		if !exists {
			continue
		}

		var cgroupPath string
		switch pod.Status.QOSClass {
		case v1.PodQOSBestEffort:
			cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "besteffort", "pod"+string(pod.UID))
		case v1.PodQOSBurstable:
			cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "burstable", "pod"+string(pod.UID))
		case v1.PodQOSGuaranteed:
			cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "pod"+string(pod.UID))
		default:
			cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "pod"+string(pod.UID))
		}

		assert.NoError(t, os.MkdirAll(cgroupPath, 0755))
		quotaFile := path.Join(cgroupPath, cpuQuotaFileName)
		assert.NoError(t, os.WriteFile(quotaFile, []byte(strconv.FormatInt(quota, 10)), 0644))
	}
}

func readCgroupQuota(t *testing.T, tmpDir string, pod *v1.Pod) int64 {
	var cgroupPath string
	switch pod.Status.QOSClass {
	case v1.PodQOSBestEffort:
		cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "besteffort", "pod"+string(pod.UID))
	case v1.PodQOSBurstable:
		cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "burstable", "pod"+string(pod.UID))
	case v1.PodQOSGuaranteed:
		cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "pod"+string(pod.UID))
	default:
		cgroupPath = path.Join(tmpDir, "cpu", "kubepods", "pod"+string(pod.UID))
	}

	quotaFile := path.Join(cgroupPath, cpuQuotaFileName)
	data, err := os.ReadFile(quotaFile)
	if err != nil {
		return -1 // File not exist
	}

	quota, err := strconv.ParseInt(string(data), 10, 64)
	assert.NoError(t, err)
	return quota
}
