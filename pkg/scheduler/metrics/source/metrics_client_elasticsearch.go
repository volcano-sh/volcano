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
	"encoding/json"
	"github.com/elastic/go-elasticsearch/v7"
)

const (
	// esHostNameField is the field name of host name in the document
	esHostNameField = "host.hostname"
	// esCpuUsageField is the field name of cpu usage in the document
	esCpuUsageField = "host.cpu.usage"
	// esMemUsageField is the field name of mem usage in the document
	esMemUsageField = "system.memory.actual.used.pct"
)

type ElasticsearchMetricsClient struct {
	address string
	es      *elasticsearch.Client
}

func NewElasticsearchMetricsClient(address string) (*ElasticsearchMetricsClient, error) {
	e := &ElasticsearchMetricsClient{address: address}
	var err error
	e.es, err = elasticsearch.NewDefaultClient()
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (p *ElasticsearchMetricsClient) NodeMetricsAvg(ctx context.Context, nodeName string, period string) (*NodeMetrics, error) {
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
								"gte": "now-" + period,
								"lt":  "now",
							},
						},
					},
					{
						"term": map[string]interface{}{
							esHostNameField: nodeName,
						},
					},
				},
			},
		},
		"aggs": map[string]interface{}{
			"cpu": map[string]interface{}{
				"avg": map[string]interface{}{
					"field": esCpuUsageField,
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
	res, err := p.es.Search(
		p.es.Search.WithContext(ctx),
		p.es.Search.WithIndex("metricbeat-*"),
		p.es.Search.WithBody(&buf),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var r map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, err
	}
	aggs := r["aggregations"].(map[string]interface{})
	cpuUsage := aggs["cpu"].(map[string]interface{})["value"].(float64)
	memUsage := aggs["mem"].(map[string]interface{})["value"].(float64)
	nodeMetrics.Cpu = cpuUsage
	nodeMetrics.Memory = memUsage
	return nodeMetrics, nil
}
