package cputhrottle

import (
	"os"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
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
		name     string
		event    interface{}
		prepare  func(t *testing.T, cgroupPath string)
		validate func(t *testing.T, handler *CPUThrottleHandler, quotaFile string)
		wantErr  bool
	}{
		{
			name: "non-cpu resource event",
			event: framework.NodeCPUThrottleEvent{
				Resource: v1.ResourceMemory,
			},
			wantErr: false,
		},
		{
			name: "apply quota to BE root cgroup",
			event: framework.NodeCPUThrottleEvent{
				Resource:      v1.ResourceCPU,
				CPUQuotaMilli: 500,
			},
			prepare: func(t *testing.T, cgroupPath string) {
				assert.NoError(t, os.MkdirAll(cgroupPath, 0755))
				quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)
				assert.NoError(t, os.WriteFile(quotaFile, []byte("10000"), 0644))
			},
			validate: func(t *testing.T, handler *CPUThrottleHandler, quotaFile string) {
				content, err := os.ReadFile(quotaFile)
				assert.NoError(t, err)
				assert.Equal(t, "50000", strings.TrimSpace(string(content))[0])
				handler.mutex.RLock()
				assert.True(t, handler.throttlingActive)
				handler.mutex.RUnlock()
			},
			wantErr: false,
		},
		{
			name: "zero quota still writes minimal value",
			event: framework.NodeCPUThrottleEvent{
				Resource:      v1.ResourceCPU,
				CPUQuotaMilli: -1,
			},
			prepare: func(t *testing.T, cgroupPath string) {
				assert.NoError(t, os.MkdirAll(cgroupPath, 0755))
				quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)
				assert.NoError(t, os.WriteFile(quotaFile, []byte("10000"), 0644))
			},
			validate: func(t *testing.T, handler *CPUThrottleHandler, quotaFile string) {
				content, err := os.ReadFile(quotaFile)
				assert.NoError(t, err)
				assert.Equal(t, "-1", strings.TrimSpace(string(content))[0])
				handler.mutex.RLock()
				assert.True(t, handler.throttlingActive)
				handler.mutex.RUnlock()
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Configuration{}
			cgroupMgr := cgroup.NewCgroupManager("cgroupfs", tmpDir, "")
			cgroupPath, err := cgroupMgr.GetQoSCgroupPath(v1.PodQOSBestEffort, cgroup.CgroupCpuSubsystem)
			assert.NoError(t, err)
			quotaFile := path.Join(cgroupPath, cgroup.CPUQuotaTotalFile)

			handler := &CPUThrottleHandler{
				BaseHandle: &base.BaseHandle{
					Name:   string(features.CPUThrottleFeature),
					Config: cfg,
				},
				mutex:            sync.RWMutex{},
				cgroupMgr:        cgroupMgr,
				throttlingActive: true,
			}

			if tt.prepare != nil {
				tt.prepare(t, cgroupPath)
			}

			err = handler.Handle(tt.event)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, handler, quotaFile)
				}
			}
		})
	}
}
