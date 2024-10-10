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

package config

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/config/source"
	"volcano.sh/volcano/pkg/agent/config/utils"
)

var oldCfg = `
{
    "globalConfig":{
        "cpuBurstConfig":{
            "enable":true
        },
        "networkQosConfig":{
            "enable":true,
            "onlineBandwidthWatermarkPercent":80,
            "offlineLowBandwidthPercent":10,
            "offlineHighBandwidthPercent":40,
            "qosCheckInterval": 10000000
        }
    }
}
`

var oldCfgWithSelector = `
{
    "globalConfig":{
        "cpuBurstConfig":{
            "enable":true
        },
        "networkQosConfig":{
            "enable":true,
            "onlineBandwidthWatermarkPercent":80,
            "offlineLowBandwidthPercent":10,
            "offlineHighBandwidthPercent":40,
            "qosCheckInterval": 10000000
        },
        "overSubscriptionConfig":{
            "enable":true,
            "overSubscriptionTypes":"cpu,memory"
        },
        "evictingConfig":{
            "evictingCPUHighWatermark":80,
            "evictingMemoryHighWatermark":60,
            "evictingCPULowWatermark":30,
            "evictingMemoryLowWatermark":30
        }
    },
	"nodesConfig": [
		{
			"selector": {
				"matchLabels": {
					"label-key": "label-value"
				}
			},
			"evictingConfig": {
				"evictingCPUHighWatermark": 60
			}
		}
	]
}
`

func makeConfigMap(data string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			Name:      "volcano-agent-configuration"},
		Data: map[string]string{
			utils.ColocationConfigKey: data,
		},
	}
}

var agentPod = &v1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "kube-system",
		Name:      "volcano-agent-pod"},
}

func defaultCfg() *api.VolcanoAgentConfig {
	return &api.VolcanoAgentConfig{GlobalConfig: &api.ColocationConfig{
		CPUBurstConfig: &api.CPUBurst{Enable: utilpointer.Bool(true)},
		NetworkQosConfig: &api.NetworkQos{
			Enable:                          utilpointer.Bool(true),
			OnlineBandwidthWatermarkPercent: utilpointer.Int(utils.DefaultOnlineBandwidthWatermarkPercent),
			OfflineLowBandwidthPercent:      utilpointer.Int(utils.DefaultOfflineLowBandwidthPercent),
			OfflineHighBandwidthPercent:     utilpointer.Int(utils.DefaultOfflineHighBandwidthPercent),
			QoSCheckInterval:                utilpointer.Int(utils.DefaultNetworkQoSInterval),
		},
		OverSubscriptionConfig: &api.OverSubscription{
			Enable:                utilpointer.Bool(true),
			OverSubscriptionTypes: utilpointer.String(utils.DefaultOverSubscriptionTypes),
		},
		EvictingConfig: &api.Evicting{
			EvictingCPUHighWatermark:    utilpointer.Int(utils.DefaultEvictingCPUHighWatermark),
			EvictingMemoryHighWatermark: utilpointer.Int(utils.DefaultEvictingMemoryHighWatermark),
			EvictingCPULowWatermark:     utilpointer.Int(utils.DefaultEvictingCPULowWatermark),
			EvictingMemoryLowWatermark:  utilpointer.Int(utils.DefaultEvictingMemoryLowWatermark),
		},
	}}
}

