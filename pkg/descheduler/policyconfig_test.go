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

package descheduler

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	utilpointer "k8s.io/utils/pointer"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/api/v1alpha1"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/defaultevictor"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/nodeutilization"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/podlifetime"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removeduplicates"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removefailedpods"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodshavingtoomanyrestarts"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatinginterpodantiaffinity"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingnodeaffinity"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingnodetaints"
	"sigs.k8s.io/descheduler/pkg/framework/plugins/removepodsviolatingtopologyspreadconstraint"
	"sigs.k8s.io/descheduler/pkg/utils"
)

func TestV1alpha1ToV1alpha2(t *testing.T) {
	SetupPlugins()
	defaultEvictorPluginConfig := api.PluginConfig{
		Name: defaultevictor.PluginName,
		Args: &defaultevictor.DefaultEvictorArgs{
			PriorityThreshold: &api.PriorityThreshold{
				Value: nil,
			},
		},
	}
	type testCase struct {
		description string
		policy      *v1alpha1.DeschedulerPolicy
		err         error
		result      *api.DeschedulerPolicy
	}
	testCases := []testCase{
		{
			description: "RemoveFailedPods enabled, LowNodeUtilization disabled strategies to profile",
			policy: &v1alpha1.DeschedulerPolicy{
				Strategies: v1alpha1.StrategyList{
					removeduplicates.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							Namespaces: &v1alpha1.Namespaces{
								Exclude: []string{
									"test2",
								},
							},
						},
					},
					nodeutilization.LowNodeUtilizationPluginName: v1alpha1.DeschedulerStrategy{
						Enabled: false,
						Params: &v1alpha1.StrategyParameters{
							NodeResourceUtilizationThresholds: &v1alpha1.NodeResourceUtilizationThresholds{
								Thresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(20),
									"memory": v1alpha1.Percentage(20),
									"pods":   v1alpha1.Percentage(20),
								},
								TargetThresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(50),
									"memory": v1alpha1.Percentage(50),
									"pods":   v1alpha1.Percentage(50),
								},
							},
						},
					},
				},
			},
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: fmt.Sprintf("strategy-%s-profile", removeduplicates.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removeduplicates.PluginName,
								Args: &removeduplicates.RemoveDuplicatesArgs{
									Namespaces: &api.Namespaces{
										Exclude: []string{
											"test2",
										},
									},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{removeduplicates.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					// Disabled strategy is not generating internal plugin since it is not being used internally currently
					// {
					// 	Name: nodeutilization.LowNodeUtilizationPluginName,
					// 	PluginConfigs: []api.PluginConfig{
					// 		{
					// 			Name: nodeutilization.LowNodeUtilizationPluginName,
					// 			Args: &nodeutilization.LowNodeUtilizationArgs{
					// 				Thresholds: api.ResourceThresholds{
					// 					"cpu":    api.Percentage(20),
					// [...]
					// [...]
					// },
				},
			},
		},
		{
			description: "convert global policy fields to defaultevictor",
			policy: &v1alpha1.DeschedulerPolicy{
				EvictFailedBarePods:     utilpointer.Bool(true),
				EvictLocalStoragePods:   utilpointer.Bool(true),
				EvictSystemCriticalPods: utilpointer.Bool(true),
				EvictDaemonSetPods:      utilpointer.Bool(true),
				IgnorePVCPods:           utilpointer.Bool(true),
				Strategies: v1alpha1.StrategyList{
					removeduplicates.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							Namespaces: &v1alpha1.Namespaces{
								Exclude: []string{
									"test2",
								},
							},
						},
					},
				},
			},
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: fmt.Sprintf("strategy-%s-profile", removeduplicates.PluginName),
						PluginConfigs: []api.PluginConfig{
							{
								Name: defaultevictor.PluginName,
								Args: &defaultevictor.DefaultEvictorArgs{
									EvictLocalStoragePods:   true,
									EvictDaemonSetPods:      true,
									EvictSystemCriticalPods: true,
									IgnorePvcPods:           true,
									EvictFailedBarePods:     true,
									PriorityThreshold: &api.PriorityThreshold{
										Value: nil,
									},
								},
							},
							{
								Name: removeduplicates.PluginName,
								Args: &removeduplicates.RemoveDuplicatesArgs{
									Namespaces: &api.Namespaces{
										Exclude: []string{
											"test2",
										},
									},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{removeduplicates.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
				},
			},
		},
		{
			description: "convert all strategies",
			policy: &v1alpha1.DeschedulerPolicy{
				Strategies: v1alpha1.StrategyList{
					removeduplicates.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params:  &v1alpha1.StrategyParameters{},
					},
					nodeutilization.LowNodeUtilizationPluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							NodeResourceUtilizationThresholds: &v1alpha1.NodeResourceUtilizationThresholds{
								Thresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(20),
									"memory": v1alpha1.Percentage(20),
									"pods":   v1alpha1.Percentage(20),
								},
								TargetThresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(50),
									"memory": v1alpha1.Percentage(50),
									"pods":   v1alpha1.Percentage(50),
								},
							},
						},
					},
					nodeutilization.HighNodeUtilizationPluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							NodeResourceUtilizationThresholds: &v1alpha1.NodeResourceUtilizationThresholds{
								Thresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(20),
									"memory": v1alpha1.Percentage(20),
									"pods":   v1alpha1.Percentage(20),
								},
							},
						},
					},
					removefailedpods.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params:  &v1alpha1.StrategyParameters{},
					},
					removepodshavingtoomanyrestarts.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							PodsHavingTooManyRestarts: &v1alpha1.PodsHavingTooManyRestarts{
								PodRestartThreshold: 100,
							},
						},
					},
					removepodsviolatinginterpodantiaffinity.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params:  &v1alpha1.StrategyParameters{},
					},
					removepodsviolatingnodeaffinity.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							NodeAffinityType: []string{"requiredDuringSchedulingIgnoredDuringExecution"},
						},
					},
					removepodsviolatingnodetaints.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params:  &v1alpha1.StrategyParameters{},
					},
					removepodsviolatingtopologyspreadconstraint.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params:  &v1alpha1.StrategyParameters{},
					},
				},
			},
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: fmt.Sprintf("strategy-%s-profile", nodeutilization.HighNodeUtilizationPluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: nodeutilization.HighNodeUtilizationPluginName,
								Args: &nodeutilization.HighNodeUtilizationArgs{
									Thresholds: api.ResourceThresholds{
										"cpu":    api.Percentage(20),
										"memory": api.Percentage(20),
										"pods":   api.Percentage(20),
									},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{nodeutilization.HighNodeUtilizationPluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", nodeutilization.LowNodeUtilizationPluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: nodeutilization.LowNodeUtilizationPluginName,
								Args: &nodeutilization.LowNodeUtilizationArgs{
									Thresholds: api.ResourceThresholds{
										"cpu":    api.Percentage(20),
										"memory": api.Percentage(20),
										"pods":   api.Percentage(20),
									},
									TargetThresholds: api.ResourceThresholds{
										"cpu":    api.Percentage(50),
										"memory": api.Percentage(50),
										"pods":   api.Percentage(50),
									},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{nodeutilization.LowNodeUtilizationPluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removeduplicates.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removeduplicates.PluginName,
								Args: &removeduplicates.RemoveDuplicatesArgs{},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{removeduplicates.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removefailedpods.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removefailedpods.PluginName,
								Args: &removefailedpods.RemoveFailedPodsArgs{},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removefailedpods.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodshavingtoomanyrestarts.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodshavingtoomanyrestarts.PluginName,
								Args: &removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs{
									PodRestartThreshold: 100,
								},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodshavingtoomanyrestarts.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatinginterpodantiaffinity.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatinginterpodantiaffinity.PluginName,
								Args: &removepodsviolatinginterpodantiaffinity.RemovePodsViolatingInterPodAntiAffinityArgs{},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodsviolatinginterpodantiaffinity.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatingnodeaffinity.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatingnodeaffinity.PluginName,
								Args: &removepodsviolatingnodeaffinity.RemovePodsViolatingNodeAffinityArgs{
									NodeAffinityType: []string{"requiredDuringSchedulingIgnoredDuringExecution"},
								},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodsviolatingnodeaffinity.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatingnodetaints.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatingnodetaints.PluginName,
								Args: &removepodsviolatingnodetaints.RemovePodsViolatingNodeTaintsArgs{},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodsviolatingnodetaints.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatingtopologyspreadconstraint.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatingtopologyspreadconstraint.PluginName,
								Args: &removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs{
									Constraints:            []v1.UnsatisfiableConstraintAction{v1.DoNotSchedule},
									TopologyBalanceNodeFit: utilpointer.Bool(true),
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{removepodsviolatingtopologyspreadconstraint.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
				},
			},
		},
		{
			description: "pass in all params to check args",
			policy: &v1alpha1.DeschedulerPolicy{
				Strategies: v1alpha1.StrategyList{
					removeduplicates.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							RemoveDuplicates: &v1alpha1.RemoveDuplicates{
								ExcludeOwnerKinds: []string{"ReplicaSet"},
							},
						},
					},
					nodeutilization.LowNodeUtilizationPluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							NodeResourceUtilizationThresholds: &v1alpha1.NodeResourceUtilizationThresholds{
								Thresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(20),
									"memory": v1alpha1.Percentage(20),
									"pods":   v1alpha1.Percentage(20),
								},
								TargetThresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(50),
									"memory": v1alpha1.Percentage(50),
									"pods":   v1alpha1.Percentage(50),
								},
								UseDeviationThresholds: true,
								NumberOfNodes:          3,
							},
						},
					},
					nodeutilization.HighNodeUtilizationPluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							NodeResourceUtilizationThresholds: &v1alpha1.NodeResourceUtilizationThresholds{
								Thresholds: v1alpha1.ResourceThresholds{
									"cpu":    v1alpha1.Percentage(20),
									"memory": v1alpha1.Percentage(20),
									"pods":   v1alpha1.Percentage(20),
								},
							},
						},
					},
					removefailedpods.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							FailedPods: &v1alpha1.FailedPods{
								MinPodLifetimeSeconds:   utilpointer.Uint(3600),
								ExcludeOwnerKinds:       []string{"Job"},
								Reasons:                 []string{"NodeAffinity"},
								IncludingInitContainers: true,
							},
						},
					},
					removepodshavingtoomanyrestarts.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							PodsHavingTooManyRestarts: &v1alpha1.PodsHavingTooManyRestarts{
								PodRestartThreshold:     100,
								IncludingInitContainers: true,
							},
						},
					},
					removepodsviolatinginterpodantiaffinity.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params:  &v1alpha1.StrategyParameters{},
					},
					removepodsviolatingnodeaffinity.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							NodeAffinityType: []string{"requiredDuringSchedulingIgnoredDuringExecution"},
						},
					},
					removepodsviolatingnodetaints.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							ExcludedTaints: []string{"dedicated=special-user", "reserved"},
						},
					},
					removepodsviolatingtopologyspreadconstraint.PluginName: v1alpha1.DeschedulerStrategy{
						Enabled: true,
						Params: &v1alpha1.StrategyParameters{
							IncludeSoftConstraints: true,
						},
					},
				},
			},
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: fmt.Sprintf("strategy-%s-profile", nodeutilization.HighNodeUtilizationPluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: nodeutilization.HighNodeUtilizationPluginName,
								Args: &nodeutilization.HighNodeUtilizationArgs{
									Thresholds: api.ResourceThresholds{
										"cpu":    api.Percentage(20),
										"memory": api.Percentage(20),
										"pods":   api.Percentage(20),
									},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{nodeutilization.HighNodeUtilizationPluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", nodeutilization.LowNodeUtilizationPluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: nodeutilization.LowNodeUtilizationPluginName,
								Args: &nodeutilization.LowNodeUtilizationArgs{
									UseDeviationThresholds: true,
									NumberOfNodes:          3,
									Thresholds: api.ResourceThresholds{
										"cpu":    api.Percentage(20),
										"memory": api.Percentage(20),
										"pods":   api.Percentage(20),
									},
									TargetThresholds: api.ResourceThresholds{
										"cpu":    api.Percentage(50),
										"memory": api.Percentage(50),
										"pods":   api.Percentage(50),
									},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{nodeutilization.LowNodeUtilizationPluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removeduplicates.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removeduplicates.PluginName,
								Args: &removeduplicates.RemoveDuplicatesArgs{
									ExcludeOwnerKinds: []string{"ReplicaSet"},
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{removeduplicates.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removefailedpods.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removefailedpods.PluginName,
								Args: &removefailedpods.RemoveFailedPodsArgs{
									ExcludeOwnerKinds:       []string{"Job"},
									MinPodLifetimeSeconds:   utilpointer.Uint(3600),
									Reasons:                 []string{"NodeAffinity"},
									IncludingInitContainers: true,
								},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removefailedpods.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodshavingtoomanyrestarts.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodshavingtoomanyrestarts.PluginName,
								Args: &removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs{
									PodRestartThreshold:     100,
									IncludingInitContainers: true,
								},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodshavingtoomanyrestarts.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatinginterpodantiaffinity.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatinginterpodantiaffinity.PluginName,
								Args: &removepodsviolatinginterpodantiaffinity.RemovePodsViolatingInterPodAntiAffinityArgs{},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodsviolatinginterpodantiaffinity.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatingnodeaffinity.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatingnodeaffinity.PluginName,
								Args: &removepodsviolatingnodeaffinity.RemovePodsViolatingNodeAffinityArgs{
									NodeAffinityType: []string{"requiredDuringSchedulingIgnoredDuringExecution"},
								},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodsviolatingnodeaffinity.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatingnodetaints.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatingnodetaints.PluginName,
								Args: &removepodsviolatingnodetaints.RemovePodsViolatingNodeTaintsArgs{
									ExcludedTaints: []string{"dedicated=special-user", "reserved"},
								},
							},
						},
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{
								Enabled: []string{removepodsviolatingnodetaints.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
					{
						Name: fmt.Sprintf("strategy-%s-profile", removepodsviolatingtopologyspreadconstraint.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: removepodsviolatingtopologyspreadconstraint.PluginName,
								Args: &removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs{
									Constraints:            []v1.UnsatisfiableConstraintAction{v1.DoNotSchedule, v1.ScheduleAnyway},
									TopologyBalanceNodeFit: utilpointer.Bool(true),
								},
							},
						},
						Plugins: api.Plugins{
							Balance: api.PluginSet{
								Enabled: []string{removepodsviolatingtopologyspreadconstraint.PluginName},
							},
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
						},
					},
				},
			},
		},
		{
			description: "invalid strategy name",
			policy: &v1alpha1.DeschedulerPolicy{Strategies: v1alpha1.StrategyList{
				"InvalidName": v1alpha1.DeschedulerStrategy{
					Enabled: true,
					Params:  &v1alpha1.StrategyParameters{},
				},
			}},
			result: nil,
			err:    fmt.Errorf("unknown strategy name: InvalidName"),
		},
		{
			description: "invalid threshold priority",
			policy: &v1alpha1.DeschedulerPolicy{Strategies: v1alpha1.StrategyList{
				nodeutilization.LowNodeUtilizationPluginName: v1alpha1.DeschedulerStrategy{
					Enabled: true,
					Params: &v1alpha1.StrategyParameters{
						ThresholdPriority:          utilpointer.Int32(100),
						ThresholdPriorityClassName: "name",
						NodeResourceUtilizationThresholds: &v1alpha1.NodeResourceUtilizationThresholds{
							Thresholds: v1alpha1.ResourceThresholds{
								"cpu":    v1alpha1.Percentage(20),
								"memory": v1alpha1.Percentage(20),
								"pods":   v1alpha1.Percentage(20),
							},
							TargetThresholds: v1alpha1.ResourceThresholds{
								"cpu":    v1alpha1.Percentage(50),
								"memory": v1alpha1.Percentage(50),
								"pods":   v1alpha1.Percentage(50),
							},
						},
					},
				},
			}},
			result: nil,
			err:    fmt.Errorf("priority threshold misconfigured for plugin LowNodeUtilization"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := &api.DeschedulerPolicy{}
			scope := scope{}
			err := v1alpha1.V1alpha1ToInternal(tc.policy, pluginregistry.PluginRegistry, result, scope)
			if err != nil {
				if err.Error() != tc.err.Error() {
					t.Errorf("unexpected error: %s", err.Error())
				}
			}
			if err == nil {
				// sort to easily compare deepequality
				result.Profiles = api.SortDeschedulerProfileByName(result.Profiles)
				diff := cmp.Diff(tc.result, result)
				if diff != "" {
					t.Errorf("test '%s' failed. Results are not deep equal. mismatch (-want +got):\n%s", tc.description, diff)
				}
			}
		})
	}
}

func TestDecodeVersionedPolicy(t *testing.T) {
	client := fakeclientset.NewSimpleClientset()
	SetupPlugins()
	defaultEvictorPluginConfig := api.PluginConfig{
		Name: defaultevictor.PluginName,
		Args: &defaultevictor.DefaultEvictorArgs{
			PriorityThreshold: &api.PriorityThreshold{
				Value: utilpointer.Int32(utils.SystemCriticalPriority),
			},
		},
	}
	type testCase struct {
		description string
		policy      []byte
		err         error
		result      *api.DeschedulerPolicy
	}
	testCases := []testCase{
		{
			description: "v1alpha1 to internal",
			policy: []byte(`apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "PodLifeTime":
    enabled: true
    params:
      podLifeTime:
        maxPodLifeTimeSeconds: 5
      namespaces:
        include:
          - "testleaderelection-a"
`),
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: fmt.Sprintf("strategy-%s-profile", podlifetime.PluginName),
						PluginConfigs: []api.PluginConfig{
							defaultEvictorPluginConfig,
							{
								Name: podlifetime.PluginName,
								Args: &podlifetime.PodLifeTimeArgs{
									Namespaces: &api.Namespaces{
										Include: []string{"testleaderelection-a"},
									},
									MaxPodLifeTimeSeconds: utilpointer.Uint(5),
								},
							},
						},
						Plugins: api.Plugins{
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							Deschedule: api.PluginSet{
								Enabled: []string{podlifetime.PluginName},
							},
						},
					},
				},
			},
		},
		{
			description: "v1alpha1 to internal with priorityThreshold",
			policy: []byte(`apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "PodLifeTime":
    enabled: true
    params:
      podLifeTime:
        maxPodLifeTimeSeconds: 5
      namespaces:
        include:
          - "testleaderelection-a"
      thresholdPriority: null
      thresholdPriorityClassName: prioritym
`),
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: fmt.Sprintf("strategy-%s-profile", podlifetime.PluginName),
						PluginConfigs: []api.PluginConfig{
							{
								Name: "DefaultEvictor",
								Args: &defaultevictor.DefaultEvictorArgs{
									PriorityThreshold: &api.PriorityThreshold{
										Value: utilpointer.Int32(0),
									},
								},
							},
							{
								Name: podlifetime.PluginName,
								Args: &podlifetime.PodLifeTimeArgs{
									Namespaces: &api.Namespaces{
										Include: []string{"testleaderelection-a"},
									},
									MaxPodLifeTimeSeconds: utilpointer.Uint(5),
								},
							},
						},
						Plugins: api.Plugins{
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							Deschedule: api.PluginSet{
								Enabled: []string{podlifetime.PluginName},
							},
						},
					},
				},
			},
		},
		{
			description: "v1alpha1 to internal with priorityThreshold value and name should return error",
			policy: []byte(`apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "PodLifeTime":
    enabled: true
    params:
      podLifeTime:
        maxPodLifeTimeSeconds: 5
      namespaces:
        include:
          - "testleaderelection-a"
      thresholdPriority: 222
      thresholdPriorityClassName: prioritym
`),
			result: nil,
			err:    fmt.Errorf("failed decoding descheduler's policy config \"filename\": priority threshold misconfigured for plugin PodLifeTime"),
		},
		{
			description: "v1alpha2 to internal",
			policy: []byte(`apiVersion: "descheduler/v1alpha2"
kind: "DeschedulerPolicy"
profiles:
  - name: ProfileName
    pluginConfig:
    - name: "DefaultEvictor"
      args:
        evictSystemCriticalPods: true
        evictFailedBarePods: true
        evictLocalStoragePods: true
        evictDaemonSetPods: true
        nodeFit: true
    - name: "RemovePodsHavingTooManyRestarts"
      args:
        podRestartThreshold: 100
        includingInitContainers: true
    plugins:
      deschedule:
        enabled:
          - "RemovePodsHavingTooManyRestarts"
`),
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: "ProfileName",
						PluginConfigs: []api.PluginConfig{
							{
								Name: defaultevictor.PluginName,
								Args: &defaultevictor.DefaultEvictorArgs{
									EvictSystemCriticalPods: true,
									EvictFailedBarePods:     true,
									EvictLocalStoragePods:   true,
									EvictDaemonSetPods:      true,
									PriorityThreshold:       &api.PriorityThreshold{Value: utilpointer.Int32(2000000000)},
									NodeFit:                 true,
								},
							},
							{
								Name: removepodshavingtoomanyrestarts.PluginName,
								Args: &removepodshavingtoomanyrestarts.RemovePodsHavingTooManyRestartsArgs{
									PodRestartThreshold:     100,
									IncludingInitContainers: true,
								},
							},
						},
						Plugins: api.Plugins{
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							Deschedule: api.PluginSet{
								Enabled: []string{removepodshavingtoomanyrestarts.PluginName},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result, err := decode("filename", tc.policy, client, pluginregistry.PluginRegistry)
			if err != nil {
				if tc.err == nil {
					t.Errorf("unexpected error: %s.", err.Error())
				} else if err.Error() != tc.err.Error() {
					t.Errorf("unexpected error: %s. Was expecting %s", err.Error(), tc.err.Error())
				}
			}
			diff := cmp.Diff(tc.result, result)
			if diff != "" && err == nil {
				t.Errorf("test '%s' failed. Results are not deep equal. mismatch (-want +got):\n%s", tc.description, diff)
			}
		})
	}
}

