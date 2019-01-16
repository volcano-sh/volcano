/*
Copyright 2019 The Volcano Authors.

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
	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

func parsePolicies(req *Request) map[vkv1.Event]vkv1.Action {
	actions := map[vkv1.Event]vkv1.Action{}

	// Set Job level policies
	for _, policy := range req.Job.Spec.Policies {
		actions[policy.Event] = policy.Action
	}

	// TODO(k82cn): set task level polices

	// Set default action
	actions[vkv1.OutOfSyncEvent] = vkv1.SyncJobAction

	// Set command action
	if len(req.Action) != 0 {
		actions[req.Event] = req.Action
	}

	return actions
}
