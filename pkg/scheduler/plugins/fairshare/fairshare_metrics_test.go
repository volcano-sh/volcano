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

package fairshare

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	shareMetric = "volcano_namespace_share"
	usageMetric = "volcano_namespace_decayed_usage"
)

func cpuList(v string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(v)}
}

func resetState(t *testing.T) {
	t.Helper()
	saved := state
	state = &fairShareProcessState{usage: make(map[string]map[string]float64)}
	t.Cleanup(func() { state = saved })
}

func gaugeSeries(t *testing.T, name, namespace, queue string) (float64, bool) {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if labels["namespace_name"] == namespace && labels["queue"] == queue {
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

func runFairshareCycle(tc uthelper.TestCommonStruct, extraArgs ...framework.Arguments) {
	args := framework.Arguments{"fairshare.resourceKey": "cpu"}
	for _, extra := range extraArgs {
		for k, v := range extra {
			args[k] = v
		}
	}
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:      PluginName,
		Arguments: args,
	}}}}
	tc.Plugins = map[string]framework.PluginBuilder{PluginName: New}
	tc.RegisterSession(tiers, nil)
	tc.Close()
}

func TestMetrics_ShareDeletedWhenNamespaceHasNoWork(t *testing.T) {
	resetState(t)
	node := util.BuildNode("n1", cpuList("8"), nil)
	queue := util.BuildQueue("q-share", 1, cpuList("8"))

	runFairshareCycle(uthelper.TestCommonStruct{
		Nodes:  []*v1.Node{node},
		Queues: []*schedulingv1beta1.Queue{queue},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("pg-a", "ns-a", "q-share", 1, nil, schedulingv1beta1.PodGroupRunning),
			util.BuildPodGroup("pg-b", "ns-b", "q-share", 1, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("ns-a", "a-0", "n1", v1.PodRunning, cpuList("2"), "pg-a", nil, nil),
			util.BuildPod("ns-b", "b-0", "", v1.PodPending, cpuList("2"), "pg-b", nil, nil),
		},
	})
	if _, ok := gaugeSeries(t, shareMetric, "ns-a", "q-share"); !ok {
		t.Fatalf("expected ns-a share to be reported while ns-a has running work")
	}

	runFairshareCycle(uthelper.TestCommonStruct{
		Nodes:  []*v1.Node{node},
		Queues: []*schedulingv1beta1.Queue{queue},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("pg-b", "ns-b", "q-share", 1, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("ns-b", "b-0", "", v1.PodPending, cpuList("2"), "pg-b", nil, nil),
		},
	})
	if v, ok := gaugeSeries(t, shareMetric, "ns-a", "q-share"); ok {
		t.Errorf("ns-a share still reported (%v) after ns-a has no running or pending work", v)
	}
	if _, ok := gaugeSeries(t, shareMetric, "ns-b", "q-share"); !ok {
		t.Errorf("ns-b share should still be reported while ns-b has pending work")
	}
}

func TestMetrics_DecayedUsageDeletedWhenUsageRemoved(t *testing.T) {
	resetState(t)
	node := util.BuildNode("n1", cpuList("8"), nil)
	queue := util.BuildQueue("q-usage", 1, cpuList("8"))
	tc := uthelper.TestCommonStruct{
		Nodes:  []*v1.Node{node},
		Queues: []*schedulingv1beta1.Queue{queue},
	}

	ensureGlobalQueueUsage("q-usage")["ns-a"] = 3600
	runFairshareCycle(tc)
	if _, ok := gaugeSeries(t, usageMetric, "ns-a", "q-usage"); !ok {
		t.Fatalf("expected ns-a decayed usage to be reported")
	}

	delete(state.usage["q-usage"], "ns-a")
	runFairshareCycle(tc)
	if v, ok := gaugeSeries(t, usageMetric, "ns-a", "q-usage"); ok {
		t.Errorf("ns-a decayed usage still reported (%v) after it was dropped from state", v)
	}
}

