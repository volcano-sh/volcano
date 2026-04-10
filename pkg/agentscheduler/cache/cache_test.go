package cache

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

func TestUpdateSnapshot_SkipNodeWithNilNodeObject(t *testing.T) {
	cache := NewDefaultMockSchedulerCache("test-scheduler")

	nilNodeInfo := schedulingapi.NewNodeInfo(nil)
	nilNodeInfo.Name = "node-nil"
	nilNodeInfo.Generation = 2

	validNodeInfo := schedulingapi.NewNodeInfo(&v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-valid"},
	})
	validNodeInfo.Generation = 1

	nilNodeItem := newNodeInfoListItem(nilNodeInfo)
	validNodeItem := newNodeInfoListItem(validNodeInfo)
	nilNodeItem.next = validNodeItem
	validNodeItem.prev = nilNodeItem

	cache.Nodes[nilNodeInfo.Name] = nilNodeItem
	cache.Nodes[validNodeInfo.Name] = validNodeItem
	cache.headNode = nilNodeItem

	snapshot := k8sutil.NewEmptySnapshot()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("UpdateSnapshot panicked with nil node object: %v", r)
		}
	}()

	if err := cache.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("UpdateSnapshot returned error: %v", err)
	}

	fwkMap := snapshot.GetFwkNodeInfoMap()
	if len(fwkMap) != 1 {
		t.Fatalf("expected 1 valid node in snapshot, got %d", len(fwkMap))
	}
	if _, ok := fwkMap["node-valid"]; !ok {
		t.Fatalf("expected node-valid to exist in snapshot, got keys: %#v", fwkMap)
	}
	if _, ok := fwkMap["node-nil"]; ok {
		t.Fatalf("did not expect node-nil to be included in snapshot")
	}
}

