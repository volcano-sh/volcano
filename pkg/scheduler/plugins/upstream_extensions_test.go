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

package plugins

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/imagelocality"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeunschedulable"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodevolumelimits"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumezone"
)

type extensionPoint string

const (
	enqueueExtensions   extensionPoint = "EnqueueExtensions"
	preEnqueue          extensionPoint = "PreEnqueue"
	preFilter           extensionPoint = "PreFilter"
	preFilterExtensions extensionPoint = "PreFilterExtensions"
	filter              extensionPoint = "Filter"
	postFilter          extensionPoint = "PostFilter"
	preScore            extensionPoint = "PreScore"
	score               extensionPoint = "Score"
	normalizeScore      extensionPoint = "NormalizeScore"
	reserve             extensionPoint = "Reserve"
	permit              extensionPoint = "Permit"
	preBind             extensionPoint = "PreBind"
	bind                extensionPoint = "Bind"
	postBind            extensionPoint = "PostBind"
	sign                extensionPoint = "Sign"
)

type normalizeScorePlugin interface {
	NormalizeScore(context.Context, fwk.CycleState, *v1.Pod, fwk.NodeScoreList) *fwk.Status
}

type upstreamPluginCompatibility struct {
	name                 string
	plugin               any
	adapted              []extensionPoint
	intentionallyIgnored map[extensionPoint]string
}

