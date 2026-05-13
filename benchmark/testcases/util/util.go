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

package util

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/e2e-framework/klient/conf"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"volcano.sh/apis/pkg/client/clientset/versioned"
)

var (
	// TestEnv is the global test environment.
	TestEnv env.Environment
	// EnvConfig is the global environment configuration.
	EnvConfig *envconf.Config
	// Resources is the global k8s resources client.
	Resources *resources.Resources
	// KubeClient is the global Kubernetes clientset, initialized once in InitTestMain.
	KubeClient kubernetes.Interface
	// VCClient is the global Volcano clientset, initialized once in InitTestMain.
	VCClient versioned.Interface

	index uint64
)

// Index returns an atomically incrementing index for unique naming.
func Index() uint64 {
	return atomic.AddUint64(&index, 1)
}

// EnvInt returns a positive integer from the environment, or fallback when unset/invalid.
func EnvInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// InitTestMain initializes the test environment with kubeconfig and REST client.
func InitTestMain(m *testing.M) {
	var err error
	path := conf.ResolveKubeConfigFile()
	EnvConfig = envconf.NewWithKubeConfig(path)
	TestEnv = env.NewWithConfig(EnvConfig)

	restConfig := EnvConfig.Client().RESTConfig()
	restConfig.RateLimiter = nil
	restConfig.QPS = float32(EnvInt("BENCHMARK_CLIENT_QPS", 500))
	restConfig.Burst = EnvInt("BENCHMARK_CLIENT_BURST", 1000)

	Resources, err = resources.New(restConfig)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	KubeClient, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	VCClient, err = versioned.NewForConfig(restConfig)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(TestEnv.Run(m))
}

// waitForPods waits for at least expectedCount pods matching the predicate via List+Watch.
func waitForPods(ctx context.Context, namespace string, expectedCount int, timeout time.Duration, match func(*corev1.Pod) bool) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	matched := make(map[types.UID]struct{})
	labelSelector := BenchmarkLabel + "=true"
	listOpts := metav1.ListOptions{LabelSelector: labelSelector}

	// The outer loop handles watch expiration: API server closes watch connections
	// after a timeout, so we need to re-list and re-watch when that happens.
	for {
		podList, err := KubeClient.CoreV1().Pods(namespace).List(ctx, listOpts)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("timed out: matched %d/%d pods", len(matched), expectedCount)
			}
			return fmt.Errorf("listing pods: %w", err)
		}
		for i := range podList.Items {
			if match(&podList.Items[i]) {
				matched[podList.Items[i].UID] = struct{}{}
			}
		}
		if len(matched) >= expectedCount {
			return nil
		}

		watchOpts := listOpts
		watchOpts.ResourceVersion = podList.ResourceVersion
		watcher, err := KubeClient.CoreV1().Pods(namespace).Watch(ctx, watchOpts)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("timed out: matched %d/%d pods", len(matched), expectedCount)
			}
			return fmt.Errorf("watching pods: %w", err)
		}

		for event := range watcher.ResultChan() {
			if event.Type == watch.Deleted {
				if pod, ok := event.Object.(*corev1.Pod); ok {
					delete(matched, pod.UID)
				}
				continue
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			if match(pod) {
				matched[pod.UID] = struct{}{}
			} else {
				delete(matched, pod.UID)
			}
			if len(matched) >= expectedCount {
				watcher.Stop()
				return nil
			}
		}
		watcher.Stop()
		if ctx.Err() != nil {
			return fmt.Errorf("timed out: matched %d/%d pods", len(matched), expectedCount)
		}
	}
}

// WaitForPodsCreated waits until all expected pods exist in the API server.
func WaitForPodsCreated(ctx context.Context, namespace string, expectedCount int, timeout time.Duration) error {
	return waitForPods(ctx, namespace, expectedCount, timeout, func(_ *corev1.Pod) bool {
		return true // any existing pod matches
	})
}

