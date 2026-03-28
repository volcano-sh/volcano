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

// Package hypernode registers the HyperNode controller implementation from
// volcano.sh/hypernode with the Volcano controller-manager.
package hypernode

import (
	hn "volcano.sh/hypernode/pkg/hypernode"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func init() {
	_ = framework.RegisterController(&adapter{c: hn.NewController()})
}

type adapter struct {
	c *hn.Controller
}

func (a *adapter) Name() string {
	return a.c.Name()
}

func (a *adapter) Initialize(opt *framework.ControllerOption) error {
	return a.c.Initialize(&hn.Options{
		KubeClient:              opt.KubeClient,
		VolcanoClient:           opt.VolcanoClient,
		SharedInformerFactory:   opt.SharedInformerFactory,
		VCSharedInformerFactory: opt.VCSharedInformerFactory,
	})
}

func (a *adapter) Run(stopCh <-chan struct{}) {
	a.c.Run(stopCh)
}
