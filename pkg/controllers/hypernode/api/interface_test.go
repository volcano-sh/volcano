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

package api

import (
	"testing"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	fakevcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
)

type legacyTestDiscoverer struct{}

func (*legacyTestDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	return make(chan []*topologyv1alpha1.HyperNode), nil
}
func (*legacyTestDiscoverer) Stop() error   { return nil }
func (*legacyTestDiscoverer) Name() string  { return "legacy-test" }
func (*legacyTestDiscoverer) ResultSynced() {}

func TestLegacyDiscovererRegistration(t *testing.T) {
	const source = "legacy-registration-test"
	constructorCalled := false
	RegisterDiscoverer(source, func(cfg DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) Discoverer {
		constructorCalled = kubeClient != nil && vcClient != nil
		return &legacyTestDiscoverer{}
	})

	discoverer, err := NewDiscoverer(
		DiscoveryConfig{Source: source},
		fake.NewSimpleClientset(),
		fakevcclientset.NewSimpleClientset(),
	)
	if err != nil {
		t.Fatalf("NewDiscoverer() returned an error: %v", err)
	}
	if !constructorCalled || discoverer == nil {
		t.Fatal("legacy discoverer constructor was not called with clients")
	}
}
