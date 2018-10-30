/*
Copyright 2018 The Kubernetes Authors.

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

package preempt

import (
	"testing"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/plugins/drf"
)

func TestPreempt(t *testing.T) {
	framework.RegisterPluginBuilder("drf", drf.New)
	defer framework.CleanupPluginBuilders()

	// TODO (k82cn): Add UT cases here.
}
