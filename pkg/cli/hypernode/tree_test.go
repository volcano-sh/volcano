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
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func exactMember(kind topologyv1alpha1.MemberType, name string) topologyv1alpha1.MemberSpec {
	return topologyv1alpha1.MemberSpec{
		Type: kind,
		Selector: topologyv1alpha1.MemberSelector{
			ExactMatch: &topologyv1alpha1.ExactMatch{Name: name},
		},
	}
}

func testHyperNode(name string, tier int, members ...topologyv1alpha1.MemberSpec) topologyv1alpha1.HyperNode {
	return topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       topologyv1alpha1.HyperNodeSpec{Tier: tier, Members: members},
		Status:     topologyv1alpha1.HyperNodeStatus{NodeCount: 999},
	}
}

func testNode(name string, ready corev1.ConditionStatus, cordoned bool, resources map[corev1.ResourceName][2]string) corev1.Node {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.NodeSpec{Unschedulable: cordoned},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{},
			Capacity:    corev1.ResourceList{},
		},
	}
	if ready != "" {
		node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: ready}}
	}
	for name, values := range resources {
		if values[0] != "" {
			node.Status.Allocatable[name] = resource.MustParse(values[0])
		}
		if values[1] != "" {
			node.Status.Capacity[name] = resource.MustParse(values[1])
		}
	}
	return node
}

func outputRows(output string) [][]string {
	var rows [][]string
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimLeft(line, " │├└─+|-")
		if line == "" || strings.HasPrefix(line, "RESOURCE VALUES:") || strings.HasPrefix(line, "DRA VALUES:") {
			continue
		}
		rows = append(rows, strings.Fields(line))
	}
	return rows
}

func TestPrintTreeResolvesAndAggregatesForest(t *testing.T) {
	nodeMember := topologyv1alpha1.MemberTypeNode
	hnMember := topologyv1alpha1.MemberTypeHyperNode
	hyperNodes := []topologyv1alpha1.HyperNode{
		testHyperNode("zone", 2,
			exactMember(hnMember, "rack-b"), exactMember(hnMember, "rack-a"),
			exactMember(nodeMember, "node-a"), exactMember(nodeMember, "node-a")),
		testHyperNode("rack-b", 1, topologyv1alpha1.MemberSpec{
			Type:     nodeMember,
			Selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: "^node-[bc]$"}},
		}),
		testHyperNode("rack-a", 1, topologyv1alpha1.MemberSpec{
			Type:     nodeMember,
			Selector: topologyv1alpha1.MemberSelector{LabelMatch: &metav1.LabelSelector{MatchLabels: map[string]string{"rack": "a"}}},
		}, exactMember(nodeMember, "node-a")),
		testHyperNode("empty", 0, topologyv1alpha1.MemberSpec{
			Type:     nodeMember,
			Selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: "^absent$"}},
		}),
		testHyperNode("standalone", 7, exactMember(nodeMember, "node-c")),
	}
	hyperNodes[0].Spec.TierName = "zone"
	hyperNodes[1].Spec.TierName = "rack"
	hyperNodes[2].Spec.TierName = "rack"
	nodes := []corev1.Node{
		testNode("node-c", corev1.ConditionFalse, false, map[corev1.ResourceName][2]string{"huawei.com/Ascend910": {"4", "8"}}),
		testNode("node-b", corev1.ConditionTrue, true, map[corev1.ResourceName][2]string{"nvidia.com/gpu": {"0", "8"}}),
		testNode("node-a", corev1.ConditionTrue, false, map[corev1.ResourceName][2]string{
			"nvidia.com/gpu": {"6", "8"}, corev1.ResourceCPU: {"16", "16"},
		}),
		testNode("unselected", corev1.ConditionTrue, false, map[corev1.ResourceName][2]string{"other.com/device": {"1", "1"}}),
	}
	nodes[1].Labels = map[string]string{"rack": "a"}
	nodes[2].Labels = map[string]string{"rack": "a"}
	expected := [][]string{
		{"NAME", "TIER", "STATUS", "NODES", "READY", "CORDONED", "huawei.com/Ascend910", "nvidia.com/gpu"},
		{"empty", "0", "-", "0", "0/0", "0", "-/-", "-/-"},
		{"standalone", "7", "-", "1", "0/1", "0", "4/8", "-/-"},
		{"node-c", "-", "NotReady", "-", "-", "-", "4/8", "-/-"},
		{"zone", "2/zone", "-", "3", "2/3", "1", "4/8", "6/16"},
		{"rack-a", "1/rack", "-", "2", "2/2", "1", "-/-", "6/16"},
		{"node-a", "-", "Ready", "-", "-", "-", "-/-", "6/8"},
		{"node-b", "-", "Ready,SchedulingDisabled", "-", "-", "-", "-/-", "0/8"},
		{"rack-b", "1/rack", "-", "2", "1/2", "1", "4/8", "0/8"},
		{"node-b", "-", "Ready,SchedulingDisabled", "-", "-", "-", "-/-", "0/8"},
		{"node-c", "-", "NotReady", "-", "-", "-", "4/8", "-/-"},
		{"node-a", "-", "Ready", "-", "-", "-", "-/-", "6/8"},
	}
	var original string
	for _, reverse := range []bool{false, true} {
		if reverse {
			slices.Reverse(hyperNodes)
			slices.Reverse(nodes)
		}
		for _, unicode := range []bool{false, true} {
			var output bytes.Buffer
			require.NoError(t, printTree(&output, hyperNodes, nodes, nil, unicode))
			assert.Equal(t, expected, outputRows(output.String()))
			assert.Contains(t, output.String(), "ALLOCATABLE/CAPACITY (not free resources)")
			assert.NotContains(t, output.String(), "\t")
			assert.NotContains(t, output.String(), "999")
			if !unicode {
				assert.NotContains(t, output.String(), "`")
				assert.NotContains(t, output.String(), "├")
				assert.Contains(t, output.String(), "|   +-- node-a")
			} else {
				assert.Contains(t, output.String(), "│   ├── node-a")
			}
			if !reverse && unicode {
				original = output.String()
			}
			if reverse && unicode {
				assert.Equal(t, original, output.String())
			}
		}
	}
}

