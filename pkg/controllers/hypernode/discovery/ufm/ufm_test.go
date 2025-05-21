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

package ufm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

func TestUfmDiscoverer_Start(t *testing.T) {
	tests := []struct {
		name               string
		endpoint           string
		username           string
		password           string
		mockResponseCode   int
		expectedError      bool
		expectHyperNodes   bool
		expectedHyperNodes map[string]*topologyv1alpha1.HyperNode
	}{
		{
			name:          "MissingEndpoint",
			endpoint:      "",
			username:      "user",
			password:      "pass",
			expectedError: true,
		},
		{
			name:          "MissingUsername",
			endpoint:      "https://ufm.example.com",
			username:      "",
			password:      "pass",
			expectedError: true,
		},
		{
			name:          "MissingPassword",
			endpoint:      "https://ufm.example.com",
			username:      "user",
			password:      "",
			expectedError: true,
		},
		{
			name:               "Success",
			endpoint:           "https://ufm.example.com",
			username:           "user",
			password:           "pass",
			mockResponseCode:   http.StatusOK,
			expectHyperNodes:   true,
			expectedHyperNodes: expectedHyperNodes(),
		},
		{
			name:               "ServerError",
			endpoint:           "https://ufm.example.com",
			username:           "user",
			password:           "pass",
			mockResponseCode:   http.StatusInternalServerError,
			expectHyperNodes:   false,
			expectedHyperNodes: map[string]*topologyv1alpha1.HyperNode{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var serverBaseURL string
			// Setup test server (only for non-error configuration test cases)
			if tc.endpoint != "" && !tc.expectedError {
				server := setupTestServer(t, tc.mockResponseCode)
				defer server.Close()
				serverBaseURL = server.URL
			} else {
				serverBaseURL = tc.endpoint
			}

			u := &ufmDiscoverer{
				endpoint:          serverBaseURL,
				username:          tc.username,
				password:          tc.password,
				discoveryInterval: time.Minute,
				client:            &http.Client{},
				stopCh:            make(chan struct{}),
			}

			outputCh, err := u.Start()

			if tc.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// If HyperNodes are expected, verify the generated nodes
			if tc.expectHyperNodes {
				var hyperNodes []*topologyv1alpha1.HyperNode
				select {
				case hyperNodes = <-outputCh:
				case <-time.After(time.Second):
					t.Fatal("Timeout waiting for output")
				}

				assert.Equal(t, len(tc.expectedHyperNodes), len(hyperNodes), "Hypernode count should match")
				for _, hn := range hyperNodes {
					expected, exists := tc.expectedHyperNodes[hn.Name]
					assert.True(t, exists, "Generated hypernode %s should exist in expected hypernodes", hn.Name)
					assert.Equal(t, expected, hn)
				}
			}

			u.Stop()
		})
	}
}

// setupTestServer creates and returns an HTTP test server that mocks the UFM API
func setupTestServer(t *testing.T, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		if statusCode == http.StatusOK {
			data, err := json.Marshal(mockUfmData())
			if err != nil {
				t.Fatalf("Failed to serialize mock data: %v", err)
			}
			w.Write(data)
		} else {
			w.Write([]byte("Server Error"))
		}
	}))
}

