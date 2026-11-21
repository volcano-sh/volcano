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

package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

const (
	hyperNodeName = "hypernode-tier1"
	labelSource   = "label"
	ufmSource     = "ufm"
)

var hyperNodeResource = schema.GroupResource{Group: "topology.volcano.sh", Resource: "hypernodes"}

// newHyperNode builds a HyperNode owned by the given discovery source.
func newHyperNode(source string, nodeNames []string, extraLabels map[string]string) *topologyv1alpha1.HyperNode {
	labels := map[string]string{}
	if source != "" {
		labels[api.NetworkTopologySourceLabelKey] = source
	}
	for k, v := range extraLabels {
		labels[k] = v
	}
	return BuildHyperNode(hyperNodeName, 1, BuildMembers(nodeNames, topologyv1alpha1.MemberTypeNode), labels)
}

func getHyperNode(t *testing.T, client *vcfake.Clientset) *topologyv1alpha1.HyperNode {
	t.Helper()
	hn, err := client.TopologyV1alpha1().HyperNodes().Get(context.Background(), hyperNodeName, metav1.GetOptions{})
	require.NoError(t, err)
	return hn
}

func TestCreateOrUpdateHyperNode(t *testing.T) {
	tests := []struct {
		name         string
		existing     *topologyv1alpha1.HyperNode
		desired      *topologyv1alpha1.HyperNode
		wantErr      bool
		wantMembers  []string
		wantLabels   map[string]string
		wantNoUpdate bool
	}{
		{
			name:        "creates the HyperNode when it does not exist",
			desired:     newHyperNode(labelSource, []string{"node-b", "node-a"}, nil),
			wantMembers: []string{"node-a", "node-b"},
			wantLabels:  map[string]string{api.NetworkTopologySourceLabelKey: labelSource},
		},
		{
			name:        "updates an existing HyperNode owned by the same source and keeps its other labels",
			existing:    newHyperNode(labelSource, []string{"node-a"}, map[string]string{"team": "infra"}),
			desired:     newHyperNode(labelSource, []string{"node-a", "node-c"}, nil),
			wantMembers: []string{"node-a", "node-c"},
			wantLabels:  map[string]string{api.NetworkTopologySourceLabelKey: labelSource, "team": "infra"},
		},
		{
			name:         "refuses to update a HyperNode owned by another source",
			existing:     newHyperNode(ufmSource, []string{"node-a"}, nil),
			desired:      newHyperNode(labelSource, []string{"node-c"}, nil),
			wantErr:      true,
			wantMembers:  []string{"node-a"},
			wantLabels:   map[string]string{api.NetworkTopologySourceLabelKey: ufmSource},
			wantNoUpdate: true,
		},
		{
			name:         "refuses to update when the desired HyperNode has no source label",
			existing:     newHyperNode(labelSource, []string{"node-a"}, nil),
			desired:      newHyperNode("", []string{"node-c"}, nil),
			wantErr:      true,
			wantMembers:  []string{"node-a"},
			wantLabels:   map[string]string{api.NetworkTopologySourceLabelKey: labelSource},
			wantNoUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := vcfake.NewSimpleClientset()
			if tt.existing != nil {
				_, err := client.TopologyV1alpha1().HyperNodes().Create(context.Background(), tt.existing, metav1.CreateOptions{})
				require.NoError(t, err)
			}
			client.ClearActions()

			err := CreateOrUpdateHyperNode(client, tt.desired)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			hn := getHyperNode(t, client)
			var gotMembers []string
			for _, m := range hn.Spec.Members {
				gotMembers = append(gotMembers, m.Selector.ExactMatch.Name)
			}
			assert.Equal(t, tt.wantMembers, gotMembers)
			assert.Equal(t, tt.wantLabels, hn.Labels)

			if tt.wantNoUpdate {
				for _, action := range client.Actions() {
					assert.NotEqual(t, "update", action.GetVerb(), "HyperNode must not be updated")
				}
			}
		})
	}
}

func TestCreateOrUpdateHyperNodeReturnsCreateError(t *testing.T) {
	client := vcfake.NewSimpleClientset()
	forbidden := apierrors.NewForbidden(hyperNodeResource, hyperNodeName, nil)
	client.PrependReactor("create", "hypernodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, forbidden
	})

	err := CreateOrUpdateHyperNode(client, newHyperNode(labelSource, []string{"node-a"}, nil))

	assert.True(t, apierrors.IsForbidden(err))
	for _, action := range client.Actions() {
		assert.NotEqual(t, "get", action.GetVerb(), "an error other than AlreadyExists must not fall back to update")
	}
}