var upstreamPluginCompatibilities = []upstreamPluginCompatibility{
	{
		name:    dynamicresources.Name,
		plugin:  (*dynamicresources.DynamicResources)(nil),
		adapted: []extensionPoint{preFilter, filter, score, normalizeScore, reserve, preBind},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			preEnqueue:        "Volcano enqueues tasks through its own scheduling actions.",
			postFilter:        "Volcano preemption actions handle post-filter behavior.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    volumebinding.Name,
		plugin:  (*volumebinding.VolumeBinding)(nil),
		adapted: []extensionPoint{preFilter, filter, preScore, score, reserve, preBind},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    nodeaffinity.Name,
		plugin:  (*nodeaffinity.NodeAffinity)(nil),
		adapted: []extensionPoint{preFilter, filter, preScore, score, normalizeScore},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    nodeports.Name,
		plugin:  (*nodeports.NodePorts)(nil),
		adapted: []extensionPoint{preFilter, filter},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    tainttoleration.Name,
		plugin:  (*tainttoleration.TaintToleration)(nil),
		adapted: []extensionPoint{filter, preScore, score, normalizeScore},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    interpodaffinity.Name,
		plugin:  (*interpodaffinity.InterPodAffinity)(nil),
		adapted: []extensionPoint{preFilter, filter, preScore, score, normalizeScore},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions:   "Volcano does not use the kube-scheduler internal scheduling queue.",
			preFilterExtensions: "Volcano does not invoke kube-scheduler incremental pre-filter simulation hooks.",
			sign:                "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    nodevolumelimits.CSIName,
		plugin:  (*nodevolumelimits.CSILimits)(nil),
		adapted: []extensionPoint{preFilter, filter},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    volumezone.Name,
		plugin:  (*volumezone.VolumeZone)(nil),
		adapted: []extensionPoint{preFilter, filter},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    podtopologyspread.Name,
		plugin:  (*podtopologyspread.PodTopologySpread)(nil),
		adapted: []extensionPoint{preFilter, filter, preScore, score, normalizeScore},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions:   "Volcano does not use the kube-scheduler internal scheduling queue.",
			preFilterExtensions: "Volcano does not invoke kube-scheduler incremental pre-filter simulation hooks.",
			sign:                "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    nodeunschedulable.Name,
		plugin:  (*nodeunschedulable.NodeUnschedulable)(nil),
		adapted: []extensionPoint{filter},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    imagelocality.Name,
		plugin:  (*imagelocality.ImageLocality)(nil),
		adapted: []extensionPoint{score},
		intentionallyIgnored: map[extensionPoint]string{
			sign: "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    noderesources.Name,
		plugin:  (*noderesources.Fit)(nil),
		adapted: []extensionPoint{preScore, score},
		intentionallyIgnored: map[extensionPoint]string{
			enqueueExtensions: "Volcano does not use the kube-scheduler internal scheduling queue.",
			preFilter:         "Volcano uses NodeResourcesFit only for node-order scoring.",
			filter:            "Volcano uses NodeResourcesFit only for node-order scoring.",
			sign:              "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
	{
		name:    noderesources.BalancedAllocationName,
		plugin:  (*noderesources.BalancedAllocation)(nil),
		adapted: []extensionPoint{preScore, score},
		intentionallyIgnored: map[extensionPoint]string{
			sign: "Volcano does not use kube-scheduler's Pod signing flow.",
		},
	},
}

func TestUpstreamPluginExtensionCompatibility(t *testing.T) {
	for _, compatibility := range upstreamPluginCompatibilities {
		t.Run(compatibility.name, func(t *testing.T) {
			implemented := implementedExtensionPoints(compatibility.plugin)
			classified := make(map[extensionPoint]struct{}, len(compatibility.adapted)+len(compatibility.intentionallyIgnored))

			for _, extension := range compatibility.adapted {
				if _, exists := implemented[extension]; !exists {
					t.Errorf("adapted extension point %q is no longer implemented", extension)
				}
				classified[extension] = struct{}{}
			}
			for extension, reason := range compatibility.intentionallyIgnored {
				if _, exists := implemented[extension]; !exists {
					t.Errorf("intentionally ignored extension point %q is no longer implemented", extension)
				}
				if reason == "" {
					t.Errorf("intentionally ignored extension point %q must include a reason", extension)
				}
				if _, exists := classified[extension]; exists {
					t.Errorf("extension point %q is both adapted and intentionally ignored", extension)
				}
				classified[extension] = struct{}{}
			}
			for extension := range implemented {
				if _, exists := classified[extension]; !exists {
					t.Errorf("upstream plugin implements unclassified extension point %q; mark it as adapted or intentionally ignored", extension)
				}
			}
		})
	}
}

func implementedExtensionPoints(plugin any) map[extensionPoint]struct{} {
	implemented := make(map[extensionPoint]struct{})
	if _, ok := plugin.(fwk.EnqueueExtensions); ok {
		implemented[enqueueExtensions] = struct{}{}
	}
	if _, ok := plugin.(fwk.PreEnqueuePlugin); ok {
		implemented[preEnqueue] = struct{}{}
	}
	if _, ok := plugin.(fwk.PreFilterPlugin); ok {
		implemented[preFilter] = struct{}{}
	}
	if _, ok := plugin.(fwk.PreFilterExtensions); ok {
		implemented[preFilterExtensions] = struct{}{}
	}
	if _, ok := plugin.(fwk.FilterPlugin); ok {
		implemented[filter] = struct{}{}
	}
	if _, ok := plugin.(fwk.PostFilterPlugin); ok {
		implemented[postFilter] = struct{}{}
	}
	if _, ok := plugin.(fwk.PreScorePlugin); ok {
		implemented[preScore] = struct{}{}
	}
	if _, ok := plugin.(fwk.ScorePlugin); ok {
		implemented[score] = struct{}{}
	}
	if _, ok := plugin.(normalizeScorePlugin); ok {
		implemented[normalizeScore] = struct{}{}
	}
	if _, ok := plugin.(fwk.ReservePlugin); ok {
		implemented[reserve] = struct{}{}
	}
	if _, ok := plugin.(fwk.PermitPlugin); ok {
		implemented[permit] = struct{}{}
	}
	if _, ok := plugin.(fwk.PreBindPlugin); ok {
		implemented[preBind] = struct{}{}
	}
	if _, ok := plugin.(fwk.BindPlugin); ok {
		implemented[bind] = struct{}{}
	}
	if _, ok := plugin.(fwk.PostBindPlugin); ok {
		implemented[postBind] = struct{}{}
	}
	if _, ok := plugin.(fwk.SignPlugin); ok {
		implemented[sign] = struct{}{}
	}
	return implemented
}
