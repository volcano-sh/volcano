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

package gang

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/yaml"
	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"volcano.sh/volcano/benchmark/testcases/util"
)

// VCJobConfig defines the configuration for a single VCJob.
type VCJobConfig struct {
	Name         string // base name for the VCJob (index will be appended automatically)
	Namespace    string // namespace for the VCJob, defaults to "default"
	MinAvailable int32  // gang scheduling minAvailable
	Queue        string // volcano queue name

	// Job-level Network Topology
	EnableNetworkTopology          bool
	NetworkTopologyMode            string
	NetworkTopologyHighestTier     int
	NetworkTopologyHighestTierName string

	Tasks []TaskConfig
}

// TaskConfig defines the configuration for a single Task within a VCJob.
type TaskConfig struct {
	Name     string
	Replicas int32
	Image    string   // default: busybox:1.36
	Command  []string // default: ["/bin/sh", "-c", "sleep 30"]
	CPU      string
	Memory   string

	// Task-level Partition Policy
	EnablePartitionPolicy    bool
	PartitionTotalPartitions int32
	PartitionSize            int32
	PartitionMinPartitions   int32

	// Task-level Partition Network Topology
	EnablePartitionNetworkTopology bool
	PartitionNetworkMode           string
	PartitionHighestTier           int
	PartitionHighestTierName       string

	// Node Selector and Affinity
	EnableNodeSelector bool
	NodeSelector       map[string]string
}

// jobTemplateData extends VCJobConfig with fields dynamically generated for the template
type jobTemplateData struct {
	VCJobConfig
	JobName string
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

// withDefaultConfig applies default values to the jobTemplateData
func withDefaultConfig(data *jobTemplateData) {
	if data.Namespace == "" {
		data.Namespace = "default"
	}

	for i := range data.Tasks {
		if data.Tasks[i].Image == "" {
			data.Tasks[i].Image = util.DefaultImage
		}
		// Apply default command only if image is busybox and command is empty
		if data.Tasks[i].Image == util.DefaultImage && len(data.Tasks[i].Command) == 0 {
			data.Tasks[i].Command = util.DefaultCommand
		}
	}
}

// BuildVCJob constructs a Volcano Job from the vcjob-template.yaml file.
func BuildVCJob(cfg VCJobConfig, index uint64) (*batchv1alpha1.Job, error) {
	scenarioDir, err := getTestFileDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get test file directory: %v", err)
	}
	templatePath := filepath.Join(scenarioDir, "cases", "vcjob-template.yaml")

	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("reading vcjob template %s: %w", templatePath, err)
	}

	tmpl, err := template.New("vcjob").Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("parsing vcjob template: %w", err)
	}

	data := jobTemplateData{
		VCJobConfig: cfg,
		JobName:     fmt.Sprintf("%s-%d", cfg.Name, index),
	}
	// Apply default configurations
	withDefaultConfig(&data)

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing vcjob template: %w", err)
	}

	job := &batchv1alpha1.Job{}
	if err := yaml.Unmarshal(buf.Bytes(), job); err != nil {
		return nil, fmt.Errorf("unmarshaling vcjob yaml: %w", err)
	}

	return job, nil
}

// CreateGangJobs creates VCJobs according to the given template concurrency.
func CreateGangJobs(ctx context.Context, cfg VCJobConfig, count int) error {
	errs := make([]error, count)

	checkJob := func(i int) {
		job, err := BuildVCJob(cfg, util.Index())
		if err != nil {
			errs[i] = fmt.Errorf("building vcjob %d: %w", i, err)
			return
		}

		_, err = util.VCClient.BatchV1alpha1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
		if err != nil {
			errs[i] = fmt.Errorf("creating vcjob %d: %w", i, err)
		}
	}

	workqueue.ParallelizeUntil(ctx, util.EnvInt("BENCHMARK_CREATE_CONCURRENCY", 16), count, checkJob)

	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

// RunGangTest is the common test runner for gang scheduling benchmarks.
func RunGangTest(t *testing.T, cfg VCJobConfig, jobCount int) {
	ctx := context.Background()

	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "default"
	}

	util.SetupTestLifecycle(t, namespace, util.CleanupVCJobs)

	totalPods := 0
	for _, task := range cfg.Tasks {
		totalPods += int(task.Replicas) * jobCount
	}

	t.Logf("Starting gang scheduling test: %d VCJobs, %d total pods", jobCount, totalPods)

	err := CreateGangJobs(ctx, cfg, jobCount)
	if err != nil {
		t.Fatalf("Failed to create gang jobs: %v", err)
	}
	t.Logf("Submitted %d VCJobs", jobCount)

	// Wait for all pods to be created by job controller
	t.Log("Waiting for all pods to be created in API server...")
	if err = util.WaitForPodsCreated(ctx, namespace, totalPods, 5*time.Minute); err != nil {
		t.Fatalf("Pods creation timeout (expected %d): %v", totalPods, err)
	}

	// Wait for scheduler to schedule all pods
	t.Log("Waiting for all pods to be scheduled...")
	if err = util.WaitForPodsScheduled(ctx, namespace, totalPods, 10*time.Minute); err != nil {
		t.Fatalf("Pods scheduling timeout: %v", err)
	}

	t.Logf("=== Results ===")
	t.Logf("Total VCJobs: %d, Total pods: %d", jobCount, totalPods)
	t.Logf("Test finished. Latency metrics available via audit-exporter.")
}

// BenchmarkProfile defines the structure of the YAML configuration for gang scheduling benchmarks.
type BenchmarkProfile struct {
	Jobs        int         // How many jobs to create
	JobTemplate VCJobConfig // The template configuration for the jobs
}

// TestFromConfig runs a gang scheduling test using a YAML configuration file.
// Usage: BENCHMARK_CONFIG=./profiles/test1.yaml go test -v -run TestFromConfig
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

	t.Logf("Loaded profile from %s: Jobs=%d, TemplateName=%s", configFile, profile.Jobs, profile.JobTemplate.Name)

	RunGangTest(t, profile.JobTemplate, profile.Jobs)
}