func TestMetrics_DeletedWhenQueueDeleted(t *testing.T) {
	resetState(t)
	node := util.BuildNode("n1", cpuList("8"), nil)
	queue := util.BuildQueue("q-deleted", 1, cpuList("8"))

	ensureGlobalQueueUsage("q-deleted")["ns-a"] = 3600
	runFairshareCycle(uthelper.TestCommonStruct{
		Nodes:     []*v1.Node{node},
		Queues:    []*schedulingv1beta1.Queue{queue},
		PodGroups: []*schedulingv1beta1.PodGroup{util.BuildPodGroup("pg-a", "ns-a", "q-deleted", 1, nil, schedulingv1beta1.PodGroupRunning)},
		Pods:      []*v1.Pod{util.BuildPod("ns-a", "a-0", "n1", v1.PodRunning, cpuList("2"), "pg-a", nil, nil)},
	})
	if _, ok := gaugeSeries(t, shareMetric, "ns-a", "q-deleted"); !ok {
		t.Fatalf("expected ns-a share to be reported")
	}
	if _, ok := gaugeSeries(t, usageMetric, "ns-a", "q-deleted"); !ok {
		t.Fatalf("expected ns-a decayed usage to be reported")
	}

	metrics.DeleteQueueMetrics("q-deleted")
	if _, ok := gaugeSeries(t, shareMetric, "ns-a", "q-deleted"); ok {
		t.Errorf("share still reported after DeleteQueueMetrics")
	}
	if _, ok := gaugeSeries(t, usageMetric, "ns-a", "q-deleted"); ok {
		t.Errorf("decayed usage still reported after DeleteQueueMetrics")
	}

	ensureGlobalQueueUsage("q-deleted")["ns-a"] = 3600
	runFairshareCycle(uthelper.TestCommonStruct{
		Nodes:     []*v1.Node{node},
		Queues:    []*schedulingv1beta1.Queue{queue},
		PodGroups: []*schedulingv1beta1.PodGroup{util.BuildPodGroup("pg-a", "ns-a", "q-deleted", 1, nil, schedulingv1beta1.PodGroupRunning)},
		Pods:      []*v1.Pod{util.BuildPod("ns-a", "a-0", "n1", v1.PodRunning, cpuList("2"), "pg-a", nil, nil)},
	})
	if _, ok := gaugeSeries(t, shareMetric, "ns-a", "q-deleted"); !ok {
		t.Fatalf("expected ns-a share to be set again by the in-flight session")
	}
	delete(state.usage, "q-deleted")
	runFairshareCycle(uthelper.TestCommonStruct{Nodes: []*v1.Node{node}})
	if _, ok := gaugeSeries(t, shareMetric, "ns-a", "q-deleted"); ok {
		t.Errorf("share reported again for a deleted queue")
	}
	if _, ok := gaugeSeries(t, usageMetric, "ns-a", "q-deleted"); ok {
		t.Errorf("decayed usage reported again for a deleted queue")
	}
}

func TestMetrics_DeletedWhenTargetQueueDeleted(t *testing.T) {
	resetState(t)
	node := util.BuildNode("n1", cpuList("8"), nil)
	queue := util.BuildQueue("q-target", 1, cpuList("8"))
	args := framework.Arguments{"fairshare.targetQueues": "q-target"}

	ensureGlobalQueueUsage("q-target")["ns-a"] = 3600
	runFairshareCycle(uthelper.TestCommonStruct{
		Nodes:     []*v1.Node{node},
		Queues:    []*schedulingv1beta1.Queue{queue},
		PodGroups: []*schedulingv1beta1.PodGroup{util.BuildPodGroup("pg-a", "ns-a", "q-target", 1, nil, schedulingv1beta1.PodGroupRunning)},
		Pods:      []*v1.Pod{util.BuildPod("ns-a", "a-0", "n1", v1.PodRunning, cpuList("2"), "pg-a", nil, nil)},
	}, args)
	if _, ok := gaugeSeries(t, shareMetric, "ns-a", "q-target"); !ok {
		t.Fatalf("expected ns-a share to be reported")
	}
	if _, ok := gaugeSeries(t, usageMetric, "ns-a", "q-target"); !ok {
		t.Fatalf("expected ns-a decayed usage to be reported")
	}

	metrics.DeleteQueueMetrics("q-target")
	runFairshareCycle(uthelper.TestCommonStruct{Nodes: []*v1.Node{node}}, args)
	if v, ok := gaugeSeries(t, shareMetric, "ns-a", "q-target"); ok {
		t.Errorf("share reported again (%v) for a deleted target queue", v)
	}
	if v, ok := gaugeSeries(t, usageMetric, "ns-a", "q-target"); ok {
		t.Errorf("decayed usage reported again (%v) for a deleted target queue", v)
	}
}
