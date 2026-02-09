package pkg

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/e2e-framework/klient/conf"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

var (
	// TestEnv is the global test environment.
	TestEnv env.Environment
	// EnvConfig is the global environment configuration.
	EnvConfig *envconf.Config
	// Resources is the global k8s resources client.
	Resources *resources.Resources

	index uint64
)

// Index returns an atomically incrementing index for unique naming.
func Index() uint64 {
	return atomic.AddUint64(&index, 1)
}

// InitTestMain initializes the test environment with kubeconfig and REST client.
// It also starts a Prometheus metrics HTTP server on :9090.
func InitTestMain(m *testing.M) {
	var err error
	path := conf.ResolveKubeConfigFile()
	EnvConfig = envconf.NewWithKubeConfig(path)
	TestEnv = env.NewWithConfig(EnvConfig)

	restConfig := EnvConfig.Client().RESTConfig()
	restConfig.RateLimiter = nil
	restConfig.QPS = 100
	restConfig.Burst = 200

	Resources, err = resources.New(restConfig)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	StartMetricsServer(":9090")

	os.Exit(TestEnv.Run(m))
}

// WaitForPodsScheduled waits until all pods matching the label selector in the
// given namespace have been scheduled (have a non-empty spec.nodeName).
func WaitForPodsScheduled(ctx context.Context, namespace, labelSelector string, expectedCount int, timeout time.Duration) error {
	return wait.For(func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		err := Resources.WithNamespace(namespace).List(ctx, &pods,
			resources.WithLabelSelector(labelSelector),
		)
		if err != nil {
			return false, err
		}
		scheduled := 0
		for i := range pods.Items {
			if pods.Items[i].Spec.NodeName != "" {
				scheduled++
			}
		}
		return scheduled >= expectedCount, nil
	},
		wait.WithContext(ctx),
		wait.WithInterval(1*time.Second),
		wait.WithTimeout(timeout),
	)
}

// MeasurePodsCreationLatency measures the wall-clock time from VCJob submission
// (submitTime, captured via time.Now() before creating VCJobs) to all expected
// pods existing in the API server. It returns the duration and records it to
// the benchmark_pods_creation_latency_milliseconds Prometheus gauge.
func MeasurePodsCreationLatency(ctx context.Context, namespace, labelSelector string, expectedCount int, submitTime time.Time, timeout time.Duration) (time.Duration, error) {
	err := wait.For(func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		err := Resources.WithNamespace(namespace).List(ctx, &pods,
			resources.WithLabelSelector(labelSelector),
		)
		if err != nil {
			return false, err
		}
		return len(pods.Items) >= expectedCount, nil
	},
		wait.WithContext(ctx),
		wait.WithInterval(500*time.Millisecond),
		wait.WithTimeout(timeout),
	)
	if err != nil {
		return 0, fmt.Errorf("waiting for pods creation: %w", err)
	}
	latency := time.Since(submitTime)
	PodsCreationLatency.Set(float64(latency.Milliseconds()))
	return latency, nil
}

// MeasureSchedulingLatency waits for all expected pods to be scheduled (bound
// to a node), then records per-pod end-to-end scheduling latency (wall-clock
// time.Now() minus pod.CreationTimestamp) into the
// benchmark_e2e_scheduling_latency_milliseconds Prometheus histogram.
func MeasureSchedulingLatency(ctx context.Context, namespace, labelSelector string, expectedCount int, timeout time.Duration) error {
	if err := WaitForPodsScheduled(ctx, namespace, labelSelector, expectedCount, timeout); err != nil {
		return fmt.Errorf("waiting for pods scheduled: %w", err)
	}

	var pods corev1.PodList
	if err := Resources.WithNamespace(namespace).List(ctx, &pods,
		resources.WithLabelSelector(labelSelector),
	); err != nil {
		return fmt.Errorf("listing pods for scheduling latency: %w", err)
	}

	now := time.Now()
	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName != "" {
			latency := now.Sub(pods.Items[i].CreationTimestamp.Time)
			E2eSchedulingLatency.Observe(float64(latency.Milliseconds()))
		}
	}
	return nil
}

// WaitForDeployment waits for a deployment to become available.
func WaitForDeployment(ctx context.Context, name, namespace string, timeout time.Duration) error {
	return wait.For(func(ctx context.Context) (bool, error) {
		var pods corev1.PodList
		err := Resources.WithNamespace(namespace).List(ctx, &pods,
			resources.WithLabelSelector(fmt.Sprintf("app=%s", name)),
		)
		if err != nil {
			return false, err
		}
		for i := range pods.Items {
			for _, c := range pods.Items[i].Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	},
		wait.WithContext(ctx),
		wait.WithInterval(2*time.Second),
		wait.WithTimeout(timeout),
	)
}

// CleanupVCJobs deletes all VCJobs in the given namespace using unstructured client.
func CleanupVCJobs(ctx context.Context, namespace string) error {
	obj := &unstructured.UnstructuredList{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "batch.volcano.sh",
		Version: "v1alpha1",
		Kind:    "JobList",
	})
	err := Resources.WithNamespace(namespace).List(ctx, obj)
	if err != nil {
		return err
	}
	for i := range obj.Items {
		if err := Resources.Delete(ctx, &obj.Items[i]); err != nil {
			return err
		}
	}
	return nil
}
