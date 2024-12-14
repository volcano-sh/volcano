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
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	pmodel "github.com/prometheus/common/model"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	// promCPUUsageAvg record name of cpu average usage defined in prometheus rules
	promCPUUsageAvg = "cpu_usage_avg"
	// promMemUsageAvg record name of mem average usage defined in prometheus rules
	promMemUsageAvg = "mem_usage_avg"
)

type PrometheusMetricsClient struct {
	address string
	conf    Metrics
}

func NewPrometheusMetricsClient(metricsConf Metrics) (*PrometheusMetricsClient, error) {
	address := metricsConf.Address
	if len(address) == 0 {
		return nil, fmt.Errorf("Metrics address is empty, the load-aware rescheduling function does not take effect.")
	}
	return &PrometheusMetricsClient{address: address, conf: metricsConf}, nil
}

func (p *PrometheusMetricsClient) NodesMetricsAvg(ctx context.Context, nodesMetrics map[string]*NodeMetrics, period string) error {
	klog.V(5).Infof("Get node metrics from prometheus")
	for nodeName := range nodesMetrics {
		nodeMetrics, err := p.NodeMetricsAvg(ctx, nodeName, period)
		if err != nil {
			klog.Errorf("Failed to query the node(%s) metrios, error is: %v.", nodeName, err)
			return err
		}
		klog.V(5).Infof("Node(%s) usage metrics is %v", nodeName, nodeMetrics)
		nodesMetrics[nodeName] = &nodeMetrics
	}
	return nil
}

func (p *PrometheusMetricsClient) NodeMetricsAvg(ctx context.Context, nodeName string, period string) (NodeMetrics, error) {
	klog.V(4).Infof("Get node metrics from Prometheus: %s", p.address)
	var client api.Client
	var err error
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{},
	}
	client, err = api.NewClient(api.Config{
		Address:      p.address,
		RoundTripper: transport,
	})
	if err != nil {
		return NodeMetrics{}, err
	}
	v1api := prometheusv1.NewAPI(client)
	nodeMetrics := NodeMetrics{}
	cpuQueryStr := fmt.Sprintf("avg_over_time((1 - (avg by (instance) (irate(node_cpu_seconds_total{mode=\"idle\",instance=\"%s\"}[30s])) * 1))[%s:30s])", nodeName, period)
	memQueryStr := fmt.Sprintf("avg_over_time(((1-node_memory_MemAvailable_bytes{instance=\"%s\"}/node_memory_MemTotal_bytes{instance=\"%s\"}))[%s:30s])", nodeName, nodeName, period)

	for _, metric := range []string{cpuQueryStr, memQueryStr} {
		res, warnings, err := v1api.Query(ctx, metric, time.Now())
		if err != nil {
			klog.Errorf("Error querying Prometheus: %v", err)
		}
		if len(warnings) > 0 {
			klog.V(3).Infof("Warning querying Prometheus: %v", warnings)
		}
		if res == nil || res.String() == "" {
			klog.Warningf("Warning querying Prometheus: no data found for %s", metric)
			continue
		}
		// plugin.usage only need type pmodel.ValVector in Prometheus.rulues
		if res.Type() != pmodel.ValVector {
			continue
		}
		// only method res.String() can get data, dataType []pmodel.ValVector, eg: "{k1:v1, ...} => #[value] @#[timespace]\n {k2:v2, ...} => ..."
		firstRowValVector := strings.Split(res.String(), "\n")[0]
		rowValues := strings.Split(strings.TrimSpace(firstRowValVector), "=>")
		value := strings.Split(strings.TrimSpace(rowValues[1]), " ")
		switch metric {
		case cpuQueryStr:
			cpuUsage, err := strconv.ParseFloat(value[0], 64)
			if err != nil {
				klog.Warning("Warning: Convert cpuUsage to float fail")
			}
			nodeMetrics.CPU = cpuUsage
		case memQueryStr:
			memUsage, err := strconv.ParseFloat(value[0], 64)
			if err != nil {
				klog.Warning("Warning: Convert memUsage to float fail")
			}
			nodeMetrics.Memory = memUsage
		}
	}
	return nodeMetrics, nil
}

func (p *PrometheusMetricsClient) PodsMetricsAvg(ctx context.Context, pods []*v1.Pod, period string) (map[types.NamespacedName]map[v1.ResourceName]*resource.Quantity, error) {
	var ret = make(map[types.NamespacedName]map[v1.ResourceName]*resource.Quantity)
	var client api.Client
	var err error
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{},
	}
	client, err = api.NewClient(api.Config{
		Address:      p.address,
		RoundTripper: transport,
	})
	if err != nil {
		return nil, err
	}
	v1api := prometheusv1.NewAPI(client)
	cpuQuery := "avg_over_time((( (irate(container_cpu_usage_seconds_total{pod=\"%s\",container=\"\",name=\"\" }[30s])) * 1))[%s:30s])"
	memQuery := "container_memory_usage_bytes{pod=\"%s\",container=\"\",name=\"\"}"
	var cpuQueryStr, memQueryStr string
	for _, pod := range pods {
		tmpMap := make(map[v1.ResourceName]*resource.Quantity)
		cpuQueryStr = fmt.Sprintf(cpuQuery, pod.Name, period)
		memQueryStr = fmt.Sprintf(memQuery, pod.Name)
		for _, metric := range []string{cpuQueryStr, memQueryStr} {
			res, warnings, err := v1api.Query(ctx, metric, time.Now())
			if err != nil {
				klog.Errorf("Error querying Prometheus: %v", err)
			}
			if len(warnings) > 0 {
				klog.V(3).Infof("Warning querying Prometheus: %v", warnings)
			}
			if res == nil || res.String() == "" {
				klog.Warningf("Warning querying Prometheus: no data found for %s", metric)
				continue
			}
			// plugin.usage only need type pmodel.ValVector in Prometheus.rulues
			if res.Type() != pmodel.ValVector {
				continue
			}
			// only method res.String() can get data, dataType []pmodel.ValVector, eg: "{k1:v1, ...} => #[value] @#[timespace]\n {k2:v2, ...} => ..."
			firstRowValVector := strings.Split(res.String(), "\n")[0]
			rowValues := strings.Split(strings.TrimSpace(firstRowValVector), "=>")
			value := strings.Split(strings.TrimSpace(rowValues[1]), " ")

			tmp := resource.MustParse(value[0])
			switch metric {
			case cpuQueryStr:
				tmpMap[v1.ResourceCPU] = &tmp
			case memQueryStr:
				tmpMap[v1.ResourceMemory] = &tmp
			}
		}
		ret[types.NamespacedName{
			Namespace: pod.Namespace,
			Name:      pod.Name,
		}] = tmpMap

	}
	return ret, err
}
