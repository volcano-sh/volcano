package cache

import (
	"context"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/util"
	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"
)

type testPreBinder struct{}

func (testPreBinder) PreBind(context.Context, *agentapi.BindContext) error   { return nil }
func (testPreBinder) PreBindRollBack(context.Context, *agentapi.BindContext) {}

func newTestSchedulerCache(t *testing.T) *SchedulerCache {
	t.Helper()

	if options.ServerOpts == nil {
		options.Default()
	}
	sc := NewDefaultMockSchedulerCache("test-scheduler")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	metricsRecorder := k8smetrics.NewMetricsAsyncRecorder(1000, time.Second, ctx.Done())
	queueingHintMapPerProfile := make(k8sschedulingqueue.QueueingHintMapPerProfile)
	queueingHintMap := make(k8sschedulingqueue.QueueingHintMap)
	queueingHintMap[fwk.ClusterEvent{Resource: fwk.WildCard, ActionType: fwk.All}] = append(
		queueingHintMap[fwk.ClusterEvent{Resource: fwk.WildCard, ActionType: fwk.All}],
		&k8sschedulingqueue.QueueingHintFunction{
			QueueingHintFn: func(_ klog.Logger, _ *v1.Pod, _, _ interface{}) (fwk.QueueingHint, error) {
				return fwk.Queue, nil
			},
		},
	)
	queueingHintMapPerProfile["test-scheduler"] = queueingHintMap
	sc.schedulingQueue = k8sschedulingqueue.NewSchedulingQueue(
		Less,
		sc.SharedInformerFactory(),
		k8sschedulingqueue.WithMetricsRecorder(metricsRecorder),
		k8sschedulingqueue.WithQueueingHintMapPerProfile(queueingHintMapPerProfile),
	)
	sc.ConflictAwareBinder = NewConflictAwareBinder(sc, sc.schedulingQueue)
	sc.schedulingQueue.Run(klog.Background())
	t.Cleanup(func() {
		sc.schedulingQueue.Close()
	})

	return sc
}

func TestUpdateSnapshotTracksBinderNodesAndRemovesDeletedNodes(t *testing.T) {
	sc := newTestSchedulerCache(t)

	node1 := util.BuildNode("node-1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	node2 := util.BuildNode("node-2", api.BuildResourceList("8", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	if err := sc.AddOrUpdateNode(node1); err != nil {
		t.Fatalf("AddOrUpdateNode(node1) failed: %v", err)
	}
	if err := sc.AddOrUpdateNode(node2); err != nil {
		t.Fatalf("AddOrUpdateNode(node2) failed: %v", err)
	}

	sc.RecordCandidateNodesInBinder([]*api.NodeInfo{sc.Nodes["node-1"].info})
	snapshot := k8sutil.NewEmptySnapshot()
	if err := sc.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}

	if len(snapshot.GetVolcanoNodeInfoMap()) != 2 {
		t.Fatalf("expected 2 volcano nodes in snapshot, got %d", len(snapshot.GetVolcanoNodeInfoMap()))
	}
	if snapshot.NodesInBinder["node-1"] != 1 {
		t.Fatalf("expected node-1 to be tracked in binder map, got %+v", snapshot.NodesInBinder)
	}
	firstGeneration := snapshot.GetGeneration()
	if firstGeneration == 0 {
		t.Fatal("expected snapshot generation to be updated")
	}

	updatedNode1 := util.BuildNode("node-1", api.BuildResourceList("12", "24Gi", []api.ScalarResource{{Name: "pods", Value: "12"}}...), map[string]string{})
	if err := sc.AddOrUpdateNode(updatedNode1); err != nil {
		t.Fatalf("AddOrUpdateNode(updatedNode1) failed: %v", err)
	}
	if err := sc.RemoveNode("node-2"); err != nil {
		t.Fatalf("RemoveNode(node-2) failed: %v", err)
	}

	if err := sc.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("second UpdateSnapshot failed: %v", err)
	}

	if len(snapshot.GetVolcanoNodeInfoMap()) != 1 {
		t.Fatalf("expected 1 volcano node after removal, got %d", len(snapshot.GetVolcanoNodeInfoMap()))
	}
	if _, exists := snapshot.GetVolcanoNodeInfoMap()["node-2"]; exists {
		t.Fatal("expected deleted node-2 to be removed from snapshot")
	}
	gotCPU := snapshot.GetVolcanoNodeInfoMap()["node-1"].Node.Status.Capacity.Cpu().String()
	if gotCPU != "12" {
		t.Fatalf("expected updated node-1 cpu capacity 12, got %s", gotCPU)
	}
	if snapshot.GetGeneration() <= firstGeneration {
		t.Fatalf("expected snapshot generation to advance, old=%d new=%d", firstGeneration, snapshot.GetGeneration())
	}
}

func TestRegisterBinderStoresOnlyPreBinders(t *testing.T) {
	sc := NewDefaultMockSchedulerCache("test-scheduler")

	sc.RegisterBinder("predicates", testPreBinder{})
	sc.RegisterBinder("invalid", struct{}{})

	preBinders := sc.binderRegistry.getRegisteredPreBinders()
	if _, found := preBinders["predicates"]; !found {
		t.Fatalf("expected predicates binder to be registered, got %+v", preBinders)
	}
	if _, found := preBinders["invalid"]; found {
		t.Fatalf("did not expect invalid binder to be registered, got %+v", preBinders)
	}
}
