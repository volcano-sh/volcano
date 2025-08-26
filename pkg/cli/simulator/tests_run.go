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

package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/cli/util"
)

const (
	GroupLabelKey   = "simulator.volcano.sh/group"
	UsageLabelKey   = "simulator.volcano.sh/usage"
	UsageLabelValue = "simulator"
)

type runTestsFlags struct {
	util.CommonFlags

	Config string
}

var runTestsFlag = &runTestsFlags{}

// InitRunTestsFlags is used to init all flags during run tests.
func InitRunTestsFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &runTestsFlag.CommonFlags)
	cmd.Flags().StringVar(&runTestsFlag.Config, "config", "", "The file path of pod configs")
}

// PodConfig defines the pod config.
type PodConfig struct {
	Timestamp   time.Time           `json:"timestamp"`
	Type        string              `json:"type"`
	GroupName   string              `json:"groupName"`
	Namespace   string              `json:"namespace"`
	Replicas    int                 `json:"replicas,omitempty"`
	Queue       string              `json:"queue,omitempty"`
	Resources   ResourceConfig      `json:"resources,omitempty"`
	Labels      map[string]string   `json:"labels,omitempty"`
	Annotations map[string]string   `json:"annotations,omitempty"`
	Toleration  []corev1.Toleration `json:"toleration,omitempty"`
}

// ResourceConfig defines the resource config.
type ResourceConfig struct {
	Requests corev1.ResourceList `json:"requests,omitempty"`
	Limits   corev1.ResourceList `json:"limits,omitempty"`
}

func RunTests(ctx context.Context) error {
	// 1. Read and parse JSON configuration
	configs, err := parseConfig(runTestsFlag.Config)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}
	if len(configs) == 0 {
		klog.Infof("No test case found")
		return nil
	}

	// 2. Sort by timestamp
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Timestamp.Before(configs[j].Timestamp)
	})

	// 3. Create Kubernetes clientset
	clientset, err := createKubeClient(runTestsFlag.CommonFlags)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	startTime := time.Now()
	baseTimestamp := configs[0].Timestamp

	for _, config := range configs {
		// Calculate waiting time
		waitDuration := config.Timestamp.Sub(baseTimestamp)
		timeSinceStart := time.Since(startTime)

		if timeSinceStart < waitDuration {
			time.Sleep(waitDuration - timeSinceStart)
		}

		// handle events
		if config.Type == "PodCreated" {
			err = createPods(ctx, clientset, config)
		} else if config.Type == "PodDeleted" {
			err = deletePods(ctx, clientset, config)
		}

		if err != nil {
			return fmt.Errorf("failed to process event: %v", err)
		}
	}

	return nil
}

func parseConfig(filename string) ([]PodConfig, error) {
	file, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var configs []PodConfig
	err = json.Unmarshal(file, &configs)
	if err != nil {
		return nil, err
	}

	return configs, nil
}

func createPods(ctx context.Context, clientset kubernetes.Interface, config PodConfig) error {
	for i := 0; i < config.Replicas; i++ {
		pod := createPodFromConfig(config, i)
		_, err := clientset.CoreV1().Pods(config.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create pod %s: %v", pod.Name, err)
		}
		klog.Infof("Created pod: %s at %v", pod.Name, time.Now())
	}
	return nil
}

func deletePods(ctx context.Context, clientset kubernetes.Interface, config PodConfig) error {
	return deletePodsWithLabel(ctx, clientset, config.Namespace, GroupLabelKey, config.GroupName)
}

func createPodFromConfig(config PodConfig, index int) *corev1.Pod {
	podName := fmt.Sprintf("%s-%d", config.GroupName, index)

	// Create basic labels and annotations
	baseLabels := map[string]string{
		GroupLabelKey: config.GroupName,
		UsageLabelKey: UsageLabelValue,
	}

	baseAnnotations := map[string]string{
		"scheduling.volcano.sh/queue-name": config.Queue,
	}

	// Merge labels and annotations in configuration
	finalLabels := mergeMaps(baseLabels, config.Labels)
	finalAnnotations := mergeMaps(baseAnnotations, config.Annotations)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName,
			Namespace:   config.Namespace,
			Labels:      finalLabels,
			Annotations: finalAnnotations,
		},
		Spec: corev1.PodSpec{
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "type",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"kwok"},
									},
								},
							},
						},
					},
				},
			},
			Tolerations: append([]corev1.Toleration{
				{
					Key:      "kwok.x-k8s.io/node",
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}, config.Toleration...),
			SchedulerName: "volcano",
			Containers: []corev1.Container{
				{
					Name:  "fake-container",
					Image: "fake-image",
					Resources: corev1.ResourceRequirements{
						Requests: config.Resources.Requests,
						Limits:   config.Resources.Limits,
					},
				},
			},
		},
	}

	return pod
}

// mergeMaps Merge two mappings, the key-value pairs of the second mapping will
// overwrite the same keys in the first mapping
func mergeMaps(base, extra map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}
