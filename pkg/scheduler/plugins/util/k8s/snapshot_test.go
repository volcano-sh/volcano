package k8s

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

func TestSnapshot(t *testing.T) {
	var (
		nodeName = "test-node"
		pod1     = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "node_info_cache_test",
				Name:      "test-1",
				UID:       types.UID("test-1"),
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("100m"),
								v1.ResourceMemory: resource.MustParse("500"),
							},
						},
						Ports: []v1.ContainerPort{
							{
								HostIP:   "127.0.0.1",
								HostPort: 80,
								Protocol: "TCP",
							},
						},
					},
				},
				NodeName: nodeName,
			},
		}
		pod2 = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "node_info_cache_test",
				Name:      "test-2",
				UID:       types.UID("test-2"),
				Labels:    map[string]string{"test": "test"},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("200m"),
								v1.ResourceMemory: resource.MustParse("1Ki"),
							},
						},
						Ports: []v1.ContainerPort{
							{
								HostIP:   "127.0.0.1",
								HostPort: 8080,
								Protocol: "TCP",
							},
						},
					},
				},
				NodeName: nodeName,
			},
		}
	)

	tests := []struct {
		name              string
		nodeInfoMap       map[string]*framework.NodeInfo
		expectedNodeInfos []*framework.NodeInfo
		expectedPods      []*v1.Pod
		expectErr         error
	}{
		{
			name: "test snapshot operation",
			nodeInfoMap: map[string]*framework.NodeInfo{nodeName: {
				Requested:        &framework.Resource{},
				NonZeroRequested: &framework.Resource{},
				Allocatable:      &framework.Resource{},
				Generation:       2,
				UsedPorts: framework.HostPortInfo{
					"127.0.0.1": map[framework.ProtocolPort]struct{}{
						{Protocol: "TCP", Port: 80}:   {},
						{Protocol: "TCP", Port: 8080}: {},
					},
				},
				ImageStates:  map[string]*framework.ImageStateSummary{},
				PVCRefCounts: map[string]int{},
				Pods: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithRequiredAntiAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
			}},
			expectedNodeInfos: []*framework.NodeInfo{{
				Requested:        &framework.Resource{},
				NonZeroRequested: &framework.Resource{},
				Allocatable:      &framework.Resource{},
				Generation:       2,
				UsedPorts: framework.HostPortInfo{
					"127.0.0.1": map[framework.ProtocolPort]struct{}{
						{Protocol: "TCP", Port: 80}:   {},
						{Protocol: "TCP", Port: 8080}: {},
					},
				},
				ImageStates:  map[string]*framework.ImageStateSummary{},
				PVCRefCounts: map[string]int{},
				Pods: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
				PodsWithRequiredAntiAffinity: []*framework.PodInfo{
					{
						Pod: pod1,
					},
					{
						Pod: pod2,
					},
				},
			}},
			expectedPods: []*v1.Pod{pod2},
			expectErr:    fmt.Errorf("nodeinfo not found for node name %q", nodeName),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := NewSnapshot(tc.nodeInfoMap)
			nodeInfoList, err := snapshot.List()
			if !reflect.DeepEqual(nodeInfoList, tc.expectedNodeInfos) || err != nil {
				t.Errorf("unexpected list nodeInfos value (+got: %s/-want: %s), err: %s", tc.expectedNodeInfos, nodeInfoList, err)
			}

			_, err = snapshot.Get(nodeName)
			if !reflect.DeepEqual(tc.expectErr, err) {
				t.Errorf("unexpected get nodeInfos by nodeName value (+got: %T/-want: %T)", err, tc.expectErr)
			}

			nodeInfoList, err = snapshot.HavePodsWithAffinityList()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfoList) || err != nil {
				t.Errorf("unexpected list HavePodsWithAffinity nodeInfos value (+got: %s/-want: %s), err: %s", nodeInfoList, tc.expectedNodeInfos, err)
			}

			nodeInfoList, err = snapshot.HavePodsWithRequiredAntiAffinityList()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfoList) || err != nil {
				t.Errorf("unexpected list PodsWithRequiredAntiAffinity nodeInfos value (+got: %s/-want: %s), err: %s", nodeInfoList, tc.expectedNodeInfos, err)
			}

			sel, err := labels.Parse("test==test")
			pods, err := snapshot.Pods().List(sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Errorf("unexpected list pods value (+got: %s/-want: %s), err: %s", pods, tc.expectedNodeInfos, err)
			}

			pods, err = snapshot.Pods().FilteredList(func(pod *v1.Pod) bool {
				return true
			}, sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Errorf("unexpected list filtered pods value (+got: %s/-want: %s), err: %s", pods, tc.expectedPods, err)
			}

			nodeInfos, err := snapshot.NodeInfos().List()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfos) || err != nil {
				t.Errorf("unexpected list nodeInfos value (+got: %s/-want: %s), err: %s", nodeInfos, tc.expectedNodeInfos, err)
			}

			getBool := snapshot.StorageInfos().IsPVCUsedByPods("test")
			if !reflect.DeepEqual(false, getBool) {
				t.Errorf("unexpected get StorageInfos PVCUsed value (+got: %v/-want: %v)", false, getBool)
			}
		})
	}
}
