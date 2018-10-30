/*
Copyright 2017 The Kubernetes Authors.

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

package scheduler

import (
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/actions/allocate"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/actions/decorate"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/actions/preempt"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/plugins/drf"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/plugins/gang"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/plugins/priority"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"
)

func init() {
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder("gang", gang.New)
	framework.RegisterPluginBuilder("drf", drf.New)

	framework.RegisterAction(decorate.New())
	framework.RegisterAction(allocate.New())
	framework.RegisterAction(preempt.New())
}

// TODO (k82cn): make Actions configurable
var actionChain = []framework.Action{
	decorate.New(),
	allocate.New(),
	preempt.New(),
}
