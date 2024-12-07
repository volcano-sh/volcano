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

package base

import (
	"errors"
	"sync"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/features"
	"volcano.sh/volcano/pkg/config"
)

type BaseHandle struct {
	Name   string
	Lock   sync.RWMutex
	Config *config.Configuration
	Active bool
}

func (h *BaseHandle) HandleName() string {
	return h.Name
}

func (h *BaseHandle) IsActive() bool {
	h.Lock.Lock()
	defer h.Lock.Unlock()
	return h.Active
}

func (h *BaseHandle) Handle(event interface{}) error {
	return errors.New("unimplemented")
}

func (h *BaseHandle) RefreshCfg(cfg *api.ColocationConfig) error {
	h.Lock.Lock()
	defer h.Lock.Unlock()

	isActive, err := features.DefaultFeatureGate.Enabled(features.Feature(h.HandleName()), cfg)
	if err != nil {
		return err
	}

	if isActive {
		if supportErr := features.DefaultFeatureGate.Supported(features.Feature(h.HandleName()), h.Config); supportErr != nil {
			return supportErr
		}
	}

	if h.Active != isActive {
		klog.InfoS("Event handler config changes", "handle", h.HandleName(), "nodeColocation", *cfg.NodeLabelConfig.NodeColocationEnable,
			"nodeOverSubscription", *cfg.NodeLabelConfig.NodeOverSubscriptionEnable, "old", h.Active, "new", isActive)
	}
	h.Active = isActive
	return nil
}
