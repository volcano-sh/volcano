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

package label

import (
	clientset "k8s.io/client-go/kubernetes"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

func init() {
	api.RegisterDiscoverer("label", NewLabelDiscoverer)
}

type labelDiscoverer struct {
	config api.DiscoveryConfig
}

func (l labelDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	//TODO implement me
	panic("implement me")
}

func (l labelDiscoverer) Stop() error {
	//TODO implement me
	panic("implement me")
}

func (l labelDiscoverer) Name() string {
	return "label"
}

func NewLabelDiscoverer(cfg api.DiscoveryConfig, kubeClient clientset.Interface) api.Discoverer {
	return &labelDiscoverer{
		config: cfg,
	}
}
