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
