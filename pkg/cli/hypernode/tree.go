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
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/width"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	corehelper "k8s.io/kubernetes/pkg/apis/core/v1/helper"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	schedulerapi "volcano.sh/volcano/pkg/scheduler/api"
	hypernodeutil "volcano.sh/volcano/pkg/util/hypernode"
)

type tree struct {
	hyperNodes schedulerapi.HyperNodeInfoMap
	nodes      map[string]*corev1.Node
	direct     map[string]sets.Set[string]
	resolved   map[string]sets.Set[string]
	roots      []string
	resources  []corev1.ResourceName
	dra        draInventory
}

func buildTree(hyperNodes []topologyv1alpha1.HyperNode, nodes []corev1.Node) (*tree, error) {
	result := &tree{
		nodes:  make(map[string]*corev1.Node, len(nodes)),
		direct: make(map[string]sets.Set[string], len(hyperNodes)),
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	nodeList := make([]*corev1.Node, 0, len(nodes))
	for i := range nodes {
		node := &nodes[i]
		result.nodes[node.Name] = node
		nodeList = append(nodeList, node)
		if err := indexer.Add(node); err != nil {
			return nil, fmt.Errorf("index Node %q: %w", node.Name, err)
		}
	}
	names := sets.New[string]()
	for _, hn := range hyperNodes {
		names.Insert(hn.Name)
	}
	for _, hn := range hyperNodes {
		result.direct[hn.Name] = sets.New[string]()
		for i, member := range hn.Spec.Members {
			if err := validateMember(member, names, result.nodes); err != nil {
				return nil, fmt.Errorf("HyperNode %q member %d: %w", hn.Name, i, err)
			}
			if member.Type == topologyv1alpha1.MemberTypeNode {
				result.direct[hn.Name].Insert(hypernodeutil.GetMembers(member.Selector, nodeList).UnsortedList()...)
			}
		}
	}

	info := schedulerapi.NewHyperNodesInfo(corelisters.NewNodeLister(indexer))
	ordered := append([]topologyv1alpha1.HyperNode(nil), hyperNodes...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	for i := range ordered {
		if err := info.UpdateHyperNode(&ordered[i]); err != nil {
			return nil, fmt.Errorf("parse HyperNode %q: %w", ordered[i].Name, err)
		}
	}
	result.hyperNodes = info.HyperNodes()
	result.resolved = info.RealNodesSet()
	resourceNames := sets.New[string]()
	for name, hn := range result.hyperNodes {
		if !names.Has(name) {
			return nil, fmt.Errorf("resolved HyperNode %q does not exist in the snapshot", name)
		}
		if hn.Parent == "" {
			result.roots = append(result.roots, name)
		}
		for nodeName := range result.resolved[name] {
			node, exists := result.nodes[nodeName]
			if !exists {
				return nil, fmt.Errorf("HyperNode %q resolved missing Node %q", name, nodeName)
			}
			for _, resources := range []corev1.ResourceList{node.Status.Allocatable, node.Status.Capacity} {
				for resourceName := range resources {
					if corehelper.IsExtendedResourceName(resourceName) {
						resourceNames.Insert(string(resourceName))
					}
				}
			}
		}
	}
	sort.Strings(result.roots)
	for _, name := range sets.List(resourceNames) {
		result.resources = append(result.resources, corev1.ResourceName(name))
	}
	return result, nil
}

func validateMember(member topologyv1alpha1.MemberSpec, hyperNodes sets.Set[string], nodes map[string]*corev1.Node) error {
	if member.Type != topologyv1alpha1.MemberTypeNode && member.Type != topologyv1alpha1.MemberTypeHyperNode {
		return fmt.Errorf("unknown member type %q", member.Type)
	}
	selector := member.Selector
	count := 0
	if selector.ExactMatch != nil {
		count++
	}
	if selector.RegexMatch != nil {
		count++
	}
	if selector.LabelMatch != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("selector must contain exactly one of exactMatch, regexMatch or labelMatch")
	}
	if member.Type == topologyv1alpha1.MemberTypeHyperNode && selector.ExactMatch == nil {
		return fmt.Errorf("HyperNode members require an exactMatch selector")
	}
	if selector.ExactMatch != nil {
		name := selector.ExactMatch.Name
		if name == "" {
			return fmt.Errorf("exactMatch name must not be empty")
		}
		if member.Type == topologyv1alpha1.MemberTypeHyperNode {
			if !hyperNodes.Has(name) {
				return fmt.Errorf("referenced HyperNode %q does not exist", name)
			}
		} else if _, exists := nodes[name]; !exists {
			return fmt.Errorf("referenced Node %q does not exist", name)
		}
	}
	if selector.RegexMatch != nil {
		if selector.RegexMatch.Pattern == "" {
			return fmt.Errorf("regexMatch pattern must not be empty")
		}
		if _, err := regexp.Compile(selector.RegexMatch.Pattern); err != nil {
			return fmt.Errorf("invalid regexMatch: %w", err)
		}
	}
	if selector.LabelMatch != nil {
		if _, err := metav1.LabelSelectorAsSelector(selector.LabelMatch); err != nil {
			return fmt.Errorf("invalid labelMatch: %w", err)
		}
	}
	return nil
}

func printTree(out io.Writer, hyperNodes []topologyv1alpha1.HyperNode, nodes []corev1.Node, resourceSlices []resourcev1.ResourceSlice, unicodeLines bool) error {
	if len(hyperNodes) == 0 {
		_, err := fmt.Fprintln(out, "No HyperNodes found.")
		return err
	}
	topology, err := buildTree(hyperNodes, nodes)
	if err != nil {
		return err
	}
	members := sets.New[string]()
	for _, root := range topology.roots {
		members.Insert(topology.resolved[root].UnsortedList()...)
	}
	topology.dra, err = buildDRAInventory(resourceSlices, members)
	if err != nil {
		return err
	}
	rows := [][]string{{"NAME", "TIER", "STATUS", "NODES", "READY", "CORDONED"}}
	for _, name := range topology.resources {
		rows[0] = append(rows[0], string(name))
	}
	for _, driver := range topology.dra.drivers {
		rows[0] = append(rows[0], "DRA:"+driver)
	}
	branch, lastBranch, continuation := "+-- ", "+-- ", "|   "
	if unicodeLines {
		branch, lastBranch, continuation = "├── ", "└── ", "│   "
	}
	var visit func(name, prefix, connector string, last bool)
	visit = func(name, prefix, connector string, last bool) {
		hn := topology.hyperNodes[name]
		tier := strconv.Itoa(hn.Tier())
		if hn.TierName() != "" {
			tier += "/" + escapeControls(hn.TierName())
		}
		members := topology.resolved[name]
		ready, cordoned := 0, 0
		for nodeName := range members {
			node := topology.nodes[nodeName]
			if nodeReady(node) == "Ready" {
				ready++
			}
			if node.Spec.Unschedulable {
				cordoned++
			}
		}
		row := []string{
			prefix + connector + name, tier, "-", strconv.Itoa(members.Len()),
			fmt.Sprintf("%d/%d", ready, members.Len()), strconv.Itoa(cordoned),
		}
		rows = append(rows, append(row, topology.resourceValues(members)...))
		childPrefix := prefix
		if connector != "" {
			if last {
				childPrefix += "    "
			} else {
				childPrefix += continuation
			}
		}
		children, direct := sets.List(hn.Children), sets.List(topology.direct[name])
		total := len(children) + len(direct)
		for i, child := range children {
			nextConnector := branch
			if i == total-1 {
				nextConnector = lastBranch
			}
			visit(child, childPrefix, nextConnector, i == total-1)
		}
		for i, nodeName := range direct {
			connector := branch
			if len(children)+i == total-1 {
				connector = lastBranch
			}
			node := topology.nodes[nodeName]
			status := nodeReady(node)
			if node.Spec.Unschedulable {
				status += ",SchedulingDisabled"
			}
			row := []string{childPrefix + connector + nodeName, "-", status, "-", "-", "-"}
			rows = append(rows, append(row, topology.resourceValues(sets.New(nodeName))...))
		}
	}
	for _, root := range topology.roots {
		visit(root, "", "", true)
	}
	if len(topology.resources) != 0 {
		if _, err := fmt.Fprintln(out, "RESOURCE VALUES: ALLOCATABLE/CAPACITY (not free resources)"); err != nil {
			return err
		}
	}
	if len(topology.dra.drivers) != 0 {
		if _, err := fmt.Fprintln(out, "DRA VALUES: ADVERTISED DEVICE COUNT (not allocatable or free resources)"); err != nil {
			return err
		}
	}
	if len(topology.resources)+len(topology.dra.drivers) != 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}
	return printRows(out, rows)
}

