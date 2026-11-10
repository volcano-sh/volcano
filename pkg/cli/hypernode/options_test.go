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

package hypernode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
)

func testOptions(t *testing.T, server *httptest.Server) TreeOptions {
	t.Helper()
	config := clientcmdapi.Config{
		Clusters:       map[string]*clientcmdapi.Cluster{"test": {Server: server.URL}},
		Contexts:       map[string]*clientcmdapi.Context{"test": {Cluster: "test"}},
		CurrentContext: "test",
	}
	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, clientcmd.WriteToFile(config, path))
	return TreeOptions{CommonFlags: util.CommonFlags{Kubeconfig: path}, Charset: "unicode", DRA: true}
}

func TestRunReadsSnapshotAndPrintsTree(t *testing.T) {
	hn := testHyperNode("root", 1, exactMember(topologyv1alpha1.MemberTypeNode, "node"))
	node := testNode("node", corev1.ConditionTrue, false, map[corev1.ResourceName][2]string{
		"vendor.example.com/device": {"2", "4"},
	})
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/topology.volcano.sh/v1alpha1/hypernodes":
			_ = json.NewEncoder(w).Encode(topologyv1alpha1.HyperNodeList{
				TypeMeta: metav1.TypeMeta{Kind: "HyperNodeList", APIVersion: "topology.volcano.sh/v1alpha1"},
				Items:    []topologyv1alpha1.HyperNode{hn},
			})
		case "/api/v1/nodes":
			_ = json.NewEncoder(w).Encode(corev1.NodeList{
				TypeMeta: metav1.TypeMeta{Kind: "NodeList", APIVersion: "v1"},
				Items:    []corev1.Node{node},
			})
		case "/apis/resource.k8s.io/v1/resourceslices":
			slice := testResourceSlice("slice", "gpu.example.com", "pool", "node", "gpu-0", "gpu-1")
			_ = json.NewEncoder(w).Encode(resourcev1.ResourceSliceList{
				TypeMeta: metav1.TypeMeta{Kind: "ResourceSliceList", APIVersion: "resource.k8s.io/v1"},
				Items:    []resourcev1.ResourceSlice{slice},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	options := testOptions(t, server)
	var output bytes.Buffer
	require.NoError(t, options.Run(context.Background(), &output))
	assert.Equal(t, [][]string{
		{"NAME", "TIER", "STATUS", "NODES", "READY", "CORDONED", "vendor.example.com/device", "DRA:gpu.example.com"},
		{"root", "1", "-", "1", "1/1", "0", "2/4", "2"},
		{"node", "-", "Ready", "-", "-", "-", "2/4", "2"},
	}, outputRows(output.String()))
	assert.Contains(t, output.String(), "DRA VALUES: ADVERTISED DEVICE COUNT (not allocatable or free resources)")
	assert.Contains(t, output.String(), "└── node")
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{
		"GET /apis/topology.volcano.sh/v1alpha1/hypernodes", "GET /api/v1/nodes",
		"GET /apis/resource.k8s.io/v1/resourceslices",
	}, requests)
}

func testResourceSlice(name, driver, pool, node string, devices ...string) resourcev1.ResourceSlice {
	slice := resourcev1.ResourceSlice{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: resourcev1.ResourceSliceSpec{
			Driver:   driver,
			NodeName: &node,
			Pool:     resourcev1.ResourcePool{Name: pool, Generation: 1, ResourceSliceCount: 1},
		},
	}
	for _, name := range devices {
		slice.Spec.Devices = append(slice.Spec.Devices, resourcev1.Device{Name: name})
	}
	return slice
}

func runDRASnapshot(t *testing.T, hyperNodes []topologyv1alpha1.HyperNode, nodes []corev1.Node, slices []resourcev1.ResourceSlice) (string, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/topology.volcano.sh/v1alpha1/hypernodes":
			_ = json.NewEncoder(w).Encode(topologyv1alpha1.HyperNodeList{
				TypeMeta: metav1.TypeMeta{Kind: "HyperNodeList", APIVersion: "topology.volcano.sh/v1alpha1"},
				Items:    hyperNodes,
			})
		case "/api/v1/nodes":
			_ = json.NewEncoder(w).Encode(corev1.NodeList{
				TypeMeta: metav1.TypeMeta{Kind: "NodeList", APIVersion: "v1"},
				Items:    nodes,
			})
		case "/apis/resource.k8s.io/v1/resourceslices":
			_ = json.NewEncoder(w).Encode(resourcev1.ResourceSliceList{
				TypeMeta: metav1.TypeMeta{Kind: "ResourceSliceList", APIVersion: "resource.k8s.io/v1"},
				Items:    slices,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	options := testOptions(t, server)
	var output bytes.Buffer
	err := options.Run(context.Background(), &output)
	return output.String(), err
}

func TestRunDRAInventoryUsesCompleteLatestPoolsAndDistinctDevices(t *testing.T) {
	hyperNodes := []topologyv1alpha1.HyperNode{
		testHyperNode("root", 2,
			exactMember(topologyv1alpha1.MemberTypeHyperNode, "rack-a"),
			exactMember(topologyv1alpha1.MemberTypeHyperNode, "rack-b"),
			exactMember(topologyv1alpha1.MemberTypeNode, "node-a")),
		testHyperNode("rack-a", 1, exactMember(topologyv1alpha1.MemberTypeNode, "node-a")),
		testHyperNode("rack-b", 1,
			exactMember(topologyv1alpha1.MemberTypeNode, "node-a"),
			exactMember(topologyv1alpha1.MemberTypeNode, "node-b"),
			exactMember(topologyv1alpha1.MemberTypeNode, "node-c")),
	}
	nodes := []corev1.Node{
		testNode("node-a", corev1.ConditionTrue, false, nil),
		testNode("node-b", corev1.ConditionFalse, true, map[corev1.ResourceName][2]string{"vendor.example.com/device": {"2", "4"}}),
		testNode("node-c", corev1.ConditionTrue, false, nil),
		testNode("outside", corev1.ConditionTrue, false, nil),
	}
	old := testResourceSlice("old", "a.example.com", "pool-a", "node-a", "old-0", "old-1", "old-2")
	first := testResourceSlice("first", "a.example.com", "pool-a", "node-a", "gpu-0")
	second := testResourceSlice("second", "a.example.com", "pool-a", "node-a", "gpu-1")
	for _, slice := range []*resourcev1.ResourceSlice{&first, &second} {
		slice.Spec.Pool.Generation = 2
		slice.Spec.Pool.ResourceSliceCount = 2
	}
	first.Spec.Devices[0].Taints = []resourcev1.DeviceTaint{{Key: "maintenance", Effect: resourcev1.DeviceTaintEffectNoSchedule}}
	first.Spec.Devices[0].Capacity = map[resourcev1.QualifiedName]resourcev1.DeviceCapacity{
		"memory": {Value: resource.MustParse("80Gi")},
	}
	shared := true
	second.Spec.Devices[0].AllowMultipleAllocations = &shared
	outside := testResourceSlice("outside", "z.example.com", "outside-pool", "outside", "gpu-0")
	outside.Spec.Pool.ResourceSliceCount = 2
	resourceSlices := []resourcev1.ResourceSlice{
		old, first, second, outside,
		testResourceSlice("node-b", "a.example.com", "pool-b", "node-b", "gpu-0"),
		testResourceSlice("node-c", "a.example.com", "pool-c", "node-c"),
		testResourceSlice("other-driver", "b.example.com", "pool-a", "node-a", "gpu-0"),
	}
	want := [][]string{
		{"NAME", "TIER", "STATUS", "NODES", "READY", "CORDONED", "vendor.example.com/device", "DRA:a.example.com", "DRA:b.example.com"},
		{"root", "2", "-", "3", "2/3", "1", "2/4", "3", "1"},
		{"rack-a", "1", "-", "1", "1/1", "0", "-/-", "2", "1"},
		{"node-a", "-", "Ready", "-", "-", "-", "-/-", "2", "1"},
		{"rack-b", "1", "-", "3", "2/3", "1", "2/4", "3", "1"},
		{"node-a", "-", "Ready", "-", "-", "-", "-/-", "2", "1"},
		{"node-b", "-", "NotReady,SchedulingDisabled", "-", "-", "-", "2/4", "1", "-"},
		{"node-c", "-", "Ready", "-", "-", "-", "-/-", "0", "-"},
		{"node-a", "-", "Ready", "-", "-", "-", "-/-", "2", "1"},
	}
	original, err := runDRASnapshot(t, hyperNodes, nodes, resourceSlices)
	require.NoError(t, err)
	assert.Equal(t, want, outputRows(original))
	slices.Reverse(hyperNodes)
	slices.Reverse(nodes)
	slices.Reverse(resourceSlices)
	reversed, err := runDRASnapshot(t, hyperNodes, nodes, resourceSlices)
	require.NoError(t, err)
	assert.Equal(t, original, reversed)
}

func TestRunRejectsInvalidDRAInventoryWithoutPartialOutput(t *testing.T) {
	for _, tt := range []struct {
		name   string
		slices func() []resourcev1.ResourceSlice
		want   string
	}{
		{"incomplete-latest-generation", func() []resourcev1.ResourceSlice {
			old := testResourceSlice("old", "gpu.example.com", "pool", "node", "gpu-0")
			latest := testResourceSlice("latest", "gpu.example.com", "pool", "node", "gpu-0")
			latest.Spec.Pool.Generation = 2
			latest.Spec.Pool.ResourceSliceCount = 2
			return []resourcev1.ResourceSlice{old, latest}
		}, "incomplete"},
		{"inconsistent-slice-count", func() []resourcev1.ResourceSlice {
			first := testResourceSlice("first", "gpu.example.com", "pool", "node", "gpu-0")
			second := testResourceSlice("second", "gpu.example.com", "pool", "node", "gpu-1")
			first.Spec.Pool.ResourceSliceCount = 2
			second.Spec.Pool.ResourceSliceCount = 3
			return []resourcev1.ResourceSlice{first, second}
		}, "incomplete"},
		{"duplicate-device", func() []resourcev1.ResourceSlice {
			first := testResourceSlice("first", "gpu.example.com", "pool", "node", "gpu-0")
			second := testResourceSlice("second", "gpu.example.com", "pool", "node", "gpu-0")
			first.Spec.Pool.ResourceSliceCount = 2
			second.Spec.Pool.ResourceSliceCount = 2
			return []resourcev1.ResourceSlice{first, second}
		}, "duplicate device"},
		{"node-selector", func() []resourcev1.ResourceSlice {
			slice := testResourceSlice("selector", "gpu.example.com", "pool", "node", "gpu-0")
			slice.Spec.NodeName = nil
			slice.Spec.NodeSelector = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "zone", Operator: corev1.NodeSelectorOpExists}}},
			}}
			return []resourcev1.ResourceSlice{slice}
		}, "node-local"},
		{"all-nodes", func() []resourcev1.ResourceSlice {
			slice := testResourceSlice("shared", "gpu.example.com", "pool", "node", "gpu-0")
			all := true
			slice.Spec.NodeName = nil
			slice.Spec.AllNodes = &all
			return []resourcev1.ResourceSlice{slice}
		}, "node-local"},
		{"per-device-node-selection", func() []resourcev1.ResourceSlice {
			slice := testResourceSlice("per-device", "gpu.example.com", "pool", "node", "gpu-0")
			perDevice := true
			slice.Spec.Devices[0].NodeName = slice.Spec.NodeName
			slice.Spec.NodeName = nil
			slice.Spec.PerDeviceNodeSelection = &perDevice
			return []resourcev1.ResourceSlice{slice}
		}, "node-local"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hn := testHyperNode("root", 1, exactMember(topologyv1alpha1.MemberTypeNode, "node"))
			output, err := runDRASnapshot(t, []topologyv1alpha1.HyperNode{hn},
				[]corev1.Node{testNode("node", corev1.ConditionTrue, false, nil)}, tt.slices())
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, output)
		})
	}
}

