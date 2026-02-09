package gang_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"text/template"
	"time"

	benchpkg "volcano.sh/volcano/benchmark/pkg"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// VCJobConfig defines the configuration for a single VCJob.
type VCJobConfig struct {
	Name         string // base name for the VCJob (index will be appended automatically)
	Replicas     int32  // pods per job
	MinAvailable int32  // gang scheduling minAvailable
	CPU          string // cpu request per pod
	Memory       string // memory request per pod
	Queue        string // volcano queue name
}

// RepeatConfig generates n copies of the given VCJobConfig.
// Use this to express "N identical VCJobs" without embedding count in the config.
func RepeatConfig(cfg VCJobConfig, n int) []VCJobConfig {
	configs := make([]VCJobConfig, n)
	for i := range configs {
		configs[i] = cfg
	}
	return configs
}

func TestMain(m *testing.M) {
	benchpkg.InitTestMain(m)
}

// getScenarioDir returns the scenario directory path from environment or default.
func getScenarioDir() string {
	if dir := os.Getenv("BENCHMARK_SCENARIO_DIR"); dir != "" {
		return dir
	}
	// Default: assume running from volcano root, scenario is "gang"
	return "benchmark/testcases/gang"
}

// BuildVCJob constructs a Volcano Job from the vcjob-template.yaml file.
func BuildVCJob(cfg VCJobConfig, index uint64) (*unstructured.Unstructured, error) {
	scenarioDir := getScenarioDir()
	templatePath := filepath.Join(scenarioDir, "manifests", "volcano", "vcjob-template.yaml")

	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, fmt.Errorf("reading vcjob template %s: %w", templatePath, err)
	}

	tmpl, err := template.New("vcjob").Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("parsing vcjob template: %w", err)
	}

	// Template data
	data := map[string]interface{}{
		"Name":         fmt.Sprintf("%s-%d", cfg.Name, index),
		"Replicas":     cfg.Replicas,
		"MinAvailable": cfg.MinAvailable,
		"CPU":          cfg.CPU,
		"Memory":       cfg.Memory,
		"Queue":        cfg.Queue,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing vcjob template: %w", err)
	}

	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(buf.Bytes(), &obj.Object); err != nil {
		return nil, fmt.Errorf("unmarshaling vcjob yaml: %w", err)
	}

	return obj, nil
}

// CreateGangJobs creates VCJobs from a list of configs and returns the submission timestamp.
// Each VCJobConfig corresponds to exactly one VCJob.
func CreateGangJobs(ctx context.Context, configs []VCJobConfig) (time.Time, error) {
	submitTime := time.Now()
	for i, cfg := range configs {
		obj, err := BuildVCJob(cfg, benchpkg.Index())
		if err != nil {
			return submitTime, fmt.Errorf("building vcjob %d: %w", i, err)
		}
		if err := benchpkg.Resources.Create(ctx, obj); err != nil {
			return submitTime, fmt.Errorf("creating vcjob %d: %w", i, err)
		}
	}
	return submitTime, nil
}

// RunGangTest is the common test runner for gang scheduling benchmarks.
// Each element in configs represents a single VCJob to create.
func RunGangTest(t *testing.T, configs []VCJobConfig) {
	ctx := context.Background()

	// Clean up any pre-existing VCJobs to allow re-runs without conflicts
	t.Log("Cleaning up pre-existing VCJobs...")
	if err := benchpkg.CleanupVCJobs(ctx, "default"); err != nil {
		t.Logf("Warning: failed to cleanup old VCJobs: %v", err)
	}
	// Wait briefly for cleanup to propagate
	time.Sleep(3 * time.Second)

	totalPods := 0
	for _, cfg := range configs {
		totalPods += int(cfg.Replicas)
	}

	t.Logf("Starting gang scheduling test: %d VCJobs, %d total pods", len(configs), totalPods)

	submitTime, err := CreateGangJobs(ctx, configs)
	if err != nil {
		t.Fatalf("Failed to create gang jobs: %v", err)
	}
	t.Logf("Submitted %d VCJobs", len(configs))

	// Wait for all pods to be scheduled
	t.Log("Waiting for all pods to be scheduled...")
	err = benchpkg.WaitForPodsScheduled(ctx, "default", "volcano.sh/job-name", totalPods, 10*time.Minute)
	if err != nil {
		t.Fatalf("Pods scheduling timeout: %v", err)
	}

	// Measure pod creation latency
	latency, err := benchpkg.MeasurePodsCreationLatency(
		ctx, "default", "volcano.sh/job-name", totalPods,
		submitTime, 10*time.Minute,
	)
	if err != nil {
		t.Fatalf("Failed to measure pod creation latency: %v", err)
	}

	// Measure per-pod scheduling latency (pod creation -> node binding)
	if err := benchpkg.MeasureSchedulingLatency(
		ctx, "default", "volcano.sh/job-name", totalPods, 10*time.Minute,
	); err != nil {
		t.Fatalf("Failed to measure scheduling latency: %v", err)
	}

	t.Logf("=== Results ===")
	t.Logf("Total VCJobs: %d, Total pods: %d", len(configs), totalPods)
	t.Logf("Pod creation latency (vcjob submit -> all pods created): %v", latency)
	t.Logf("Throughput: %.1f pods/sec", float64(totalPods)/latency.Seconds())

	// Verify pod count
	var pods corev1.PodList
	err = benchpkg.Resources.WithNamespace("default").List(ctx, &pods)
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	// Filter pods with volcano label
	count := 0
	for _, p := range pods.Items {
		if _, ok := p.Labels["volcano.sh/job-name"]; ok {
			count++
		}
	}
	t.Logf("Verified: %d volcano pods found", count)
}

// TestFromCLI runs a gang scheduling test using parameters from environment variables.
// This is used by run-tests.sh in CLI params mode (JOBS, PODS, CPU, MEMORY, etc.).
func TestFromCLI(t *testing.T) {
	params := benchpkg.GetCLITestParams()
	if params == nil {
		t.Skip("Skipping: BENCHMARK_JOBS not set (not in CLI params mode)")
	}

	t.Logf("CLI params: jobs=%d, pods=%d, cpu=%s, memory=%s, minAvailable=%d, queue=%s",
		params.Jobs, params.Pods, params.CPU, params.Memory, params.MinAvailable, params.Queue)

	RunGangTest(t, RepeatConfig(VCJobConfig{
		Name:         fmt.Sprintf("gang-%dx%d", params.Jobs, params.Pods),
		Replicas:     int32(params.Pods),
		MinAvailable: int32(params.MinAvailable),
		CPU:          params.CPU,
		Memory:       params.Memory,
		Queue:        params.Queue,
	}, params.Jobs))
}
