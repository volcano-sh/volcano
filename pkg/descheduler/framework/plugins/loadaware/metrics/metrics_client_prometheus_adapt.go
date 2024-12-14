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

package source

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	"k8s.io/metrics/pkg/client/clientset/versioned"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
	customclient "k8s.io/metrics/pkg/client/custom_metrics"
)

const (
	// CustomNodeCPUUsageAvg record name of cpu average usage defined in prometheus adapt rules
	CustomNodeCPUUsageAvg = "node_cpu_usage_avg"
	// CustomNodeMemUsageAvg record name of mem average usage defined in prometheus adapt rules
	CustomNodeMemUsageAvg = "node_memory_usage_avg"
)

type KMetricsClient struct {
	CustomMetricsCli customclient.CustomMetricsClient
	MetricsCli       metricsv1beta1.MetricsV1beta1Interface
}

var kMetricsClient *KMetricsClient

func NewCustomMetricsClient() (*KMetricsClient, error) {
	if kMetricsClient != nil {
		return kMetricsClient, nil
	}
	klog.V(3).Infof("Create custom metrics client to get nodes and pods metrics")

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to build in cluster config: %v", err)
	}

	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(cfg)
	cachedDiscoClient := cacheddiscovery.NewMemCacheClient(discoveryClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoClient)
	apiVersionsGetter := customclient.NewAvailableAPIsGetter(discoveryClient)
	customMetricsClient := customclient.NewForConfig(cfg, restMapper, apiVersionsGetter)

	metricsClient, err := versioned.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("New metrics client failed, error is : %v", err)
	}
	metricsClientV1beta1 := metricsClient.MetricsV1beta1()

	kMetricsClient = &KMetricsClient{
		CustomMetricsCli: customMetricsClient,
		MetricsCli:       metricsClientV1beta1,
	}

	return kMetricsClient, nil
}

func (km *KMetricsClient) NodesMetricsAvg(_ context.Context, nodesMetrics map[string]*NodeMetrics, _ string) error {
	klog.V(5).Infof("Get node metrics from custom metrics api")

	groupKind := schema.GroupKind{
		Group: "",
		Kind:  "Node",
	}
	for _, metricName := range []string{CustomNodeCPUUsageAvg, CustomNodeMemUsageAvg} {
		metricsValue, err := km.CustomMetricsCli.RootScopedMetrics().GetForObjects(groupKind, labels.NewSelector(), metricName, labels.NewSelector())
		if err != nil {
			klog.Errorf("Failed to query the indicator %s, error is: %v.", metricName, err)
			return err
		}
		for _, metricValue := range metricsValue.Items {
			nodeName := metricValue.DescribedObject.Name
			if _, ok := nodesMetrics[nodeName]; !ok {
				klog.Warningf("The node %s information is obtained through the custom metrics API, but the volcano cache does not contain the node information.", nodeName)
				continue
			}
			klog.V(5).Infof("The current usage information of node %s is %v", nodeName, nodesMetrics[nodeName])
			switch metricName {
			case CustomNodeCPUUsageAvg:
				nodesMetrics[nodeName].CPU = metricValue.Value.AsApproximateFloat64()
			case CustomNodeMemUsageAvg:
				nodesMetrics[nodeName].Memory = metricValue.Value.AsApproximateFloat64()
			default:
				klog.Errorf("Node supports %s and %s metrics, and %s indicates abnormal metrics.", CustomNodeCPUUsageAvg, CustomNodeMemUsageAvg, metricName)
			}
			klog.V(5).Infof("The updated usage information of node %s is %v.", nodeName, nodesMetrics[nodeName])
		}
	}

	return nil
}

func (km *KMetricsClient) PodsMetricsAvg(ctx context.Context, pods []*v1.Pod, period string) (map[types.NamespacedName]map[v1.ResourceName]*resource.Quantity, error) {
	klog.V(5).Infof("Get pods metrics from metrics api")
	podsMetrics := make(map[types.NamespacedName]map[v1.ResourceName]*resource.Quantity, len(pods))

	for _, pod := range pods {
		podMetrics, err := km.MetricsCli.PodMetricses(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			klog.Warningf("Failed to query pod(%s/%s) metrics, error is: %v.", pod.Namespace, pod.Name, err)
			continue
		}
		podUsage := make(map[v1.ResourceName]*resource.Quantity)
		for _, containerUsage := range podMetrics.Containers {
			for _, resourceName := range []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory} {
				if _, ok := podUsage[resourceName]; !ok {
					podUsage[resourceName] = &resource.Quantity{}
				}
				podUsage[resourceName].Add(containerUsage.Usage[resourceName])
			}
		}
		namespaceName := types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		}
		podsMetrics[namespaceName] = podUsage
		klog.V(5).Infof("Pod(%s/%s) metrics is %v", pod.Namespace, pod.Name, podUsage)
	}
	return podsMetrics, nil
}
