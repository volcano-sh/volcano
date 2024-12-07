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

package framework

import (
	"volcano.sh/volcano/pkg/agent/config/api"
)

type Probe interface {
	// ProbeName returns name of the probe
	ProbeName() string
	// Run runs the probe
	Run(stop <-chan struct{})
	// RefreshCfg hot update probe's cfg.
	RefreshCfg(cfg *api.ColocationConfig) error
}

type Handle interface {
	// HandleName returns name of the handler
	HandleName() string
	// Handle handles the given event
	// Return an error only if the event needs to be re-enqueued to be processed
	// Need to avoid returning errors that cannot be resolved by retrying
	Handle(event interface{}) error
	// IsActive returns true if the handler is enabled
	IsActive() bool
	// RefreshCfg hot update handler's cfg.
	RefreshCfg(cfg *api.ColocationConfig) error
}
