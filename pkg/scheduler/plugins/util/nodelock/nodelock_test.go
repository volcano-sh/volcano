package nodelock

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLockNode(t *testing.T) {
	var (
		nodeName = "test-node"
		lockName = "test-node-lock"
	)

	tests := []struct {
		name      string
		expectErr error
		node      *v1.Node
	}{
		{
			name:      "lock node success",
			expectErr: nil,
			node: buildNode(nodeName,
				map[string]string{lockName: time.Now().Add(-time.Minute * 20).Format(time.RFC3339)}),
		},
		{
			name:      "lock node failed",
			expectErr: fmt.Errorf("node %s has been locked within 5 minutes", nodeName),
			node: buildNode(nodeName,
				map[string]string{lockName: time.Now().Add(time.Minute * 20).Format(time.RFC3339)}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tc.node)
			_ = UseClient(fakeClient)
			gotErr := LockNode(nodeName, lockName)
			if !reflect.DeepEqual(tc.expectErr, gotErr) {
				t.Errorf("LockNode error: (+got: %T/-want: %T)", gotErr, tc.expectErr)
			}
		})
	}
}

// buildNode builts node
func buildNode(name string, annotations map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
	}
}
