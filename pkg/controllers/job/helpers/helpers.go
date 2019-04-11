/*
Copyright 2019 The Kubernetes Authors.

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

package helpers

import (
	"strings"

	"k8s.io/api/core/v1"
)

const (
	// TaskNameFmt gives the format of task name
	TaskNameFmt = "%s-%s-%d"
)

// GetTaskIndex returns tasks index
func GetTaskIndex(pod *v1.Pod) string {
	num := strings.Split(pod.Name, "-")
	if len(num) >= 3 {
		return num[len(num)-1]
	}

	return ""
}