func (t *tree) resourceValues(members sets.Set[string]) []string {
	values := make([]string, 0, len(t.resources))
	ordered := sets.List(members)
	for _, name := range t.resources {
		var totals [2]resource.Quantity
		var reported [2]bool
		for _, member := range ordered {
			node := t.nodes[member]
			for i, resources := range []corev1.ResourceList{node.Status.Allocatable, node.Status.Capacity} {
				if value, exists := resources[name]; exists {
					totals[i].Add(value)
					reported[i] = true
				}
			}
		}
		parts := [2]string{"-", "-"}
		for i := range totals {
			if reported[i] {
				parts[i] = totals[i].String()
			}
		}
		values = append(values, parts[0]+"/"+parts[1])
	}
	return append(values, t.dra.values(members)...)
}

func nodeReady(node *corev1.Node) string {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			switch condition.Status {
			case corev1.ConditionTrue:
				return "Ready"
			case corev1.ConditionFalse:
				return "NotReady"
			default:
				return "Unknown"
			}
		}
	}
	return "Unknown"
}

func selectCharset(charset string, terminal bool, getenv func(string) string) (bool, error) {
	switch charset {
	case "unicode":
		return true, nil
	case "ascii":
		return false, nil
	case "auto":
		if !terminal || getenv("TERM") == "" || getenv("TERM") == "dumb" {
			return false, nil
		}
		for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
			if locale := getenv(name); locale != "" {
				encoding := strings.ToUpper(strings.SplitN(locale, "@", 2)[0])
				return encoding == "UTF-8" || encoding == "UTF8" ||
					strings.HasSuffix(encoding, ".UTF-8") || strings.HasSuffix(encoding, ".UTF8"), nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("invalid charset %q: expected auto, ascii or unicode", charset)
	}
}

func printRows(out io.Writer, rows [][]string) error {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], displayWidth(value))
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, value := range row {
			line.WriteString(value)
			if i != len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-displayWidth(value)+2))
			}
		}
		line.WriteByte('\n')
		if _, err := io.WriteString(out, line.String()); err != nil {
			return err
		}
	}
	return nil
}

func displayWidth(value string) int {
	result := 0
	for _, r := range value {
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			result += 2
		default:
			result++
		}
	}
	return result
}

func escapeControls(value string) string {
	var result strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) {
			fmt.Fprintf(&result, "\\u%04x", r)
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}