// WaitForPodsScheduled waits until all expected pods have a PodScheduled condition set to True.
func WaitForPodsScheduled(ctx context.Context, namespace string, expectedCount int, timeout time.Duration) error {
	return waitForPods(ctx, namespace, expectedCount, timeout, func(pod *corev1.Pod) bool {
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionTrue {
				return true
			}
		}
		return false
	})
}

// CleanupVCJobs deletes all VCJobs in the given namespace.
func CleanupVCJobs(ctx context.Context, namespace string) error {
	// List all jobs
	jobs, err := VCClient.BatchV1alpha1().Jobs(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: BenchmarkLabel + "=true",
	})
	if err != nil {
		return fmt.Errorf("listing vcjobs: %w", err)
	}

	// Delete jobs
	propagationPolicy := metav1.DeletePropagationBackground
	for i := range jobs.Items {
		err := VCClient.BatchV1alpha1().Jobs(namespace).Delete(ctx, jobs.Items[i].Name, metav1.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		})
		if err != nil {
			return fmt.Errorf("deleting vcjob %s: %w", jobs.Items[i].Name, err)
		}
	}

	return nil
}

// CleanupPods deletes all pods matching the benchmark label selector in the namespace.
func CleanupPods(ctx context.Context, namespace string) error {
	propagationPolicy := metav1.DeletePropagationBackground
	return KubeClient.CoreV1().Pods(namespace).DeleteCollection(ctx,
		metav1.DeleteOptions{PropagationPolicy: &propagationPolicy},
		metav1.ListOptions{LabelSelector: BenchmarkLabel + "=true"},
	)
}

// CleanupFunc is the signature for resource cleanup functions.
type CleanupFunc func(ctx context.Context, namespace string) error

// WaitForBenchmarkPodsGone polls until no benchmark-labeled pods remain in the namespace.
func WaitForBenchmarkPodsGone(ctx context.Context, namespace string, timeout time.Duration) error {
	labelSelector := BenchmarkLabel + "=true"
	return kwait.PollUntilContextTimeout(ctx, 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		pods, err := KubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
			Limit:         1,
		})
		if err != nil {
			return false, err
		}
		return len(pods.Items) == 0, nil
	})
}

// SetupTestLifecycle handles the common pre-cleanup, signal handling, and t.Cleanup
// registration shared across all benchmark scenarios. It:
//  1. Runs cleanupFn to remove leftover resources (unless DRY_RUN=true)
//  2. Polls until all benchmark pods are gone
//  3. Registers SIGINT/SIGTERM handler to clean up on interrupt
//  4. Registers t.Cleanup to clean up after the test finishes
func SetupTestLifecycle(t *testing.T, namespace string, cleanupFn CleanupFunc) {
	t.Helper()

	dryRun, _ := strconv.ParseBool(os.Getenv("DRY_RUN"))
	if dryRun {
		return
	}

	// Pre-cleanup: remove leftover resources from previous runs
	t.Logf("Cleaning up pre-existing benchmark resources in namespace %s...", namespace)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	if err := cleanupFn(ctx, namespace); err != nil {
		t.Fatalf("Failed to clean up pre-existing benchmark resources: %v", err)
	}
	cancel()

	// Wait for benchmark pods to be fully deleted before proceeding
	t.Log("Waiting for old benchmark pods to be removed...")
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	if err := WaitForBenchmarkPodsGone(ctx, namespace, 30*time.Second); err != nil {
		t.Fatalf("Failed to clean up old benchmark pods: %v", err)
	}
	cancel()

	// Signal handler: cleanup on SIGINT/SIGTERM
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		t.Logf("\nReceived interrupt signal. Cleaning up in namespace %s...", namespace)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := cleanupFn(ctx, namespace); err != nil {
			t.Logf("Warning: cleanup after interrupt failed: %v", err)
		}
		os.Exit(1)
	}()

	// t.Cleanup: cleanup after test finishes normally
	t.Cleanup(func() {
		t.Logf("Cleaning up benchmark resources in namespace %s...", namespace)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := cleanupFn(ctx, namespace); err != nil {
			t.Errorf("Post-test cleanup failed: %v", err)
		}
	})
}