func TestUpdateHyperNode(t *testing.T) {
	t.Run("merges labels and annotations and updates spec and status", func(t *testing.T) {
		existing := newHyperNode(labelSource, []string{"node-a"}, map[string]string{"team": "infra"})
		existing.Annotations = map[string]string{"owner": "alice"}
		client := vcfake.NewSimpleClientset(existing)

		desired := newHyperNode(labelSource, []string{"node-b"}, map[string]string{"zone": "z1"})
		desired.Annotations = map[string]string{"note": "rebuilt"}
		desired.Status.NodeCount = 1

		require.NoError(t, UpdateHyperNode(client, desired))

		hn := getHyperNode(t, client)
		assert.Equal(t, desired.Spec, hn.Spec)
		assert.Equal(t, desired.Status, hn.Status)
		assert.Equal(t, map[string]string{
			api.NetworkTopologySourceLabelKey: labelSource,
			"team":                            "infra",
			"zone":                            "z1",
		}, hn.Labels)
		assert.Equal(t, map[string]string{"owner": "alice", "note": "rebuilt"}, hn.Annotations)
	})

	t.Run("retries when the update conflicts", func(t *testing.T) {
		client := vcfake.NewSimpleClientset(newHyperNode(labelSource, []string{"node-a"}, nil))
		conflicts := 0
		client.PrependReactor("update", "hypernodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			if action.GetSubresource() == "" && conflicts == 0 {
				conflicts++
				return true, nil, apierrors.NewConflict(hyperNodeResource, hyperNodeName, nil)
			}
			return false, nil, nil
		})

		desired := newHyperNode(labelSource, []string{"node-b"}, nil)
		require.NoError(t, UpdateHyperNode(client, desired))

		assert.Equal(t, 1, conflicts)
		assert.Equal(t, desired.Spec, getHyperNode(t, client).Spec)
	})

	t.Run("returns NotFound when the HyperNode does not exist", func(t *testing.T) {
		client := vcfake.NewSimpleClientset()

		err := UpdateHyperNode(client, newHyperNode(labelSource, []string{"node-a"}, nil))

		assert.True(t, apierrors.IsNotFound(err))
	})
}

func TestDeleteHyperNode(t *testing.T) {
	tests := []struct {
		name         string
		existing     []runtime.Object
		wantNotFound bool
	}{
		{
			name:     "deletes an existing HyperNode",
			existing: []runtime.Object{newHyperNode(labelSource, []string{"node-a"}, nil)},
		},
		{
			name:         "returns NotFound when the HyperNode does not exist",
			wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := vcfake.NewSimpleClientset(tt.existing...)

			err := DeleteHyperNode(client, hyperNodeName)
			if tt.wantNotFound {
				assert.True(t, apierrors.IsNotFound(err))
				return
			}
			require.NoError(t, err)

			_, err = client.TopologyV1alpha1().HyperNodes().Get(context.Background(), hyperNodeName, metav1.GetOptions{})
			assert.True(t, apierrors.IsNotFound(err))
		})
	}
}

func TestBuildHyperNodeWithTierName(t *testing.T) {
	members := BuildMembers([]string{"node-a"}, topologyv1alpha1.MemberTypeNode)
	labels := map[string]string{api.NetworkTopologySourceLabelKey: labelSource}

	hn := BuildHyperNodeWithTierName(hyperNodeName, 2, "spine", members, labels)

	assert.Equal(t, hyperNodeName, hn.Name)
	assert.Equal(t, labels, hn.Labels)
	assert.Equal(t, 2, hn.Spec.Tier)
	assert.Equal(t, "spine", hn.Spec.TierName)
	assert.Equal(t, members, hn.Spec.Members)
}

func TestBuildMembers(t *testing.T) {
	tests := []struct {
		name       string
		names      []string
		memberType topologyv1alpha1.MemberType
		wantNames  []string
	}{
		{
			name:       "sorts node members by name",
			names:      []string{"node-c", "node-a", "node-b"},
			memberType: topologyv1alpha1.MemberTypeNode,
			wantNames:  []string{"node-a", "node-b", "node-c"},
		},
		{
			name:       "builds HyperNode members",
			names:      []string{"hypernode-1"},
			memberType: topologyv1alpha1.MemberTypeHyperNode,
			wantNames:  []string{"hypernode-1"},
		},
		{
			name:       "returns an empty list for no names",
			names:      []string{},
			memberType: topologyv1alpha1.MemberTypeNode,
			wantNames:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			members := BuildMembers(tt.names, tt.memberType)

			require.Len(t, members, len(tt.wantNames))
			for i, m := range members {
				assert.Equal(t, tt.memberType, m.Type)
				require.NotNil(t, m.Selector.ExactMatch)
				assert.Equal(t, tt.wantNames[i], m.Selector.ExactMatch.Name)
			}
		})
	}
}
