/*
Copyright 2017 The Kubernetes Authors.

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

package drf

import (
	"flag"
	"fmt"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
)

func init() {
	logLevel := os.Getenv("TEST_LOG_LEVEL")
	if len(logLevel) != 0 {
		flag.Parse()
		flag.Lookup("logtostderr").Value.Set("true")
		flag.Lookup("v").Value.Set(logLevel)
	}
}

func TestAllocate(t *testing.T) {
	tests := []struct {
		name      string
		consumers map[string]*cache.ConsumerInfo
		nodes     []*cache.NodeInfo
		expected  map[string]string
	}{
		{
			name: "one consumer with two Pods on one node",
			consumers: map[string]*cache.ConsumerInfo{
				"c1": &cache.ConsumerInfo{
					PodSets: []*cache.PodSet{
						&cache.PodSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "c1",
								Name:      "ps1",
							},
							Pending: []*cache.PodInfo{
								&cache.PodInfo{
									Name:      "p1",
									Namespace: "c1",
									Request: &cache.Resource{
										MilliCPU: 1.0,
										Memory:   100,
									},
								},
								&cache.PodInfo{
									Name:      "p2",
									Namespace: "c1",
									Request: &cache.Resource{
										MilliCPU: 1.0,
										Memory:   100,
									},
								},
							},
							Allocated: cache.EmptyResource(),
						},
					},
				},
			},
			nodes: []*cache.NodeInfo{
				&cache.NodeInfo{
					Name: "n1",
					Idle: &cache.Resource{
						MilliCPU: 2.0,
						Memory:   1024,
					},
					Allocatable: &cache.Resource{
						MilliCPU: 2.0,
						Memory:   1024,
					},
				},
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
		},
		{
			name: "two consumer on one node",
			consumers: map[string]*cache.ConsumerInfo{
				"c1": &cache.ConsumerInfo{
					PodSets: []*cache.PodSet{
						&cache.PodSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "c1",
								Name:      "ps1",
							},
							Pending: []*cache.PodInfo{
								&cache.PodInfo{
									Name:      "p1",
									Namespace: "c1",
									Request: &cache.Resource{
										MilliCPU: 1.0,
										Memory:   100,
									},
								},
								&cache.PodInfo{
									Name:      "p2",
									Namespace: "c1",
									Request: &cache.Resource{
										MilliCPU: 1.0,
										Memory:   100,
									},
								},
							},
							Allocated: cache.EmptyResource(),
						},
					},
				},
				"c2": &cache.ConsumerInfo{
					PodSets: []*cache.PodSet{
						&cache.PodSet{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "c2",
								Name:      "ps1",
							},
							Pending: []*cache.PodInfo{
								&cache.PodInfo{
									Name:      "p1",
									Namespace: "c2",
									Request: &cache.Resource{
										MilliCPU: 1.0,
										Memory:   100,
									},
								},
								&cache.PodInfo{
									Name:      "p2",
									Namespace: "c2",
									Request: &cache.Resource{
										MilliCPU: 1.0,
										Memory:   100,
									},
								},
							},
							Allocated: cache.EmptyResource(),
						},
					},
				},
			},
			nodes: []*cache.NodeInfo{
				&cache.NodeInfo{
					Name: "n1",
					Idle: &cache.Resource{
						MilliCPU: 2.0,
						Memory:   1024,
					},
					Allocatable: &cache.Resource{
						MilliCPU: 2.0,
						Memory:   1024,
					},
				},
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c2/p1": "n1",
			},
		},
	}

	drf := New()

	for i, test := range tests {
		expected := drf.Allocate(test.consumers, test.nodes)
		for _, consumer := range expected {
			for _, ps := range consumer.PodSets {
				for _, p := range ps.Pending {
					pk := fmt.Sprintf("%v/%v", p.Namespace, p.Name)
					if p.Nodename != test.expected[pk] {
						t.Errorf("case %d (%s): %v/%v expected %s, got %s",
							i, test.name, p.Namespace, p.Name, test.expected[pk], p.Nodename)
					}
				}
			}
		}
	}
}
