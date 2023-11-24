/*
 Copyright 2023 The Volcano Authors.

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

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	customclient "k8s.io/metrics/pkg/client/custom_metrics"
)

const (
	// CustomNodeCPUUsageAvg record name of cpu average usage defined in prometheus adapt rules
	CustomNodeCPUUsageAvg = "node_cpu_usage_avg"
	// CustomNodeMemUsageAvg record name of mem average usage defined in prometheus adapt rules
	CustomNodeMemUsageAvg = "node_memory_usage_avg"
)

type KMetricsClient struct {
	customMetricsCli customclient.CustomMetricsClient
}

var kMetricsClient *KMetricsClient

func NewCustomMetricsClient(cfg *rest.Config) (*KMetricsClient, error) {
	if kMetricsClient != nil {
		return kMetricsClient, nil
	}

	klog.V(3).Infof("Create custom metrics api client")
	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(cfg)
	cachedDiscoClient := cacheddiscovery.NewMemCacheClient(discoveryClient)
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscoClient)
	apiVersionsGetter := customclient.NewAvailableAPIsGetter(discoveryClient)
	customMetricsClient := customclient.NewForConfig(cfg, restMapper, apiVersionsGetter)

	kMetricsClient = &KMetricsClient{
		customMetricsCli: customMetricsClient,
	}
	return kMetricsClient, nil
}

func (km *KMetricsClient) NodesMetricsAvg(ctx context.Context, nodeMetricsMap map[string]*NodeMetrics) error {
	klog.V(5).Infof("Get node metrics from Custom Metrics")

	groupKind := schema.GroupKind{
		Group: "",
		Kind:  "Node",
	}

	for _, metricName := range []string{CustomNodeCPUUsageAvg, CustomNodeMemUsageAvg} {
		metricsValue, err := km.customMetricsCli.RootScopedMetrics().GetForObjects(groupKind, labels.NewSelector(), metricName, labels.NewSelector())
		if err != nil {
			klog.Errorf("Failed to query the indicator %s, error is: %v.", metricName, err)
			return err
		}
		for _, metricValue := range metricsValue.Items {
			nodeName := metricValue.DescribedObject.Name
			if _, ok := nodeMetricsMap[nodeName]; !ok {
				klog.Warningf("The node %s information is obtained through the custom metrics API, but the volcano cache does not contain the node information.", nodeName)
				continue
			}
			klog.V(5).Infof("The current usage information of node %s is %v", nodeName, nodeMetricsMap[nodeName])
			switch metricName {
			case CustomNodeCPUUsageAvg:
				nodeMetricsMap[nodeName].MetricsTime = metricValue.Timestamp.Time
				nodeMetricsMap[nodeName].CPU = metricValue.Value.AsApproximateFloat64() * 100
			case CustomNodeMemUsageAvg:
				nodeMetricsMap[nodeName].MetricsTime = metricValue.Timestamp.Time
				nodeMetricsMap[nodeName].Memory = metricValue.Value.AsApproximateFloat64() * 100
			default:
				klog.Errorf("Node supports %s and %s metrics, and %s indicates abnormal metrics.", CustomNodeCPUUsageAvg, CustomNodeMemUsageAvg, metricName)
			}
			klog.V(5).Infof("The updated usage information of node %s is %v.", nodeName, nodeMetricsMap[nodeName])
		}
	}

	return nil
}
