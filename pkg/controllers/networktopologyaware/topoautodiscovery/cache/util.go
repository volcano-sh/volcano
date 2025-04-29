/*
Copyright 2025 The Volcano Authors.

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

package cache

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func AppendHyperNodeMember(hyperNode *topologyv1alpha1.HyperNode, memberName string, memberType topologyv1alpha1.MemberType) {
	for _, existedMember := range hyperNode.Spec.Members {
		if existedMember.Type == memberType && existedMember.Selector.ExactMatch != nil && existedMember.Selector.ExactMatch.Name == memberName {
			return
		}
	}
	hyperNode.Spec.Members = append(hyperNode.Spec.Members, topologyv1alpha1.MemberSpec{
		Type: memberType,
		Selector: topologyv1alpha1.MemberSelector{
			ExactMatch: &topologyv1alpha1.ExactMatch{
				Name: memberName,
			},
		},
	})
}

func ParseDesiredHyperNodesFromNode(node *corev1.Node, topologyName string, topology []string, desiredHyperNodes map[string]*topologyv1alpha1.HyperNode) {
	var preTierHyperNodeName string
	var leafTierVisited bool
	var preTier int
	for index, topoKey := range topology {
		tier := index + 1

		for key, value := range node.Labels {
			if topoKey != key {
				continue
			}

			hyperNodeName := GetHyperNodeName(topologyName, value, tier)
			var curHyerNode *topologyv1alpha1.HyperNode
			if _, ok := desiredHyperNodes[hyperNodeName]; !ok {
				curHyerNode = &topologyv1alpha1.HyperNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: hyperNodeName,
						Labels: map[string]string{
							key:                    value,
							LabelBasedTopologyName: topologyName,
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: tier,
					},
				}
				desiredHyperNodes[hyperNodeName] = curHyerNode
			} else {
				curHyerNode = desiredHyperNodes[hyperNodeName]
			}

			if !leafTierVisited {
				leafTierVisited = true
				AppendHyperNodeMember(curHyerNode, node.Name, topologyv1alpha1.MemberTypeNode)
			} else {
				if preTier+1 == tier {
					AppendHyperNodeMember(curHyerNode, preTierHyperNodeName, topologyv1alpha1.MemberTypeHyperNode)
				} else {
					klog.V(6).InfoS("Missing middle network topology layers", "node", node.Name, "missing tier", tier-1, "topology-name", topologyName)
				}
			}
			preTierHyperNodeName = hyperNodeName
			preTier = tier
			break
		}
	}
}

// Sort by Selector.ExactMatch.Name
func SortHyperNodeMembers(hyperNode *topologyv1alpha1.HyperNode) {
	sort.Slice(hyperNode.Spec.Members, func(i, j int) bool {
		if hyperNode.Spec.Members[i].Type != hyperNode.Spec.Members[j].Type {
			return hyperNode.Spec.Members[i].Type == topologyv1alpha1.MemberTypeNode
		}
		if hyperNode.Spec.Members[i].Selector.ExactMatch == nil {
			return true
		}
		if hyperNode.Spec.Members[j].Selector.ExactMatch == nil {
			return false
		}
		return hyperNode.Spec.Members[i].Selector.ExactMatch.Name < hyperNode.Spec.Members[j].Selector.ExactMatch.Name
	})
}

// GetHyperNodeName generates a name to the HyperNode generated from the node label

// k8s resource name rules:
// 1. Lowercase letters
// 2. Allow only lowercase letters(a-z), numbers(0-9), hyphen(-)
// 3. Doesn't start or end with hypen
// 4. No consecutive hyphens("--")

// hypernode name: ${topologyName}-t${tier}-${labelvalue}-${labelvaluehash}
func GetHyperNodeName(topologyName string, labelValue string, tier int) string {
	hyperNodeName := topologyName + "-t" + strconv.Itoa(tier) + "-" + labelValue
	hyperNodeName = strings.ToLower(hyperNodeName)
	// Replace any characters that are not lowercase letters, digits, or hyphen with hyphen character
	hyperNodeName = regexp.MustCompile(`[^a-z0-9]`).ReplaceAllLiteralString(hyperNodeName, "-")
	hyperNodeName = strings.Trim(hyperNodeName, "-")
	// Replace consecutive hyphens with a single hyphen
	hyperNodeName = regexp.MustCompile(`-+`).ReplaceAllLiteralString(hyperNodeName, "-")
	// Add hash value at the end.
	// Solve the two different labelValues like "VALUE" and "value" will be the same value "value", but has different hash value.
	hasher := fnv.New32a()
	hasher.Reset()
	hasher.Write([]byte(labelValue))
	hashHex := rand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
	hyperNodeName = hyperNodeName + "-" + hashHex
	return hyperNodeName
}
