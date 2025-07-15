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

package fake

import (
	"sync"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

type fakeDiscoverer struct {
	startCalled bool
	stopCalled  bool
	hyperNodes  []*topologyv1alpha1.HyperNode
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

func (f *fakeDiscoverer) Name() string {
	return "FakeDiscoverer"
}

func NewFakeDiscoverer(hyperNodes []*topologyv1alpha1.HyperNode, cfg api.DiscoveryConfig) api.Discoverer {
	return &fakeDiscoverer{
		hyperNodes: hyperNodes,
		stopCh:     make(chan struct{}),
	}
}

func (f *fakeDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	f.startCalled = true
	f.wg.Add(1)

	ch := make(chan []*topologyv1alpha1.HyperNode)
	go func() {
		defer f.wg.Done()
		select {
		case ch <- f.hyperNodes:
		case <-f.stopCh:
			return
		}
	}()

	return ch, nil
}

func (f *fakeDiscoverer) Stop() error {
	f.stopCalled = true
	close(f.stopCh)
	f.wg.Wait()
	return nil
}
