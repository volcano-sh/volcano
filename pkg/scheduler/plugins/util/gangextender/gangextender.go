/*
 Copyright 2023 The Volcano Authors.
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

package gangextender

import (
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	sparkPodLabelRoleKey         = "spark-role"
	sparkPodLabelRoleDriverValue = "driver"
)

func GangPreemptRejectExtender(preemptee *api.TaskInfo) bool {
	sparkReject := sparkGangExtender(preemptee)
	return sparkReject
}

func sparkGangExtender(preemptee *api.TaskInfo) bool {
	if value, ok := preemptee.Pod.Labels[sparkPodLabelRoleKey]; ok {
		if value == sparkPodLabelRoleDriverValue {
			klog.V(4).Infof("Failed to preempt task <%v/%v> because this task is a driver of spark job",
				preemptee.Namespace, preemptee.Name)
			return true
		} else {
			return false
		}
	}
	return false
}
