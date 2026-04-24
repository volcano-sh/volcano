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

package networkqos

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	"volcano.sh/volcano/pkg/agent/events/framework"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/agent/utils/exec"
	"volcano.sh/volcano/pkg/config"
)

// mockExecutor is a mock implementation of exec.ExecInterface for testing bwmcli calls.
type mockExecutor struct {
	lastCmd string
	err     error
}

func (m *mockExecutor) CommandContext(ctx context.Context, cmd string) (string, error) {
	m.lastCmd = cmd
	return "", m.err
}

func TestNeworkQoSHandle_Handle(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTempCgroup")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)

	testCases := []struct {
		name             string
		cgroupMgr        cgroup.CgroupManager
		cgroupSubpath    string
		recorder         record.EventRecorder
		event            framework.PodEvent
		expectedErr      bool
		expectedQoSLevel string
	}{
		{
			name:          "Burstable pod event && CgroupDriver=Cgroupfs",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/net_cls/kubepods/burstable",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000001",
				QoSLevel: -1,
				QoSClass: "Burstable",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test1",
						Namespace: "default",
					},
				},
			},
			expectedErr:      false,
			expectedQoSLevel: "4294967295",
		},

		{
			name:          "Guaranteed pod event && CgroupDriver=Cgroupfs",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/net_cls/kubepods",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000002",
				QoSLevel: -1,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test2",
						Namespace: "default",
					},
				},
			},
			expectedErr:      false,
			expectedQoSLevel: "4294967295",
		},

		{
			name:          "BestEffort pod event && CgroupDriver=Cgroupfs",
			cgroupMgr:     cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			cgroupSubpath: "cgroup/net_cls/kubepods/besteffort",
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000003",
				QoSLevel: -1,
				QoSClass: "BestEffort",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test3",
						Namespace: "default",
					},
				},
			},
			expectedErr:      false,
			expectedQoSLevel: "4294967295",
		},
	}

	for _, tc := range testCases {
		fakeCgroupPath := path.Join(dir, tc.cgroupSubpath, "pod"+string(tc.event.UID))
		err = os.MkdirAll(fakeCgroupPath, 0750)
		assert.Equal(t, err == nil, true, tc.name)

		tmpFile := path.Join(fakeCgroupPath, "net_cls.classid")
		if err = os.WriteFile(tmpFile, []byte("0"), 0660); err != nil {
			assert.Equal(t, nil, err, tc.name)
		}

		fakeClient := fake.NewSimpleClientset(tc.event.Pod)
		informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
		informerFactory.Core().V1().Pods().Informer()
		informerFactory.Start(context.TODO().Done())
		if !cache.WaitForNamedCacheSync("", context.TODO().Done(), informerFactory.Core().V1().Pods().Informer().HasSynced) {
			assert.Equal(t, nil, err, tc.name)
		}

		cfg := &config.Configuration{
			InformerFactory: &config.InformerFactory{
				K8SInformerFactory: informerFactory,
			},
			GenericConfiguration: &config.VolcanoAgentConfiguration{
				KubeClient: fakeClient,
				Recorder:   record.NewFakeRecorder(100),
			},
		}

		h := NewNetworkQoSHandle(cfg, nil, tc.cgroupMgr)
		handleErr := h.Handle(tc.event)
		fmt.Println(handleErr)
		assert.Equal(t, tc.expectedErr, handleErr != nil, tc.name)

		actualLevel, readErr := os.ReadFile(tmpFile)
		assert.Equal(t, nil, readErr, tc.name)
		assert.Equal(t, tc.expectedQoSLevel, string(actualLevel), tc.name)
	}
}

