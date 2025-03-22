package validate

import (
	"fmt"
)

type Vertex struct {
	Key      string
	Parents  []*Vertex
	Children []*Vertex
	Value    interface{}
}

type DAG struct {
	Vertexes []*Vertex
}

func (dag *DAG) AddVertex(v *Vertex) {
	dag.Vertexes = append(dag.Vertexes, v)
}

func (dag *DAG) AddEdge(from, to *Vertex) {
	from.Children = append(from.Children, to)

	to.Parents = append(from.Parents, from)
}

func (dag *DAG) BFS(root *Vertex) error {
	var q []*Vertex

	visitMap := make(map[string]bool)
	visitMap[root.Key] = true

	q = append(q, root)

	for len(q) > 0 {
		current := q[0]
		q = q[1:]

		for _, v := range current.Children {
			if visitMap[v.Key] {
				return fmt.Errorf("find bad dependency, please check the dependencies of your templates")
			}
			visitMap[v.Key] = true
			q = append(q, v)
		}
	}

	return nil
}
