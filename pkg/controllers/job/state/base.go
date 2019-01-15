/*
Copyright 2017 The Volcano Authors.

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

package state

import (
	"github.com/golang/glog"

	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

type baseState struct {
	request  *Request
	policies map[vkv1.Event]vkv1.Action
}

func (ps *baseState) Execute() error {
	action := ps.policies[ps.request.Event]
	glog.V(3).Infof("The action for event <%s> is <%s>",
		ps.request.Event, action)
	switch action {
	case vkv1.RestartJobAction:
		fn := actionFns[action]
		return fn(ps.request)
	default:
		fn := actionFns[action]
		return fn(ps.request)
	}
}
