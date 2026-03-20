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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

func TestUfmDiscoverer_Start(t *testing.T) {
	tests := []struct {
		name               string
		config             api.DiscoveryConfig
		mockResponseCode   int
		secretExists       bool
		secretData         map[string][]byte
		expectedError      bool
		expectHyperNodes   bool
		expectedHyperNodes map[string]*topologyv1alpha1.HyperNode
	}{
		{
			name: "MissingEndpoint",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "",
				},
				Credentials: &api.Credentials{
					SecretRef: &api.SecretRef{
						Name:      "ufm-creds",
						Namespace: "default",
					},
				},
			},
			secretExists:  true,
			secretData:    map[string][]byte{"username": []byte("user"), "password": []byte("pass")},
			expectedError: true,
		},
		{
			name: "MissingSecretRef",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "https://ufm.example.com",
				},
			},
			expectedError: true,
		},
		{
			name: "SecretNotFound",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "https://ufm.example.com",
				},
				Credentials: &api.Credentials{
					SecretRef: &api.SecretRef{
						Name:      "nonexistent",
						Namespace: "default",
					},
				},
			},
			secretExists:  false,
			expectedError: true,
		},
		{
			name: "MissingUsername",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "https://ufm.example.com",
				},
				Credentials: &api.Credentials{
					SecretRef: &api.SecretRef{
						Name:      "ufm-creds",
						Namespace: "default",
					},
				},
			},
			secretExists:  true,
			secretData:    map[string][]byte{"password": []byte("pass")},
			expectedError: true,
		},
		{
			name: "MissingPassword",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "https://ufm.example.com",
				},
				Credentials: &api.Credentials{
					SecretRef: &api.SecretRef{
						Name:      "ufm-creds",
						Namespace: "default",
					},
				},
			},
			secretExists:  true,
			secretData:    map[string][]byte{"username": []byte("user")},
			expectedError: true,
		},
		{
			name: "Success",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "https://ufm.example.com",
				},
				Credentials: &api.Credentials{
					SecretRef: &api.SecretRef{
						Name:      "ufm-creds",
						Namespace: "default",
					},
				},
				Interval: time.Minute,
			},
			secretExists:       true,
			secretData:         map[string][]byte{"username": []byte("user"), "password": []byte("pass")},
			mockResponseCode:   http.StatusOK,
			expectHyperNodes:   true,
			expectedHyperNodes: expectedHyperNodes(),
		},
		{
			name: "ServerError",
			config: api.DiscoveryConfig{
				Source: "ufm",
				Config: map[string]interface{}{
					"endpoint": "https://ufm.example.com",
				},
				Credentials: &api.Credentials{
					SecretRef: &api.SecretRef{
						Name:      "ufm-creds",
						Namespace: "default",
					},
				},
				Interval: time.Minute,
			},
			secretExists:       true,
			secretData:         map[string][]byte{"username": []byte("user"), "password": []byte("pass")},
			mockResponseCode:   http.StatusInternalServerError,
			expectHyperNodes:   false,
			expectedHyperNodes: map[string]*topologyv1alpha1.HyperNode{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			fakeVcClient := vcclientset.NewSimpleClientset()
			if tc.secretExists {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tc.config.Credentials.SecretRef.Name,
						Namespace: tc.config.Credentials.SecretRef.Namespace,
					},
					Data: tc.secretData,
				}
				_, err := fakeClient.CoreV1().Secrets(tc.config.Credentials.SecretRef.Namespace).Create(
					context.TODO(), secret, metav1.CreateOptions{},
				)
				if err != nil {
					t.Fatalf("Failed to create test secret: %v", err)
				}
			}

			var serverBaseURL string
			// Setup test server (only for non-error configuration test cases)
			if tc.config.Config["endpoint"] != "" && tc.secretExists && !tc.expectedError {
				server := setupTestServer(t, tc.mockResponseCode)
				defer server.Close()
				serverBaseURL = server.URL
				tc.config.Config["endpoint"] = serverBaseURL
			}

			u := NewUFMDiscoverer(tc.config, fakeClient, fakeVcClient)
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
