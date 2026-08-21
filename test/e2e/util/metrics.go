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
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const schedulerNamespace = "volcano-system"

// SchedulerCounterValue returns the sum of counter samples matching labels from
// the Volcano scheduler metrics endpoint. The standard E2E deployment exposes
// this endpoint through the scheduler Service without requiring Prometheus.
func SchedulerCounterValue(ctx context.Context, client kubernetes.Interface, name string, labels map[string]string) (float64, error) {
	services, err := client.CoreV1().Services(schedulerNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=volcano-scheduler",
	})
	if err != nil {
		return 0, fmt.Errorf("list scheduler services: %w", err)
	}
	if len(services.Items) != 1 {
		return 0, fmt.Errorf("expected one scheduler service, got %d", len(services.Items))
	}

	serviceProxyName := "http:" + services.Items[0].Name + ":8080"
	raw, err := client.CoreV1().RESTClient().Get().
		Namespace(schedulerNamespace).
		Resource("services").
		Name(serviceProxyName).
		SubResource("proxy").
		Suffix("metrics").
		DoRaw(ctx)
	if err != nil {
		return 0, fmt.Errorf("read scheduler metrics: %w", err)
	}

	return parseSchedulerCounterValue(raw, name, labels)
}

func parseSchedulerCounterValue(raw []byte, name string, labels map[string]string) (float64, error) {
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("parse scheduler metrics: %w", err)
	}
	family := families[name]
	if family == nil {
		return 0, nil
	}

	var total float64
	for _, metric := range family.Metric {
		if metricHasLabels(metric.Label, labels) && metric.Counter != nil {
			total += metric.Counter.GetValue()
		}
	}
	return total, nil
}

// WaitSchedulerCounterIncrease waits until a matching scheduler counter exceeds
// its baseline value.
func WaitSchedulerCounterIncrease(ctx context.Context, client kubernetes.Interface, name string, labels map[string]string, baseline float64, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		value, err := SchedulerCounterValue(ctx, client, name, labels)
		if err != nil {
			return false, nil
		}
		return value > baseline, nil
	})
}

func metricHasLabels(metricLabels []*dto.LabelPair, expected map[string]string) bool {
	for name, value := range expected {
		matched := false
		for _, label := range metricLabels {
			if label.GetName() == name && label.GetValue() == value {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