func TestNetworkQoSHandle_HandleV2(t *testing.T) {
	// Set cgroup version to v2 for testing
	os.Setenv("VOLCANO_TEST_CGROUP_VERSION", "v2")
	defer os.Unsetenv("VOLCANO_TEST_CGROUP_VERSION")

	dir, err := os.MkdirTemp("/tmp", "MkdirTempCgroupV2")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
	}()
	assert.NoError(t, err)

	testCases := []struct {
		name            string
		cgroupMgr       cgroup.CgroupManager
		event           framework.PodEvent
		mockExecErr     error
		expectedErr     bool
		expectedCmdPart string
		expectedLevel   string
	}{
		{
			name:      "v2: Burstable offline pod (QoSLevel=-1) calls bwmcli with -1",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000011",
				QoSLevel: -1,
				QoSClass: "Burstable",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v2-test1",
						Namespace: "default",
					},
				},
			},
			mockExecErr:     nil,
			expectedErr:     false,
			expectedCmdPart: "bwmcli -s",
			expectedLevel:   "-1",
		},
		{
			name:      "v2: Guaranteed online pod (QoSLevel=0) calls bwmcli with 0",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000012",
				QoSLevel: 0,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v2-test2",
						Namespace: "default",
					},
				},
			},
			mockExecErr:     nil,
			expectedErr:     false,
			expectedCmdPart: "bwmcli -s",
			expectedLevel:   "0",
		},
		{
			name:      "v2: BestEffort offline pod (QoSLevel=-1) calls bwmcli with -1",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000013",
				QoSLevel: -1,
				QoSClass: "BestEffort",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v2-test3",
						Namespace: "default",
					},
				},
			},
			mockExecErr:     nil,
			expectedErr:     false,
			expectedCmdPart: "bwmcli -s",
			expectedLevel:   "-1",
		},
		{
			name:      "v2: QoSLevel=2 (LC) normalized to 0",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000014",
				QoSLevel: 2,
				QoSClass: "Guaranteed",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v2-test4",
						Namespace: "default",
					},
				},
			},
			mockExecErr:     nil,
			expectedErr:     false,
			expectedCmdPart: "bwmcli -s",
			expectedLevel:   "0",
		},
		{
			name:      "v2: QoSLevel=-2 (abnormal) normalized to -1",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000015",
				QoSLevel: -2,
				QoSClass: "BestEffort",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v2-test5",
						Namespace: "default",
					},
				},
			},
			mockExecErr:     nil,
			expectedErr:     false,
			expectedCmdPart: "bwmcli -s",
			expectedLevel:   "-1",
		},
		{
			name:      "v2: bwmcli execution failure returns error",
			cgroupMgr: cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), ""),
			event: framework.PodEvent{
				UID:      "00000000-1111-2222-3333-000000000016",
				QoSLevel: -1,
				QoSClass: "Burstable",
				Pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v2-test6",
						Namespace: "default",
					},
				},
			},
			mockExecErr:     fmt.Errorf("bwmcli: command not found"),
			expectedErr:     true,
			expectedCmdPart: "bwmcli -s",
			expectedLevel:   "-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup mock executor
			mockExec := &mockExecutor{err: tc.mockExecErr}
			exec.SetExecutor(mockExec)
			defer exec.SetExecutor(nil)

			// Create v2 unified cgroup directories (no net_cls subsystem in path)
			var cgroupSubpath string
			switch tc.event.QoSClass {
			case "Burstable":
				cgroupSubpath = "cgroup/kubepods/burstable"
			case "BestEffort":
				cgroupSubpath = "cgroup/kubepods/besteffort"
			default:
				cgroupSubpath = "cgroup/kubepods"
			}
			fakeCgroupPath := path.Join(dir, cgroupSubpath, "pod"+string(tc.event.UID))
			err := os.MkdirAll(fakeCgroupPath, 0750)
			assert.NoError(t, err, tc.name)

			fakeClient := fake.NewSimpleClientset(tc.event.Pod)
			informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
			informerFactory.Core().V1().Pods().Informer()
			informerFactory.Start(context.TODO().Done())
			cache.WaitForNamedCacheSync("", context.TODO().Done(), informerFactory.Core().V1().Pods().Informer().HasSynced)

			cfg := &config.Configuration{
				InformerFactory: &config.InformerFactory{
					K8SInformerFactory: informerFactory,
				},
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					KubeClient: fakeClient,
					Recorder:   record.NewFakeRecorder(100),
				},
			}

			h := NewNetworkQoSHandle(cfg, nil, tc.cgroupMgr)
			handleErr := h.Handle(tc.event)

			if tc.expectedErr {
				assert.Error(t, handleErr, tc.name)
			} else {
				assert.NoError(t, handleErr, tc.name)
			}

			// Verify bwmcli command was called with correct arguments
			assert.Contains(t, mockExec.lastCmd, tc.expectedCmdPart, tc.name)
			assert.Contains(t, mockExec.lastCmd, tc.expectedLevel, tc.name)

			// Verify the command contains the cgroup path (unified, no net_cls)
			assert.Contains(t, mockExec.lastCmd, fakeCgroupPath, tc.name)

			// Verify no net_cls.classid file was written in v2 mode
			netClsFile := path.Join(fakeCgroupPath, "net_cls.classid")
			_, statErr := os.Stat(netClsFile)
			assert.True(t, os.IsNotExist(statErr), "net_cls.classid should not exist in cgroup v2 mode")
		})
	}
}

func TestNetworkQoSHandle_SkipWithBandwidthAnnotation(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTempCgroupSkip")
	defer func() {
		os.RemoveAll(dir)
	}()
	assert.NoError(t, err)

	testCases := []struct {
		name        string
		annotations map[string]string
	}{
		{
			name: "skip when ingress-bandwidth annotation exists",
			annotations: map[string]string{
				"kubernetes.io/ingress-bandwidth": "10M",
			},
		},
		{
			name: "skip when egress-bandwidth annotation exists",
			annotations: map[string]string{
				"kubernetes.io/egress-bandwidth": "10M",
			},
		},
		{
			name: "skip when both bandwidth annotations exist",
			annotations: map[string]string{
				"kubernetes.io/ingress-bandwidth": "10M",
				"kubernetes.io/egress-bandwidth":  "10M",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "skip-pod",
					Namespace:   "default",
					Annotations: tc.annotations,
				},
			}

			fakeClient := fake.NewSimpleClientset(pod)
			informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
			informerFactory.Core().V1().Pods().Informer()
			informerFactory.Start(context.TODO().Done())
			cache.WaitForNamedCacheSync("", context.TODO().Done(), informerFactory.Core().V1().Pods().Informer().HasSynced)

			cfg := &config.Configuration{
				InformerFactory: &config.InformerFactory{
					K8SInformerFactory: informerFactory,
				},
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					KubeClient: fakeClient,
					Recorder:   record.NewFakeRecorder(100),
				},
			}

			cgroupMgr := cgroup.NewCgroupManager("cgroupfs", path.Join(dir, "cgroup"), "")
			h := NewNetworkQoSHandle(cfg, nil, cgroupMgr)

			event := framework.PodEvent{
				UID:      "skip-uid",
				QoSLevel: -1,
				QoSClass: "Burstable",
				Pod:      pod,
			}

			handleErr := h.Handle(event)
			assert.NoError(t, handleErr, tc.name)
		})
	}
}
