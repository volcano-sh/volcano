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
	"volcano.sh/volcano/pkg/config"
)

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
