/*
Copyright 2026 The Volcano Authors.

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

package k8s

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

func newBenchmarkPodWithPVC(name, nodeName, claimName string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       types.UID(name),
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Volumes: []v1.Volume{
				{
					Name: "data",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: claimName,
						},
					},
				},
			},
		},
	}
}

func BenchmarkSnapshotIsPVCUsedByPods(b *testing.B) {
	cases := []struct {
		name          string
		claimsPerNode int
		hit           string
	}{
		{name: "empty_nodes"},
		{name: "unused", claimsPerNode: 10},
		{name: "first_node", claimsPerNode: 10, hit: "first"},
		{name: "last_node", claimsPerNode: 10, hit: "last"},
	}

	for _, nodeCount := range []int{100, 1000, 5000, 10000} {
		for _, tc := range cases {
			b.Run(fmt.Sprintf("nodes=%d/%s", nodeCount, tc.name), func(b *testing.B) {
				nodes := make(map[string]fwk.NodeInfo, nodeCount)
				for i := range nodeCount {
					name := fmt.Sprintf("node-%d", i)
					node := framework.NewNodeInfo()
					node.SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
					for j := range tc.claimsPerNode {
						claim := fmt.Sprintf("data-%d-%d", i, j)
						node.AddPod(newBenchmarkPodWithPVC(claim, name, claim))
					}
					nodes[name] = node
				}

				snapshot := NewSnapshot(nodes)
				if tc.hit != "" {
					list, err := snapshot.List()
					if err != nil {
						b.Fatal(err)
					}
					index := 0
					if tc.hit == "last" {
						index = len(list) - 1
					}
					// Use the actual snapshot order, which is independent of map iteration order.
					node := list[index].(*framework.NodeInfo)
					node.AddPod(newBenchmarkPodWithPVC("holder", node.Node().Name, "target"))
				}

				want := tc.hit != ""
				if got := snapshot.IsPVCUsedByPods("default/target"); got != want {
					b.Fatalf("got %t, want %t", got, want)
				}

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if snapshot.IsPVCUsedByPods("default/target") != want {
						b.Fatal("PVC usage changed")
					}
				}
			})
		}
	}
}
