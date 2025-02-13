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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/config/source"
	"volcano.sh/volcano/pkg/agent/config/utils"
	"volcano.sh/volcano/pkg/config"
)

type Listener interface {
	SyncConfig(cfg *api.ColocationConfig) error
}

type ConfigManager struct {
	kubeClient         clientset.Interface
	configmapNamespace string
	configmapName      string
	agentPodNamespace  string
	agentPodName       string
	source             source.ConfigEventSource
	listeners          []Listener
	podLister          listersv1.PodLister
	recorder           record.EventRecorder
}

func NewManager(config *config.Configuration, listeners []Listener) *ConfigManager {
	return &ConfigManager{
		kubeClient:         config.GenericConfiguration.KubeClient,
		configmapNamespace: config.GenericConfiguration.KubePodNamespace,
		configmapName:      utils.ConfigMapName,
		agentPodNamespace:  config.GenericConfiguration.KubePodNamespace,
		agentPodName:       config.GenericConfiguration.KubePodName,
		source:             source.NewConfigMapSource(config.GenericConfiguration.KubeClient, config.GenericConfiguration.KubeNodeName, config.GenericConfiguration.KubePodNamespace),
		listeners:          listeners,
		podLister:          config.GenericConfiguration.PodLister,
		recorder:           config.GenericConfiguration.Recorder,
	}
}

func (m *ConfigManager) PrepareConfigmap() error {
	getCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cm, getErr := m.kubeClient.CoreV1().ConfigMaps(m.configmapNamespace).Get(getCtx, m.configmapName, metav1.GetOptions{})
	if getErr == nil {
		klog.InfoS("Configmap already exists", "name", m.configmapName, "namespace", m.configmapNamespace)
		return m.updateConfigMap(cm)
	}

	if !errors.IsNotFound(getErr) {
		klog.ErrorS(getErr, "Failed to get configMap", "name", m.configmapName, "namespace", m.configmapNamespace)
		return getErr
	}

	klog.InfoS("configMap not found, will create a new one")
	var lastCreateErr error
	waitErr := wait.PollImmediate(200*time.Millisecond, time.Minute, func() (done bool, err error) {
		_, createErr := m.kubeClient.CoreV1().ConfigMaps(m.configmapNamespace).Create(context.TODO(), &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      m.configmapName,
				Namespace: m.configmapNamespace},
			Data: map[string]string{
				utils.ColocationConfigKey: utils.DefaultCfg,
			}}, metav1.CreateOptions{})
		if errors.IsAlreadyExists(createErr) {
			return true, nil
		}
		lastCreateErr = createErr
		return createErr == nil, nil
	})
	if waitErr != nil {
		klog.ErrorS(waitErr, "Failed to wait for creating configMap")
		return fmt.Errorf("failed to create configmap(%s:%s), waiting error: %v, latest creation error: %v",
			m.agentPodNamespace, utils.ConfigMapName, waitErr, lastCreateErr)
	}
	return nil
}

func (m *ConfigManager) updateConfigMap(cm *corev1.ConfigMap) error {
	oldData := cm.Data[utils.ColocationConfigKey]
	c := &api.VolcanoAgentConfig{}
	if err := json.Unmarshal([]byte(oldData), c); err != nil {
		return fmt.Errorf("failed to unmarshal old configMap data: %v", err)
	}

	changed := false
	if c.GlobalConfig == nil {
		return fmt.Errorf("empty glaobal config")
	}
	if c.GlobalConfig.OverSubscriptionConfig == nil {
		changed = true
		c.GlobalConfig.OverSubscriptionConfig = &api.OverSubscription{
			Enable:                utilpointer.Bool(true),
			OverSubscriptionTypes: utilpointer.String(utils.DefaultOverSubscriptionTypes),
		}
	}
	if c.GlobalConfig.EvictingConfig == nil {
		changed = true
		c.GlobalConfig.EvictingConfig = &api.Evicting{
			EvictingCPUHighWatermark:    utilpointer.Int(utils.DefaultEvictingCPUHighWatermark),
			EvictingMemoryHighWatermark: utilpointer.Int(utils.DefaultEvictingMemoryHighWatermark),
			EvictingCPULowWatermark:     utilpointer.Int(utils.DefaultEvictingCPULowWatermark),
			EvictingMemoryLowWatermark:  utilpointer.Int(utils.DefaultEvictingMemoryLowWatermark),
		}
	}
	if !changed {
		return nil
	}

	newData, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal configMap: %v", err)
	}
	cm.Data[utils.ColocationConfigKey] = string(newData)
	_, err = m.kubeClient.CoreV1().ConfigMaps(m.configmapNamespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
	if err == nil {
		klog.InfoS("Successfully updated volcano agent configMap")
		return nil
	}
	return err
}

func (m *ConfigManager) Start(ctx context.Context) error {
	klog.InfoS("Start configuration manager")
	if err := m.PrepareConfigmap(); err != nil {
		return err
	}

	changesQueue, err := m.source.Source(ctx.Done())
	if err != nil {
		return err
	}
	if err = m.notifyListeners(); err != nil {
		klog.ErrorS(err, "Failed to notify all listeners, retry by process chanagesQueue")
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if !m.process(changesQueue) {
					return
				}
			}
		}
	}()
	klog.InfoS("Successfully started configuration manager")
	return nil
}

func (m *ConfigManager) process(changedQueue workqueue.RateLimitingInterface) bool {
	key, quit := changedQueue.Get()
	if quit {
		return false
	}
	defer changedQueue.Done(key)

	pod, getErr := m.getAgentPod()
	if getErr != nil {
		klog.ErrorS(getErr, "Failed to get agent pod", "namespace", m.agentPodNamespace, "name", m.agentPodName)
	}

	err := m.notifyListeners()
	if err != nil {
		if pod != nil {
			m.recorder.Event(pod, corev1.EventTypeWarning, "ConfigApplyFailed", fmt.Sprintf("Failed to apply config(%v)", err))
		}
		klog.ErrorS(err, "Failed to notify listeners")
		changedQueue.AddRateLimited(key)
		return true
	}

	if pod != nil {
		m.recorder.Event(pod, corev1.EventTypeNormal, "ConfigApplySuccess", "Successfully applied config")
	}
	klog.InfoS("Successfully notified listeners")
	changedQueue.Forget(key)
	return true
}

func (m *ConfigManager) notifyListeners() error {
	config, err := m.source.GetLatestConfig()
	if err != nil {
		return err
	}

	var errs []error
	for _, listener := range m.listeners {
		if syncErr := listener.SyncConfig(config); syncErr != nil {
			errs = append(errs, syncErr)
		}
	}
	return utilerrors.NewAggregate(errs)
}

func (m *ConfigManager) getAgentPod() (*corev1.Pod, error) {
	pod, err := m.podLister.Pods(m.agentPodNamespace).Get(m.agentPodName)
	if err == nil {
		return pod, nil
	}

	if !errors.IsNotFound(err) {
		return nil, err
	}
	return m.kubeClient.CoreV1().Pods(m.agentPodNamespace).Get(context.TODO(), m.agentPodName, metav1.GetOptions{})
}