// mockUfmData returns test UFM data for testing
func mockUfmData() []UFMInterface {
	return []UFMInterface{
		// group0
		{SystemName: "node0", PeerNodeName: "leaf0", NodeDescription: "node00-x", Description: "Computer IB Port"},
		{SystemName: "node0", PeerNodeName: "leaf1", NodeDescription: "node00-x", Description: "Computer IB Port"},
		{SystemName: "node0", PeerNodeName: "leaf2", NodeDescription: "node00-x", Description: "Computer IB Port"},
		{SystemName: "node0", PeerNodeName: "leaf3", NodeDescription: "node00-x", Description: "Computer IB Port"},

		{SystemName: "node1", PeerNodeName: "leaf0", NodeDescription: "node01-x", Description: "Computer IB Port"},
		{SystemName: "node1", PeerNodeName: "leaf1", NodeDescription: "node01-x", Description: "Computer IB Port"},
		{SystemName: "node1", PeerNodeName: "leaf2", NodeDescription: "node01-x", Description: "Computer IB Port"},
		{SystemName: "node1", PeerNodeName: "leaf3", NodeDescription: "node01-x", Description: "Computer IB Port"},

		{SystemName: "node2", PeerNodeName: "leaf0", NodeDescription: "node02-x", Description: "Computer IB Port"},
		{SystemName: "node2", PeerNodeName: "leaf1", NodeDescription: "node02-x", Description: "Computer IB Port"},
		{SystemName: "node2", PeerNodeName: "leaf2", NodeDescription: "node02-x", Description: "Computer IB Port"},
		{SystemName: "node2", PeerNodeName: "leaf3", NodeDescription: "node02-x", Description: "Computer IB Port"},

		{SystemName: "node3", PeerNodeName: "leaf0", NodeDescription: "node03-x", Description: "Computer IB Port"},
		{SystemName: "node3", PeerNodeName: "leaf1", NodeDescription: "node03-x", Description: "Computer IB Port"},
		{SystemName: "node3", PeerNodeName: "leaf2", NodeDescription: "node03-x", Description: "Computer IB Port"},
		{SystemName: "node3", PeerNodeName: "leaf3", NodeDescription: "node03-x", Description: "Computer IB Port"},

		// group1
		{SystemName: "node4", PeerNodeName: "leaf4", NodeDescription: "node14-x", Description: "Computer IB Port"},
		{SystemName: "node4", PeerNodeName: "leaf5", NodeDescription: "node14-x", Description: "Computer IB Port"},
		{SystemName: "node4", PeerNodeName: "leaf6", NodeDescription: "node14-x", Description: "Computer IB Port"},

		{SystemName: "node5", PeerNodeName: "leaf4", NodeDescription: "node15-x", Description: "Computer IB Port"},
		{SystemName: "node5", PeerNodeName: "leaf5", NodeDescription: "node15-x", Description: "Computer IB Port"},
		{SystemName: "node5", PeerNodeName: "leaf6", NodeDescription: "node15-x", Description: "Computer IB Port"},
		{SystemName: "node5", PeerNodeName: "leaf7", NodeDescription: "node15-x", Description: "Computer IB Port"},

		{SystemName: "node6", PeerNodeName: "leaf4", NodeDescription: "node16-x", Description: "Computer IB Port"},
		{SystemName: "node6", PeerNodeName: "leaf5", NodeDescription: "node16-x", Description: "Computer IB Port"},
		{SystemName: "node6", PeerNodeName: "leaf6", NodeDescription: "node16-x", Description: "Computer IB Port"},
		{SystemName: "node6", PeerNodeName: "leaf7", NodeDescription: "node16-x", Description: "Computer IB Port"},

		{SystemName: "node7", PeerNodeName: "leaf4", NodeDescription: "node17-x", Description: "Computer IB Port"},
		{SystemName: "node7", PeerNodeName: "leaf5", NodeDescription: "node17-x", Description: "Computer IB Port"},
		{SystemName: "node7", PeerNodeName: "leaf6", NodeDescription: "node17-x", Description: "Computer IB Port"},
	}
}

func buildHyperNode(name string, tier int, members []topologyv1alpha1.MemberSpec, labels map[string]string) *topologyv1alpha1.HyperNode {
	return &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier:    tier,
			Members: members,
		},
	}
}

func expectedHyperNodes() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"leaf-hn-0": buildHyperNode("leaf-hn-0", 1,
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node1"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node2"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node3"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "ufm"}),
		"leaf-hn-1": buildHyperNode("leaf-hn-1", 1,
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node4"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node5"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node6"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node7"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "ufm"}),
		"spine-hn": buildHyperNode("spine-hn", 2,
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "leaf-hn-0"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "leaf-hn-1"}},
				}}, map[string]string{api.NetworkTopologySourceLabelKey: "ufm"}),
	}
}