func TestPrintTreeConnectorsAndColumnAlignment(t *testing.T) {
	hyperNodes := []topologyv1alpha1.HyperNode{
		testHyperNode("root", 2, exactMember(topologyv1alpha1.MemberTypeHyperNode, "rack")),
		testHyperNode("rack", 1, exactMember(topologyv1alpha1.MemberTypeNode, "node-with-a-long-name"), exactMember(topologyv1alpha1.MemberTypeNode, "node-b")),
	}
	hyperNodes[1].Spec.TierName = "机架"
	nodes := []corev1.Node{
		testNode("node-with-a-long-name", corev1.ConditionTrue, false, nil),
		testNode("node-b", corev1.ConditionFalse, true, nil),
	}
	for _, tt := range []struct {
		name    string
		unicode bool
		prefix  []string
	}{
		{"unicode", true, []string{"NAME", "root", "└── rack", "    ├── node-b", "    └── node-with-a-long-name"}},
		{"ascii", false, []string{"NAME", "root", "+-- rack", "    +-- node-b", "    +-- node-with-a-long-name"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			require.NoError(t, printTree(&output, hyperNodes, nodes, nil, tt.unicode))
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			require.Len(t, lines, len(tt.prefix))
			for i, prefix := range tt.prefix {
				assert.True(t, strings.HasPrefix(lines[i], prefix), lines[i])
			}
			// Count visible columns independently of the renderer: tree runes are
			// single-width and this fixture's two CJK runes are double-width.
			visible := func(s string) int {
				n := 0
				for _, r := range s {
					n++
					if r == '机' || r == '架' {
						n++
					}
				}
				return n
			}
			statusColumn := strings.Index(lines[0], "STATUS")
			for i, value := range []string{"-", "-", "NotReady,SchedulingDisabled", "Ready"} {
				line := lines[i+1]
				afterName := line[len(tt.prefix[i+1]):]
				start := strings.Index(afterName, value) + len(tt.prefix[i+1])
				assert.Equal(t, statusColumn, visible(line[:start]), line)
			}
		})
	}
}

