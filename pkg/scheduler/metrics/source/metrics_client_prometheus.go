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
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	pmodel "github.com/prometheus/common/model"
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
	conf    map[string]string
}

func NewPrometheusMetricsClient(address string, conf map[string]string) (*PrometheusMetricsClient, error) {
	return &PrometheusMetricsClient{address: address, conf: conf}, nil
}

var Period string
func (p *PrometheusMetricsClient) NodeMetricsAvg(ctx context.Context, nodeName string, period string) (*NodeMetrics, error) {
	klog.V(4).Infof("Get node metrics from Prometheus: %s", p.address)
	var client api.Client
	var err error
	insecureSkipVerify := p.conf["tls.insecureSkipVerify"] == "true"
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecureSkipVerify,
		},
	}
	client, err = api.NewClient(api.Config{
		Address:      p.address,
		RoundTripper: tr,
	})
	if err != nil {
		return nil, err
	}
	v1api := prometheusv1.NewAPI(client)
	nodeMetrics := &NodeMetrics{}
	cpuQueryStr := fmt.Sprintf("avg_over_time((100 - (avg by (instance) (irate(node_cpu_seconds_total{mode=\"idle\",instance=\"%s\"}[30s])) * 100))[%s:30s])", nodeName, period)
	memQueryStr := fmt.Sprintf("100*avg_over_time(((1-node_memory_MemAvailable_bytes{instance=\"%s\"}/node_memory_MemTotal_bytes{instance=\"%s\"}))[%s:30s])", nodeName, nodeName, period)

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
			cpuUsage, _ := strconv.ParseFloat(value[0], 64)
			nodeMetrics.CPU = cpuUsage
		case memQueryStr:
			memUsage, _ := strconv.ParseFloat(value[0], 64)
			nodeMetrics.Memory = memUsage
		}
	}
	return nodeMetrics, nil
}