func TestValidateDeschedulerConfiguration(t *testing.T) {
	SetupPlugins()
	type testCase struct {
		description       string
		deschedulerPolicy api.DeschedulerPolicy
		result            error
	}
	testCases := []testCase{
		{
			description: "multiple errors",
			deschedulerPolicy: api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: removefailedpods.PluginName,
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{Enabled: []string{removefailedpods.PluginName}},
						},
						PluginConfigs: []api.PluginConfig{
							{
								Name: removefailedpods.PluginName,
								Args: &removefailedpods.RemoveFailedPodsArgs{
									Namespaces: &api.Namespaces{
										Include: []string{"test1"},
										Exclude: []string{"test1"},
									},
								},
							},
						},
					},
					{
						Name: removepodsviolatingtopologyspreadconstraint.PluginName,
						Plugins: api.Plugins{
							Deschedule: api.PluginSet{Enabled: []string{removepodsviolatingtopologyspreadconstraint.PluginName}},
						},
						PluginConfigs: []api.PluginConfig{
							{
								Name: removepodsviolatingtopologyspreadconstraint.PluginName,
								Args: &removepodsviolatingtopologyspreadconstraint.RemovePodsViolatingTopologySpreadConstraintArgs{
									Namespaces: &api.Namespaces{
										Include: []string{"test1"},
										Exclude: []string{"test1"},
									},
								},
							},
						},
					},
				},
			},
			result: fmt.Errorf("[in profile RemoveFailedPods: only one of Include/Exclude namespaces can be set, in profile RemovePodsViolatingTopologySpreadConstraint: only one of Include/Exclude namespaces can be set]"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := validateDeschedulerConfiguration(tc.deschedulerPolicy, pluginregistry.PluginRegistry)
			if result.Error() != tc.result.Error() {
				t.Errorf("test '%s' failed. expected \n'%s', got \n'%s'", tc.description, tc.result, result)
			}
		})
	}
}

