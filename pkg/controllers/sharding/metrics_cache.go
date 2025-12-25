/*
Copyright 2025 The Volcano Authors.
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

package sharding

// Implement NodeMetricsProvider interface
func (sc *ShardingController) GetNodeMetrics(nodeName string) *NodeMetrics {
	return sc.nodeMetricsCache[nodeName]
}

func (sc *ShardingController) GetAllNodeMetrics() map[string]*NodeMetrics {
	sc.metricsMutex.RLock()
	defer sc.metricsMutex.RUnlock()

	// Create a copy to avoid concurrent modification
	metricsCopy := make(map[string]*NodeMetrics, len(sc.nodeMetricsCache))
	for name, metrics := range sc.nodeMetricsCache {
		metricsCopy[name] = metrics
	}
	return metricsCopy
}

func (sc *ShardingController) UpdateNodeMetrics(nodeName string, metrics *NodeMetrics) {
	// Update or create metrics
	if sc.nodeMetricsCache == nil {
		sc.nodeMetricsCache = make(map[string]*NodeMetrics)
	}
	sc.nodeMetricsCache[nodeName] = metrics
}