func TestPrintTreeParentFirstChain(t *testing.T) {
	hyperNodes := []topologyv1alpha1.HyperNode{
		testHyperNode("b-middle", 2, exactMember(topologyv1alpha1.MemberTypeHyperNode, "c-rack")),
		testHyperNode("a-root", 3, exactMember(topologyv1alpha1.MemberTypeHyperNode, "b-middle")),
		testHyperNode("c-rack", 1, exactMember(topologyv1alpha1.MemberTypeNode, "node")),
	}
	var output bytes.Buffer
	require.NoError(t, printTree(&output, hyperNodes, []corev1.Node{
		testNode("node", corev1.ConditionTrue, false, nil),
	}, nil, true))
	assert.Equal(t, [][]string{
		{"NAME", "TIER", "STATUS", "NODES", "READY", "CORDONED"},
		{"a-root", "3", "-", "1", "1/1", "0"},
		{"b-middle", "2", "-", "1", "1/1", "0"},
		{"c-rack", "1", "-", "1", "1/1", "0"},
		{"node", "-", "Ready", "-", "-", "-"},
	}, outputRows(output.String()))
}

func TestPrintTreeMissingResourcesAndUnknownStatus(t *testing.T) {
	nodes := []corev1.Node{
		testNode("node-a", "", false, map[corev1.ResourceName][2]string{
			"vendor.com/device": {"", "8"}, "vendor.com/zero": {"0", "0"},
			"kubernetes.io/native": {"10", "10"}, "vendor.com/capacity-only": {"", "16"},
		}),
		testNode("node-b", corev1.ConditionUnknown, true, map[corev1.ResourceName][2]string{
			"vendor.com/device": {"2", ""}, "vendor.com/zero": {"0", ""},
		}),
	}
	hn := testHyperNode("root", 1,
		exactMember(topologyv1alpha1.MemberTypeNode, "node-a"),
		exactMember(topologyv1alpha1.MemberTypeNode, "node-b"))
	var output bytes.Buffer
	require.NoError(t, printTree(&output, []topologyv1alpha1.HyperNode{hn}, nodes, nil, false))
	assert.Equal(t, [][]string{
		{"NAME", "TIER", "STATUS", "NODES", "READY", "CORDONED", "vendor.com/capacity-only", "vendor.com/device", "vendor.com/zero"},
		{"root", "1", "-", "2", "0/2", "1", "-/16", "2/8", "0/0"},
		{"node-a", "-", "Unknown", "-", "-", "-", "-/16", "-/8", "0/0"},
		{"node-b", "-", "Unknown,SchedulingDisabled", "-", "-", "-", "-/-", "2/-", "0/-"},
	}, outputRows(output.String()))
}

func TestPrintTreeMixedQuantityFormatsRemainStable(t *testing.T) {
	hn := testHyperNode("root", 1,
		exactMember(topologyv1alpha1.MemberTypeNode, "node-a"),
		exactMember(topologyv1alpha1.MemberTypeNode, "node-b"))
	nodes := []corev1.Node{
		testNode("node-a", corev1.ConditionTrue, false, map[corev1.ResourceName][2]string{
			"vendor.com/device-memory": {"1Gi", "1Gi"},
		}),
		testNode("node-b", corev1.ConditionTrue, false, map[corev1.ResourceName][2]string{
			"vendor.com/device-memory": {"1073741824", "1073741824"},
		}),
	}
	for i := 0; i < 256; i++ {
		var output bytes.Buffer
		require.NoError(t, printTree(&output, []topologyv1alpha1.HyperNode{hn}, nodes, nil, false))
		require.Equal(t, []string{"root", "1", "-", "2", "2/2", "0", "2Gi/2Gi"}, outputRows(output.String())[1])
	}
}