func TestDecodeDefaults(t *testing.T) {
	client := fakeclientset.NewSimpleClientset()
	SetupPlugins()
	type testCase struct {
		description string
		policy      []byte
		err         error
		result      *api.DeschedulerPolicy
	}
	testCases := []testCase{
		{
			description: "use empty RemoveFailedPods, check MinPodLifetimeSeconds default",
			policy: []byte(`apiVersion: "descheduler/v1alpha2"
kind: "DeschedulerPolicy"
profiles:
  - name: ProfileName
    pluginConfig:
    - name: "DefaultEvictor"
      args:
        evictSystemCriticalPods: true
        evictFailedBarePods: true
        evictLocalStoragePods: true
        evictDaemonSetPods: true
        nodeFit: true
    - name: "RemoveFailedPods"
    plugins:
      filter:
        enabled:
          - "DefaultEvictor"
      preEvictionFilter:
        enabled:
          - "DefaultEvictor"
      deschedule:
        enabled:
          - "RemovePodsHavingTooManyRestarts"
`),
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: "ProfileName",
						PluginConfigs: []api.PluginConfig{
							{
								Name: defaultevictor.PluginName,
								Args: &defaultevictor.DefaultEvictorArgs{
									EvictSystemCriticalPods: true,
									EvictFailedBarePods:     true,
									EvictLocalStoragePods:   true,
									EvictDaemonSetPods:      true,
									PriorityThreshold:       &api.PriorityThreshold{Value: utilpointer.Int32(2000000000)},
									NodeFit:                 true,
								},
							},
							{
								Name: removefailedpods.PluginName,
								Args: &removefailedpods.RemoveFailedPodsArgs{
									MinPodLifetimeSeconds: utilpointer.Uint(3600),
								},
							},
						},
						Plugins: api.Plugins{
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							Deschedule: api.PluginSet{
								Enabled: []string{removepodshavingtoomanyrestarts.PluginName},
							},
						},
					},
				},
			},
		},
		{
			description: "omit default evictor extension point with their enablement",
			policy: []byte(`apiVersion: "descheduler/v1alpha2"
kind: "DeschedulerPolicy"
profiles:
  - name: ProfileName
    pluginConfig:
    - name: "DefaultEvictor"
      args:
        evictSystemCriticalPods: true
        evictFailedBarePods: true
        evictLocalStoragePods: true
        evictDaemonSetPods: true
        nodeFit: true
    - name: "RemoveFailedPods"
    plugins:
      deschedule:
        enabled:
          - "RemovePodsHavingTooManyRestarts"
`),
			result: &api.DeschedulerPolicy{
				Profiles: []api.DeschedulerProfile{
					{
						Name: "ProfileName",
						PluginConfigs: []api.PluginConfig{
							{
								Name: defaultevictor.PluginName,
								Args: &defaultevictor.DefaultEvictorArgs{
									EvictSystemCriticalPods: true,
									EvictFailedBarePods:     true,
									EvictLocalStoragePods:   true,
									EvictDaemonSetPods:      true,
									PriorityThreshold:       &api.PriorityThreshold{Value: utilpointer.Int32(2000000000)},
									NodeFit:                 true,
								},
							},
							{
								Name: removefailedpods.PluginName,
								Args: &removefailedpods.RemoveFailedPodsArgs{
									MinPodLifetimeSeconds: utilpointer.Uint(3600),
								},
							},
						},
						Plugins: api.Plugins{
							Filter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							PreEvictionFilter: api.PluginSet{
								Enabled: []string{defaultevictor.PluginName},
							},
							Deschedule: api.PluginSet{
								Enabled: []string{removepodshavingtoomanyrestarts.PluginName},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result, err := decode("filename", tc.policy, client, pluginregistry.PluginRegistry)
			if err != nil {
				if tc.err == nil {
					t.Errorf("unexpected error: %s.", err.Error())
				} else {
					t.Errorf("unexpected error: %s. Was expecting %s", err.Error(), tc.err.Error())
				}
			}
			diff := cmp.Diff(tc.result, result)
			if diff != "" && err == nil {
				t.Errorf("test '%s' failed. Results are not deep equal. mismatch (-want +got):\n%s", tc.description, diff)
			}
		})
	}
}
