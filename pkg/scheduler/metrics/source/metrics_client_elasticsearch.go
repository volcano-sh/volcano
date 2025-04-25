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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
	"k8s.io/klog/v2"
)

const (
	// esHostNameField is the field name of host name in the document
	esHostNameField = "host.hostname"
	// esCPUUsageField is the field name of cpu usage in the document
	esCPUUsageField = "host.cpu.usage"
	// esMemUsageField is the field name of mem usage in the document
	esMemUsageField = "system.memory.actual.used.pct"
)

type ElasticsearchMetricsClient struct {
	address           string
	indexName         string
	es                *elasticsearch.Client
	hostnameFieldName string
}

func NewElasticsearchMetricsClient(conf map[string]string) (*ElasticsearchMetricsClient, error) {
	address := conf["address"]
	if len(address) == 0 {
		return nil, errors.New("metrics address is empty")
	}

	e := &ElasticsearchMetricsClient{address: address}
	indexName := conf["elasticsearch.index"]
	if len(indexName) == 0 {
		e.indexName = "metricbeat-*"
	} else {
		e.indexName = indexName
	}
	hostNameFieldName := conf["elasticsearch.hostnameFieldName"]
	if len(hostNameFieldName) == 0 {
		e.hostnameFieldName = esHostNameField
	} else {
		e.hostnameFieldName = hostNameFieldName
	}
	var err error
	insecureSkipVerify := conf["tls.insecureSkipVerify"] == "true"
	if insecureSkipVerify {
		klog.Warningf("WARNING: TLS certificate verification is disabled which is insecure. This should not be used in production environments")
	}

	e.es, err = elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{address},
		Username:  conf["elasticsearch.username"],
		Password:  conf["elasticsearch.password"],
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecureSkipVerify,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (e *ElasticsearchMetricsClient) NodesMetricsAvg(ctx context.Context, nodeMetricsMap map[string]*NodeMetrics) error {
	for nodeName := range nodeMetricsMap {
		nodeMetrics, err := e.NodeMetricsAvg(ctx, nodeName)
		if err != nil {
			return err
		}
		nodeMetricsMap[nodeName] = nodeMetrics
	}
	return nil
}

func (e *ElasticsearchMetricsClient) NodeMetricsAvg(ctx context.Context, nodeName string) (*NodeMetrics, error) {
	nodeMetrics := &NodeMetrics{}
	var buf bytes.Buffer
	query := map[string]interface{}{
		"size": 0,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []map[string]interface{}{
					{
						"range": map[string]interface{}{
							"@timestamp": map[string]interface{}{
								"gte": "now-" + NODE_METRICS_PERIOD,
								"lt":  "now",
							},
						},
					},
					{
						"term": map[string]interface{}{
							e.hostnameFieldName: nodeName,
						},
					},
				},
			},
		},
		"aggs": map[string]interface{}{
			"cpu": map[string]interface{}{
				"avg": map[string]interface{}{
					"field": esCPUUsageField,
				},
			},
			"mem": map[string]interface{}{
				"avg": map[string]interface{}{
					"field": esMemUsageField,
				},
			},
		},
	}
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		return nil, err
	}
	res, err := e.es.Search(
		e.es.Search.WithContext(ctx),
		e.es.Search.WithIndex(e.indexName),
		e.es.Search.WithBody(&buf),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var r struct {
		Aggregations struct {
			CPU struct {
				Value float64 `json:"value"`
			}
			Mem struct {
				Value float64 `json:"value"`
			}
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, err
	}
	// The data obtained from Elasticsearch is in decimals and needs to be multiplied by 100.
	nodeMetrics.CPU = r.Aggregations.CPU.Value * 100
	nodeMetrics.Memory = r.Aggregations.Mem.Value * 100
	nodeMetrics.MetricsTime = time.Now()
	return nodeMetrics, nil
}
