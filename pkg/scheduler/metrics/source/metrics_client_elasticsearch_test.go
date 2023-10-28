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

import "testing"

func TestElasticsearchMetricsClientDefaultIndexName(t *testing.T) {
	client, err := NewElasticsearchMetricsClient(map[string]string{"address": "http://localhost:9200"})
	if err != nil {
		t.Errorf("Failed to create client: %v", err)
	}
	if client.indexName != "metricbeat-*" {
		t.Errorf("Default index name should be metricbeat-*")
	}
}

func TestElasticsearchMetricsClientCustomIndexName(t *testing.T) {
	client, err := NewElasticsearchMetricsClient(map[string]string{"address": "http://localhost:9200", "elasticsearch.index": "custom-index"})
	if err != nil {
		t.Errorf("Failed to create client: %v", err)
	}
	if client.indexName != "custom-index" {
		t.Errorf("Custom index name should be custom-index")
	}
}
