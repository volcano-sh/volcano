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
)

type Vertex struct {
	Edges []*Vertex
	Name  string
}

func NewVertex(name string) *Vertex { return &Vertex{Name: name, Edges: []*Vertex{}} }

func LoadVertexs(data map[string][]string) ([]*Vertex, error) {
	graphMap := make(map[string]*Vertex)
	output := make([]*Vertex, len(graphMap))

	for name := range data {
		graphMap[name] = NewVertex(name)
	}

	for name, values := range data {
		graphMap[name].Edges = make([]*Vertex, len(values))
		for index, value := range values {
			if _, ok := graphMap[value]; !ok {
				return output, fmt.Errorf("%s: %s", VertexNotDefinedError.Error(), value)
			}
			graphMap[name].Edges[index] = graphMap[value]
		}
	}

	for _, value := range graphMap {
		output = append(output, value)
	}
	return output, nil
}

// IsDAG solves the problem in O(V+E) time and O(V) space.
func IsDAG(graph []*Vertex) bool {
	for _, vertex := range graph {
		visited := make(map[*Vertex]struct{})
		visited[vertex] = struct{}{}
		if hasCycle(vertex, visited) {
			return false
		}
	}
	return true
}

func hasCycle(vertex *Vertex, visited map[*Vertex]struct{}) bool {
	for _, neighbor := range vertex.Edges {
		if _, ok := visited[neighbor]; !ok {
			visited[neighbor] = struct{}{}
			if hasCycle(neighbor, visited) {
				return true
			}
			delete(visited, neighbor)
		} else {
			return true
		}
	}
	return false
}
