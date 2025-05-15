package config

import (
	"sync"

	"volcano.sh/volcano/pkg/controllers/hypernode/api"
)

type FakeLoader struct {
	mu     sync.Mutex
	Config *api.NetworkTopologyConfig
}

func (m *FakeLoader) LoadConfig() (*api.NetworkTopologyConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.Config, nil
}

func (m *FakeLoader) SetConfig(cfg *api.NetworkTopologyConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Config = cfg
}
func NewFakeLoader(cfg *api.NetworkTopologyConfig) *FakeLoader {
	return &FakeLoader{
		Config: cfg,
	}
}
