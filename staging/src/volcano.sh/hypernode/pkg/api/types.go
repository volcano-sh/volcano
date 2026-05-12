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

package api

import (
	"time"
)

const (
	NetworkTopologySourceLabelKey = "volcano.sh/network-topology-source"
	DefaultDiscoveryInterval      = time.Hour
)

// NetworkTopologyConfig represents the configuration for network topology
type NetworkTopologyConfig struct {
	// NetworkTopologyDiscovery specifies the network topology to discover,
	// each discovery source has its own specific configuration
	NetworkTopologyDiscovery []DiscoveryConfig `json:"networkTopologyDiscovery" yaml:"networkTopologyDiscovery"`
}

// SecretRef refers to a secret containing sensitive information
type SecretRef struct {
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace" yaml:"namespace"`
}

// Credentials specifies how to retrieve credentials
type Credentials struct {
	SecretRef *SecretRef `json:"secretRef" yaml:"secretRef"`
}

// DiscoveryConfig contains configuration for a specific discovery source
type DiscoveryConfig struct {
	// Source specifies the discover source (e.g., "ufm", "roce", etc.)
	Source string `json:"source" yaml:"source"`

	// Enabled determines if discovery for this source is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Interval is the period between topology discovery operations
	// If not specified, DefaultDiscoveryInterval will be used
	Interval time.Duration `json:"interval" yaml:"interval"`

	// Credentials specifies the username/password to access the discovery source
	Credentials *Credentials `json:"credentials" yaml:"credentials"`

	// Config contains specific configuration parameters for each discovery source
	Config map[string]interface{} `json:"config" yaml:"config"`
}

// GetDiscoveryConfig returns the configuration for a specific discovery source
// Returns nil if the discovery source is not found or not enabled
func (c *NetworkTopologyConfig) GetDiscoveryConfig(source string) *DiscoveryConfig {
	for i := range c.NetworkTopologyDiscovery {
		if c.NetworkTopologyDiscovery[i].Source == source && c.NetworkTopologyDiscovery[i].Enabled {
			// Create a copy to avoid modifying original data
			config := c.NetworkTopologyDiscovery[i]
			if config.Interval <= 0 {
				config.Interval = DefaultDiscoveryInterval
			}
			return &config
		}
	}
	return nil
}

// GetEnabledDiscoverySources returns a list of enabled discovery sources
func (c *NetworkTopologyConfig) GetEnabledDiscoverySources() []string {
	sources := make([]string, 0, len(c.NetworkTopologyDiscovery))
	for _, dc := range c.NetworkTopologyDiscovery {
		if dc.Enabled {
			sources = append(sources, dc.Source)
		}
	}
	return sources
}
