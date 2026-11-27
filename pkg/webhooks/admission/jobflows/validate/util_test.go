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

package validate

import (
	"fmt"
	"testing"
	"time"
)

func TestIsDAG(t *testing.T) {
	tests := []struct {
		graph map[string][]string
		isDAG bool
		isErr bool
	}{
		{map[string][]string{"1": {"2"}, "2": {"1", "3"}}, false, true},
		{map[string][]string{"1": {"2"}, "2": {"3"}, "3": {"1"}}, false, false},
		{map[string][]string{"1": {"2"}, "2": {"1", "3"}, "3": {}}, false, false},
		{map[string][]string{"1": {"2", "3"}, "2": {"3"}, "3": {"4"}, "4": {}}, true, false},
		{map[string][]string{"1": {"2", "3"}, "2": {"4", "5"}, "3": {}, "4": {}, "5": {}}, true, false},
		{map[string][]string{"1": {"2", "3", "4"}, "2": {"3"}, "3": {"4"}, "4": {"5"}, "5": {}}, true, false},
		{map[string][]string{"1": {"2", "3", "4"}, "2": {"3", "5"}, "3": {"5"}, "4": {"3", "5"}, "5": {}}, true, false},
		{map[string][]string{"1": {"2", "3", "4"}, "2": {"3", "5"}, "3": {"5"}, "4": {"3", "5"}, "5": {"5", "3"}}, false, false},
	}

	for i, test := range tests {
		data, err := LoadVertexs(test.graph)
		if (err != nil) != test.isErr {
			t.Fatalf("Failed case #%d. LoadVertexs error %v, data: %v", i, err, data)
		}

		if test.isErr == false {
			if got := IsDAG(data); got != test.isDAG && test.isErr == false {
				t.Fatalf("Failed case #%d. Want %v got %v", i, test.isDAG, got)
			}
		}
	}
}

// TestIsDAGLayeredGraph checks that IsDAG stays linear on a layered DAG where
// every flow depends on both flows of the next layer. Such a graph has 2^layers
// distinct paths, so a DFS that re-walks shared descendants never finishes.
func TestIsDAGLayeredGraph(t *testing.T) {
	const layers = 64
	graph := make(map[string][]string, 2*layers)
	for l := 0; l < layers; l++ {
		for w := 0; w < 2; w++ {
			name := fmt.Sprintf("flow-%d-%d", l, w)
			if l+1 < layers {
				graph[name] = []string{fmt.Sprintf("flow-%d-0", l+1), fmt.Sprintf("flow-%d-1", l+1)}
			} else {
				graph[name] = []string{}
			}
		}
	}
	data, err := LoadVertexs(graph)
	if err != nil {
		t.Fatalf("LoadVertexs error %v", err)
	}

	result := make(chan bool, 1)
	go func() { result <- IsDAG(data) }()
	select {
	case got := <-result:
		if !got {
			t.Fatalf("Want layered graph to be a DAG")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("IsDAG did not finish on a %d-layer graph", layers)
	}

	graph["flow-63-0"] = []string{"flow-0-0"}
	data, err = LoadVertexs(graph)
	if err != nil {
		t.Fatalf("LoadVertexs error %v", err)
	}
	if IsDAG(data) {
		t.Fatalf("Want layered graph with back edge to not be a DAG")
	}
}
