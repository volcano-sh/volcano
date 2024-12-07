/*
Copyright 2024 The Volcano Authors.

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

package features

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/config/api"
	agentutils "volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/agent/utils/version"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

var (
	// DefaultFeatureGate is a shared global FeatureGate.
	DefaultFeatureGate FeatureGate = &featureGate{}
)

// FeatureGate indicates whether a given feature is enabled or not
type FeatureGate interface {
	// Enabled returns true if the key is enabled.
	Enabled(key Feature, c *api.ColocationConfig) (bool, error)
	// Supported returns nil if the key is supported, return error with the reason why not supported
	Supported(key Feature, cfg *config.Configuration) error
}

type featureGate struct{}

func (f *featureGate) Enabled(key Feature, c *api.ColocationConfig) (bool, error) {
	if c.NodeLabelConfig == nil {
		return false, fmt.Errorf("nil node label config")
	}
	nodeColocationEnabled := c.NodeLabelConfig.NodeColocationEnable != nil && *c.NodeLabelConfig.NodeColocationEnable
	nodeOverSubscriptionEnabled := c.NodeLabelConfig.NodeOverSubscriptionEnable != nil && *c.NodeLabelConfig.NodeOverSubscriptionEnable

	switch key {
	case CPUQoSFeature:
		if c.CPUQosConfig == nil || c.CPUQosConfig.Enable == nil {
			return false, fmt.Errorf("nil cpu qos config")
		}
		return (nodeColocationEnabled || nodeOverSubscriptionEnabled) && *c.CPUQosConfig.Enable, nil

	case CPUBurstFeature:
		if c.CPUBurstConfig == nil || c.CPUBurstConfig.Enable == nil {
			return false, fmt.Errorf("nil cpu burst config")
		}
		return (nodeColocationEnabled || nodeOverSubscriptionEnabled) && *c.CPUBurstConfig.Enable, nil

	case MemoryQoSFeature:
		if c.MemoryQosConfig == nil || c.MemoryQosConfig.Enable == nil {
			return false, fmt.Errorf("nil memory qos config")
		}
		return (nodeColocationEnabled || nodeOverSubscriptionEnabled) && *c.MemoryQosConfig.Enable, nil

	case NetworkQoSFeature:
		if c.NetworkQosConfig == nil || c.NetworkQosConfig.Enable == nil {
			return false, fmt.Errorf("nil memory qos config")
		}
		return (nodeColocationEnabled || nodeOverSubscriptionEnabled) && *c.NetworkQosConfig.Enable, nil

	case OverSubscriptionFeature:
		if c.OverSubscriptionConfig == nil || c.OverSubscriptionConfig.Enable == nil {
			return false, fmt.Errorf("nil overSubscription config")
		}
		return nodeOverSubscriptionEnabled && *c.OverSubscriptionConfig.Enable, nil
	case EvictionFeature, ResourcesFeature:
		// Always return true because eviction manager need take care of all nodes.
		return true, nil
	default:
		return false, fmt.Errorf("unsupported feature %s", string(key))
	}
}

type UnsupportedError string

func (s UnsupportedError) Error() string {
	return "unsupported feature: " + string(s)
}

func IsUnsupportedError(err error) bool {
	_, ok := err.(UnsupportedError)
	return ok
}

func (f *featureGate) Supported(feature Feature, cfg *config.Configuration) error {
	if !cfg.IsFeatureSupported(string(feature)) {
		return UnsupportedError(fmt.Sprintf("feature(%s) is not supprted by volcano-agent", string(feature)))
	}

	switch feature {
	case NetworkQoSFeature:
		if err := CheckNodeSupportNetworkQoS(); err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func CheckNodeSupportNetworkQoS() error {
	return checkNodeOSSupportNetworkQoS()
}

func checkNodeOSSupportNetworkQoS() error {
	osReleaseFile := strings.TrimSpace(os.Getenv(utils.HostOSReleasePathEnv))
	if osReleaseFile == "" {
		osReleaseFile = utils.DefaultNodeOSReleasePath
	}
	osRelease, err := agentutils.GetOSReleaseFromFile(osReleaseFile)
	if err != nil {
		return err
	}
	if osRelease.Name == utils.OpenEulerOSReleaseName && version.HigherOrEqual(osRelease.Version, utils.OpenEulerOSReleaseVersion) {
		return nil
	}
	klog.V(4).InfoS("os does not support network qos", "os-name", osRelease.Name, "os-version", osRelease.Version)
	return UnsupportedError(fmt.Sprintf("os(%s:%s) does not support network qos", osRelease.Name, osRelease.Version))
}
