/*
Copyright 2026 The Volcano Authors.

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

package pod

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/yaml"

	"volcano.sh/volcano/benchmark/testcases/util"
)

const (
	DefaultSchedulerName = "agent-scheduler"
)

// PodConfig defines the configuration for benchmark pods.
type PodConfig struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace,omitempty"`
	SchedulerName string `json:"schedulerName,omitempty"`
	Image         string `json:"image,omitempty"`
	CPU           string `json:"cpu"`
	Memory        string `json:"memory"`
}

// BenchmarkProfile defines the structure of the YAML configuration for pod scheduling benchmarks.
type BenchmarkProfile struct {
	Pods        int       `json:"pods"`
	PodTemplate PodConfig `json:"podTemplate"`
}

// podTemplateData extends PodConfig with fields dynamically generated for the template.
type podTemplateData struct {
	PodConfig
	PodName string
	Command []string
}

func TestMain(m *testing.M) {
	util.InitTestMain(m)
}

// getTestFileDir returns the directory of the current test file.
func getTestFileDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("could not determine test file directory")
	}
	return filepath.Dir(file), nil
}

// withDefaults applies default values to podTemplateData.
func withDefaults(data *podTemplateData) {
	if data.Namespace == "" {
		data.Namespace = "default"
	}
	if data.SchedulerName == "" {
		data.SchedulerName = DefaultSchedulerName
	}
	if data.Image == "" {
		data.Image = util.DefaultImage
	}
	if data.Image == util.DefaultImage && len(data.Command) == 0 {
		data.Command = util.DefaultCommand
	}
}

// BuildPod constructs a Pod from the pod-template.yaml file.
func BuildPod(cfg PodConfig, index uint64) (*corev1.Pod, error) {
	scenarioDir, err := getTestFileDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get test file directory: %v", err)
	}
	templatePath := filepath.Join(scenarioDir, "cases", "pod-template.yaml")

	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("reading pod template %s: %w", templatePath, err)
	}

	tmpl, err := template.New("pod").Funcs(template.FuncMap{
		"env": os.Getenv,
	}).Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("parsing pod template: %w", err)
	}

	data := podTemplateData{
		PodConfig: cfg,
		PodName:   fmt.Sprintf("%s-%d", cfg.Name, index),
	}
	withDefaults(&data)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing pod template: %w", err)
	}

	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(buf.Bytes(), pod); err != nil {
		return nil, fmt.Errorf("unmarshaling pod yaml: %w", err)
	}

	return pod, nil
}

// createPods creates bare pods concurrently using client-go.
func createPods(ctx context.Context, cfg PodConfig, count int) error {
	errs := make([]error, count)
	workqueue.ParallelizeUntil(ctx, util.EnvInt("BENCHMARK_CREATE_CONCURRENCY", 16), count, func(i int) {
		pod, err := BuildPod(cfg, util.Index())
		if err != nil {
			errs[i] = fmt.Errorf("building pod %d: %w", i, err)
			return
		}
		_, err = util.KubeClient.CoreV1().Pods(cfg.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		if err != nil {
			errs[i] = fmt.Errorf("creating pod %d: %w", i, err)
		}
	})

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// RunPodTest is the test runner for pod scheduling benchmarks.
func RunPodTest(t *testing.T, cfg PodConfig, podCount int) {
	ctx := context.Background()

	// Apply default values
	defaults := podTemplateData{PodConfig: cfg}
	withDefaults(&defaults)
	cfg = defaults.PodConfig

	util.SetupTestLifecycle(t, cfg.Namespace, util.CleanupPods)

	t.Logf("Starting pod scheduling test: %d pods (schedulerName=%s)", podCount, cfg.SchedulerName)

	if err := createPods(ctx, cfg, podCount); err != nil {
		t.Fatalf("Failed to create pods: %v", err)
	}
	t.Logf("Submitted %d pods", podCount)

	// Pods are created directly — no need to wait for a controller to create them.
	// Go straight to waiting for scheduling.
	t.Log("Waiting for all pods to be scheduled...")
	if err := util.WaitForPodsScheduled(ctx, cfg.Namespace, podCount, 10*time.Minute); err != nil {
		t.Fatalf("Pods scheduling timeout: %v", err)
	}

	t.Logf("=== Results ===")
	t.Logf("Total pods: %d, schedulerName: %s", podCount, cfg.SchedulerName)
	t.Logf("Test finished. Latency metrics available via audit-exporter.")
}

// TestFromConfig runs a pod scheduling test using a YAML configuration file.
// Usage: BENCHMARK_CONFIG=./cases/case-template.yaml go test -v -run TestFromConfig
func TestFromConfig(t *testing.T) {
	configFile := os.Getenv("BENCHMARK_CONFIG")
	if configFile == "" {
		t.Skip("Skipping: BENCHMARK_CONFIG not set. Use BENCHMARK_CONFIG=/path/to/config.yaml to run.")
	}

	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config file %s: %v", configFile, err)
	}

	var profile BenchmarkProfile
	if err := yaml.Unmarshal(content, &profile); err != nil {
		t.Fatalf("Failed to parse config file %s: %v", configFile, err)
	}

	t.Logf("Loaded profile from %s: Pods=%d, Template=%s", configFile, profile.Pods, profile.PodTemplate.Name)

	RunPodTest(t, profile.PodTemplate, profile.Pods)
}