func TestRunAPIErrorDoesNotPrintPartialTree(t *testing.T) {
	for _, failPath := range []string{
		"/apis/topology.volcano.sh/v1alpha1/hypernodes", "/api/v1/nodes", "/apis/resource.k8s.io/v1/resourceslices",
	} {
		t.Run(failPath, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == failPath {
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(metav1.Status{
						TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
						Status:   metav1.StatusFailure, Reason: metav1.StatusReasonForbidden,
						Code: http.StatusForbidden, Message: "list access denied",
					})
					return
				}
				if r.URL.Path == "/api/v1/nodes" {
					fmt.Fprint(w, `{"apiVersion":"v1","kind":"NodeList","items":[]}`)
				} else {
					fmt.Fprint(w, `{"apiVersion":"topology.volcano.sh/v1alpha1","kind":"HyperNodeList","items":[]}`)
				}
			}))
			defer server.Close()
			options := testOptions(t, server)
			var output bytes.Buffer
			require.ErrorContains(t, options.Run(context.Background(), &output), "list access denied")
			assert.Empty(t, output.String())
		})
	}
}

func TestRunCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	options := testOptions(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-started:
			cancel()
		case <-ctx.Done():
		}
	}()
	var output bytes.Buffer
	require.ErrorContains(t, options.Run(ctx, &output), "context canceled")
	assert.Empty(t, output.String())
}

func TestRunRejectsCharsetBeforeLoadingConfig(t *testing.T) {
	options := TreeOptions{Charset: "invalid", CommonFlags: util.CommonFlags{Kubeconfig: "/does-not-exist"}}
	var output bytes.Buffer
	require.ErrorContains(t, options.Run(context.Background(), &output), "charset")
	assert.Empty(t, output.String())
}