func TestPrepareConfigmap(t *testing.T) {
	tests := []struct {
		name             string
		initialObjects   []runtime.Object
		expectedErrIsNil bool
		expectedConfig   *api.VolcanoAgentConfig
	}{
		{
			name:             "configmap-existed and update",
			initialObjects:   []runtime.Object{makeConfigMap(oldCfg)},
			expectedErrIsNil: true,
			expectedConfig:   defaultCfg(),
		},
		{
			name:             "configmap-existed and no update",
			initialObjects:   []runtime.Object{makeConfigMap(oldCfgWithSelector)},
			expectedErrIsNil: true,
			expectedConfig: &api.VolcanoAgentConfig{GlobalConfig: &api.ColocationConfig{
				CPUBurstConfig: &api.CPUBurst{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &api.NetworkQos{
					Enable:                          utilpointer.Bool(true),
					OnlineBandwidthWatermarkPercent: utilpointer.Int(utils.DefaultOnlineBandwidthWatermarkPercent),
					OfflineLowBandwidthPercent:      utilpointer.Int(utils.DefaultOfflineLowBandwidthPercent),
					OfflineHighBandwidthPercent:     utilpointer.Int(utils.DefaultOfflineHighBandwidthPercent),
					QoSCheckInterval:                utilpointer.Int(utils.DefaultNetworkQoSInterval),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable:                utilpointer.Bool(true),
					OverSubscriptionTypes: utilpointer.String(utils.DefaultOverSubscriptionTypes),
				},
				EvictingConfig: &api.Evicting{
					EvictingCPUHighWatermark:    utilpointer.Int(utils.DefaultEvictingCPUHighWatermark),
					EvictingMemoryHighWatermark: utilpointer.Int(utils.DefaultEvictingMemoryHighWatermark),
					EvictingCPULowWatermark:     utilpointer.Int(utils.DefaultEvictingCPULowWatermark),
					EvictingMemoryLowWatermark:  utilpointer.Int(utils.DefaultEvictingMemoryLowWatermark),
				},
			},
				NodesConfig: []api.NodesConfig{
					{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label-key": "label-value",
							},
						},
						ColocationConfig: api.ColocationConfig{EvictingConfig: &api.Evicting{
							EvictingCPUHighWatermark: utilpointer.Int(60),
						}},
					},
				}},
		},
		{
			name:             "configmap-not-existed",
			initialObjects:   []runtime.Object{},
			expectedErrIsNil: true,
			expectedConfig:   defaultCfg(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.initialObjects...)
			mgr := &ConfigManager{
				kubeClient:         client,
				configmapNamespace: "kube-system",
				configmapName:      "volcano-agent-configuration",
			}
			actualErr := mgr.PrepareConfigmap()
			assert.Equal(t, tc.expectedErrIsNil, actualErr == nil)
			cm, err := client.CoreV1().ConfigMaps("kube-system").Get(context.TODO(), "volcano-agent-configuration", metav1.GetOptions{})
			assert.NoError(t, err)
			c := &api.VolcanoAgentConfig{}
			err = json.Unmarshal([]byte(cm.Data[utils.ColocationConfigKey]), c)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedConfig, c)
		})

	}
}

type FakeListener struct {
	called  int
	initial bool
	errors  int
}

func (l *FakeListener) SyncConfig(cfg *api.ColocationConfig) error {
	l.called++
	if !l.initial {
		l.initial = true
		return nil
	}
	if l.errors == 0 {
		return nil
	}
	l.errors--
	return errors.New("error")
}

type FakeRateLimiter struct{}

func (r *FakeRateLimiter) When(item interface{}) time.Duration {
	return 0
}

func (r *FakeRateLimiter) NumRequeues(item interface{}) int {
	return 0
}

func (r *FakeRateLimiter) Forget(item interface{}) {}

func TestStart(t *testing.T) {
	tests := []struct {
		name                 string
		initialObjects       []runtime.Object
		sourceQueueObjects   []interface{}
		ListerSyncErrTimes   int
		expectedNotifyCalled int
		expectedErrIsNil     bool
	}{
		{
			name:                 "without-sync-retry",
			initialObjects:       []runtime.Object{makeConfigMap(utils.DefaultCfg)},
			sourceQueueObjects:   []interface{}{"node", "configmap"},
			ListerSyncErrTimes:   0,
			expectedNotifyCalled: 3,
			expectedErrIsNil:     true,
		},
		{
			name:                 "with-sync-retry",
			initialObjects:       []runtime.Object{makeConfigMap(utils.DefaultCfg)},
			sourceQueueObjects:   []interface{}{"node", "configmap"},
			ListerSyncErrTimes:   2,
			expectedNotifyCalled: 5,
			expectedErrIsNil:     true,
		},
	}

	fakeClient := fake.NewSimpleClientset(agentPod)
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
	informerFactory.Core().V1().Pods().Informer()
	informerFactory.Start(context.TODO().Done())
	cache.WaitForNamedCacheSync("", context.TODO().Done(), informerFactory.Core().V1().Pods().Informer().HasSynced)

	for _, tc := range tests {
		l := &FakeListener{
			errors: tc.ListerSyncErrTimes,
		}
		queue := workqueue.NewNamedRateLimitingQueue(&FakeRateLimiter{}, "configmap-resource")
		for _, ob := range tc.sourceQueueObjects {
			queue.Add(ob)
		}
		s := source.NewFakeSource(queue)
		client := fake.NewSimpleClientset(tc.initialObjects...)
		mgr := &ConfigManager{
			kubeClient:         client,
			configmapNamespace: "kube-system",
			configmapName:      "volcano-agent-configuration",
			agentPodNamespace:  "kube-system",
			agentPodName:       "volcano-agent-pod",
			source:             s,
			listeners:          []Listener{l},
			podLister:          informerFactory.Core().V1().Pods().Lister(),
			recorder:           &record.FakeRecorder{},
		}
		actualErr := mgr.Start(context.TODO())
		s.Stop()
		assert.Equal(t, tc.expectedErrIsNil, actualErr == nil)
		assert.Equal(t, tc.expectedNotifyCalled, l.called)
	}
}
