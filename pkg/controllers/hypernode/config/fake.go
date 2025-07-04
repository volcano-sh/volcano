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
