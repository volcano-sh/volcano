package pkg

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Options holds benchmark test configuration parameters.
type Options struct {
	NodeSize            int
	CpuPerNode          string
	MemoryPerNode       string
	CpuRequestPerPod    string
	MemoryRequestPerPod string
	Queue               string
	Gang                bool
}

// CLITestParams holds parameters for ad-hoc CLI-driven test runs.
type CLITestParams struct {
	Jobs         int
	Pods         int
	CPU          string
	Memory       string
	MinAvailable int
	Queue        string
	Scenario     string
	ScenarioDir  string
}

// GetCLITestParams reads test parameters from BENCHMARK_* environment variables.
// Returns nil if BENCHMARK_JOBS is not set (i.e., not in CLI params mode).
func GetCLITestParams() *CLITestParams {
	jobs := getEnv("BENCHMARK_JOBS", "")
	if jobs == "" {
		return nil
	}
	pods := getEnvInt("BENCHMARK_PODS", 0)
	minAvail := getEnvInt("BENCHMARK_MIN_AVAILABLE", pods)

	return &CLITestParams{
		Jobs:         getEnvInt("BENCHMARK_JOBS", 0),
		Pods:         pods,
		CPU:          getEnv("BENCHMARK_CPU", "1"),
		Memory:       getEnv("BENCHMARK_MEMORY", "1Gi"),
		MinAvailable: minAvail,
		Queue:        getEnv("BENCHMARK_QUEUE", "benchmark-queue"),
		Scenario:     getEnv("BENCHMARK_SCENARIO", "default"),
		ScenarioDir:  getEnv("BENCHMARK_SCENARIO_DIR", ""),
	}
}

// AddFlags registers options from environment variables and command-line flags.
func (o *Options) AddFlags() {
	flag.IntVar(&o.NodeSize, "nodes-size", getEnvInt("NODES_SIZE", 100), "Number of KWOK nodes")
	flag.StringVar(&o.CpuPerNode, "cpu-per-node", getEnv("CPU_PER_NODE", "32"), "CPU per node")
	flag.StringVar(&o.MemoryPerNode, "memory-per-node", getEnv("MEMORY_PER_NODE", "256Gi"), "Memory per node")
	flag.StringVar(&o.CpuRequestPerPod, "cpu-request-per-pod", getEnv("CPU_REQUEST_PER_POD", "1"), "CPU request per pod")
	flag.StringVar(&o.MemoryRequestPerPod, "memory-request-per-pod", getEnv("MEMORY_REQUEST_PER_POD", "1Gi"), "Memory request per pod")
	flag.StringVar(&o.Queue, "queue", getEnv("QUEUE", "benchmark-queue"), "Volcano queue name")
	flag.BoolVar(&o.Gang, "gang", getEnvBool("GANG", true), "Enable gang scheduling")
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		var result int
		_, err := fmt.Sscan(value, &result)
		if err == nil {
			return result
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		result, err := strconv.ParseBool(strings.ToLower(value))
		if err == nil {
			return result
		}
	}
	return fallback
}
