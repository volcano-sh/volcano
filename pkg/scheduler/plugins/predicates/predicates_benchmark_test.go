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

package predicates

import (
	"context"
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	fwk "k8s.io/kube-scheduler/framework"
	schedframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumerestrictions"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
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

// BenchmarkSimulatePredicate compares running all registered filters versus
// SimulateFilterOrder through Session.SimulatePredicateFn.
func BenchmarkSimulatePredicate(b *testing.B) {
	for _, withVolume := range []bool{false, true} {
		for _, podCount := range []int{0, 10, 50} {
			b.Run(fmt.Sprintf("volume=%t/pods=%d", withVolume, podCount), func(b *testing.B) {
				ResetVolumeBindingPluginForTest()
				defer ResetVolumeBindingPluginForTest()

				var pp *PredicatesPlugin
				node := util.BuildNode(
					"node",
					api.BuildResourceList("100", "100Gi", api.ScalarResource{Name: "pods", Value: "200"}),
					map[string]string{
						v1.LabelHostname:     "node",
						v1.LabelTopologyZone: "zone-a",
					},
				)
				incoming := util.BuildPod(
					"default", "incoming", "", v1.PodPending,
					api.BuildResourceList("10m", "16Mi"), "job",
					nil, map[string]string{v1.LabelHostname: "node"},
				)

				fixture := uthelper.TestCommonStruct{
					Plugins: map[string]framework.PluginBuilder{
						PluginName: func(args framework.Arguments) framework.Plugin {
							pp = New(args).(*PredicatesPlugin)
							return pp
						},
					},
					Nodes: []*v1.Node{node},
				}
				for i := range podCount {
					fixture.Pods = append(fixture.Pods, util.BuildPod(
						"default", fmt.Sprintf("pod-%d", i), "node", v1.PodRunning,
						api.BuildResourceList("10m", "16Mi"), "existing", nil, nil,
					))
				}
				if withVolume {
					incoming.Spec.Volumes = []v1.Volume{
						{
							Name: "data",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{ClaimName: "data"},
							},
						},
					}
					fixture.PVCs = []*v1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{
								Namespace:   "default",
								Name:        "data",
								Annotations: map[string]string{"pv.kubernetes.io/bind-completed": "yes"},
							},
							Spec: v1.PersistentVolumeClaimSpec{
								VolumeName:  "data",
								AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
							},
							Status: v1.PersistentVolumeClaimStatus{Phase: v1.ClaimBound},
						},
					}
					fixture.PVs = []*v1.PersistentVolume{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name:   "data",
								Labels: map[string]string{v1.LabelTopologyZone: "zone-a"},
							},
							Spec: v1.PersistentVolumeSpec{
								PersistentVolumeSource: v1.PersistentVolumeSource{
									HostPath: &v1.HostPathVolumeSource{Path: "/data"},
								},
								AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
								ClaimRef:    &v1.ObjectReference{Namespace: "default", Name: "data"},
							},
						},
					}
				}

				enabled := true
				tiers := []conf.Tier{
					{
						Plugins: []conf.PluginOption{
							{Name: PluginName, EnabledPredicate: &enabled},
						},
					},
				}
				ssn := fixture.RegisterSession(tiers, nil)
				defer fixture.Close()

				task := api.NewTaskInfo(incoming)
				if err := ssn.PrePredicateFn(task); err != nil {
					b.Fatal(err)
				}
				if err := ssn.PredicateForPreemptAction(task, ssn.Nodes["node"]); err != nil {
					b.Fatal(err)
				}

				selectedOrder := pp.SimulateFilterOrder
				for _, mode := range []string{"all", "selected"} {
					b.Run(mode, func(b *testing.B) {
						if mode == "all" {
							pp.SimulateFilterOrder = pp.FilterOrder
						} else {
							pp.SimulateFilterOrder = selectedOrder
						}
						state := ssn.GetCycleState(task.UID).Clone()
						nodeCopy := ssn.Nodes["node"].Clone()

						b.ReportAllocs()
						for b.Loop() {
							if err := ssn.SimulatePredicateFn(context.Background(), state, task, nodeCopy); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			})
		}
	}
}

// BenchmarkVolumeRestrictionsPreFilter measures VolumeRestrictions.PreFilter
// across varying cluster sizes, per-node PVC counts, and RWOP task ratios.
func BenchmarkVolumeRestrictionsPreFilter(b *testing.B) {
	for _, claimsPerNode := range []int{0, 10} {
		for _, nodeCount := range []int{1000, 5000} {
			for _, rwopPercent := range []int{0, 1, 5, 100} {
				b.Run(fmt.Sprintf("nodes=%d/PVCsPerNode=%d/RWOP=%d%%", nodeCount, claimsPerNode, rwopPercent), func(b *testing.B) {
					nodes := make(map[string]fwk.NodeInfo, nodeCount)
					for i := range nodeCount {
						name := fmt.Sprintf("node-%d", i)
						node := schedframework.NewNodeInfo()
						node.SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}})
						for j := range claimsPerNode {
							claim := fmt.Sprintf("data-%d-%d", i, j)
							node.AddPod(newBenchmarkPodWithPVC(claim, name, claim))
						}
						nodes[name] = node
					}

					factory := informers.NewSharedInformerFactory(k8sfake.NewSimpleClientset(), 0)
					store := factory.Core().V1().PersistentVolumeClaims().Informer().GetStore()
					for _, mode := range []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce, v1.ReadWriteOncePod} {
						claim := &v1.PersistentVolumeClaim{
							ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: string(mode)},
							Spec:       v1.PersistentVolumeClaimSpec{AccessModes: []v1.PersistentVolumeAccessMode{mode}},
						}
						if err := store.Add(claim); err != nil {
							b.Fatal(err)
						}
					}

					pp := New(framework.Arguments{
						NodeAffinityEnable:       false,
						NodePortsEnable:          false,
						TaintTolerationEnable:    false,
						PodAffinityEnable:        false,
						NodeVolumeLimitsEnable:   false,
						VolumeZoneEnable:         false,
						PodTopologySpreadEnable:  false,
						VolumeBindingEnable:      false,
						VolumeRestrictionsEnable: true,
					}).(*PredicatesPlugin)
					pp.enabledPredicates.dynamicResourceAllocationEnable = false
					pp.enabledPredicates.nodeDeclaredFeaturesEnable = false
					pp.Handle = k8s.NewFramework(nodes, k8s.WithInformerFactory(factory))
					pp.InitPlugin()
					if len(pp.PreFilterOrder) != 1 || pp.PreFilterOrder[0] != volumerestrictions.Name {
						b.Fatalf("unexpected PreFilter plugins: %v", pp.PreFilterOrder)
					}

					nodeList, err := pp.Handle.SnapshotSharedLister().NodeInfos().List()
					if err != nil {
						b.Fatal(err)
					}

					tasks := make([]*api.TaskInfo, 100)
					for i := range tasks {
						claim := string(v1.ReadWriteOnce)
						if i < rwopPercent {
							claim = string(v1.ReadWriteOncePod)
						}
						tasks[i] = api.NewTaskInfo(newBenchmarkPodWithPVC(fmt.Sprintf("pending-%d", i), "", claim))
					}

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; b.Loop(); i++ {
						if err := pp.PrePredicate(tasks[i%len(tasks)], schedframework.NewCycleState(), nodeList); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		}
	}
}