func TestPrintTreeRejectsInvalidSnapshots(t *testing.T) {
	hnMember, nodeMember := topologyv1alpha1.MemberTypeHyperNode, topologyv1alpha1.MemberTypeNode
	one := func(member topologyv1alpha1.MemberSpec) []topologyv1alpha1.HyperNode {
		return []topologyv1alpha1.HyperNode{testHyperNode("root", 1, member)}
	}
	for _, tt := range []struct {
		name       string
		hyperNodes []topologyv1alpha1.HyperNode
		want       string
	}{
		{"missing hypernode", one(exactMember(hnMember, "absent")), "absent"},
		{"missing node", one(exactMember(nodeMember, "absent")), "absent"},
		{"self reference", one(exactMember(hnMember, "root")), "cyclic"},
		{"pure cycle", []topologyv1alpha1.HyperNode{
			testHyperNode("one", 1, exactMember(hnMember, "two")),
			testHyperNode("two", 2, exactMember(hnMember, "one")),
		}, "cyclic"},
		{"cycle beside a valid root", []topologyv1alpha1.HyperNode{
			testHyperNode("a-root", 1, exactMember(nodeMember, "node")),
			testHyperNode("one", 1, exactMember(hnMember, "two")),
			testHyperNode("two", 2, exactMember(hnMember, "one")),
		}, "cyclic"},
		{"multiple parents", []topologyv1alpha1.HyperNode{
			testHyperNode("one", 2, exactMember(hnMember, "child")),
			testHyperNode("two", 2, exactMember(hnMember, "child")),
			testHyperNode("child", 1, exactMember(nodeMember, "node")),
		}, "parent"},
		{"unknown type", one(exactMember("Other", "node")), "type"},
		{"missing selector", one(topologyv1alpha1.MemberSpec{Type: nodeMember}), "selector"},
		{"empty exact name", one(exactMember(nodeMember, "")), "name"},
		{"invalid regex", one(topologyv1alpha1.MemberSpec{
			Type:     nodeMember,
			Selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: "["}},
		}), "regex"},
		{"empty regex", one(topologyv1alpha1.MemberSpec{
			Type:     nodeMember,
			Selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{}},
		}), "regex"},
		{"invalid label selector", one(topologyv1alpha1.MemberSpec{
			Type: nodeMember,
			Selector: topologyv1alpha1.MemberSelector{LabelMatch: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "rack", Operator: "Invalid"}},
			}},
		}), "label"},
		{"unsupported hypernode selector", one(topologyv1alpha1.MemberSpec{
			Type:     hnMember,
			Selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: ".*"}},
		}), "exactMatch"},
		{"conflicting selectors", one(topologyv1alpha1.MemberSpec{
			Type: nodeMember,
			Selector: topologyv1alpha1.MemberSelector{
				ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node"},
				LabelMatch: &metav1.LabelSelector{},
			},
		}), "selector"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := printTree(&output, tt.hyperNodes, []corev1.Node{testNode("node", corev1.ConditionTrue, false, nil)}, nil, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.Empty(t, output.String())
		})
	}
}

func TestSelectCharset(t *testing.T) {
	for _, tt := range []struct {
		name    string
		charset string
		tty     bool
		env     map[string]string
		unicode bool
	}{
		{"utf8 terminal", "auto", true, map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}, true},
		{"utf8 spelling", "auto", true, map[string]string{"TERM": "xterm", "LC_CTYPE": "C.utf8"}, true},
		{"bare utf8 locale", "auto", true, map[string]string{"TERM": "xterm", "LC_CTYPE": "UTF-8"}, true},
		{"locale precedence", "auto", true, map[string]string{"TERM": "xterm", "LC_ALL": "C", "LANG": "en_US.UTF-8"}, false},
		{"ctype precedence", "auto", true, map[string]string{"TERM": "xterm", "LC_CTYPE": "C", "LANG": "en_US.UTF-8"}, false},
		{"empty all falls through", "auto", true, map[string]string{"TERM": "xterm", "LC_ALL": "", "LANG": "C.UTF-8"}, true},
		{"unknown locale", "auto", true, map[string]string{"TERM": "xterm"}, false},
		{"unknown terminal", "auto", true, map[string]string{"LANG": "C.UTF-8"}, false},
		{"dumb terminal", "auto", true, map[string]string{"TERM": "dumb", "LANG": "C.UTF-8"}, false},
		{"pipe", "auto", false, map[string]string{"TERM": "xterm", "LANG": "C.UTF-8"}, false},
		{"force unicode", "unicode", false, nil, true},
		{"force ascii", "ascii", true, map[string]string{"TERM": "xterm", "LANG": "C.UTF-8"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectCharset(tt.charset, tt.tty, func(key string) string { return tt.env[key] })
			require.NoError(t, err)
			assert.Equal(t, tt.unicode, got)
		})
	}
	_, err := selectCharset("invalid", true, func(string) string { return "" })
	require.Error(t, err)
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("output closed")
}

func TestPrintTreeEmptyAndWriterErrors(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, printTree(&output, nil, nil, nil, false))
	assert.Equal(t, "No HyperNodes found.\n", output.String())
	require.ErrorContains(t, printTree(failingWriter{}, nil, nil, nil, false), "output closed")
	hn := testHyperNode("root", 1, exactMember(topologyv1alpha1.MemberTypeNode, "node"))
	require.ErrorContains(t, printTree(failingWriter{}, []topologyv1alpha1.HyperNode{hn}, []corev1.Node{
		testNode("node", corev1.ConditionTrue, false, nil),
	}, nil, false), "output closed")
}
