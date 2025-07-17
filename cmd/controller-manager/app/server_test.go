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

package app

import (
	"sort"
	"testing"

	"volcano.sh/volcano/pkg/controllers/framework"
	_ "volcano.sh/volcano/pkg/controllers/garbagecollector"
	_ "volcano.sh/volcano/pkg/controllers/job"
	_ "volcano.sh/volcano/pkg/controllers/jobflow"
	_ "volcano.sh/volcano/pkg/controllers/jobtemplate"
	_ "volcano.sh/volcano/pkg/controllers/podgroup"
	_ "volcano.sh/volcano/pkg/controllers/queue"
)

func TestIsControllerEnabled(t *testing.T) {
	var knownControllers = func() []string {
		controllerNames := []string{}
		fn := func(controller framework.Controller) {
			controllerNames = append(controllerNames, controller.Name())
		}
		framework.ForeachController(fn)
		sort.Strings(controllerNames)
		return controllerNames
	}
	testCases := []struct {
		name              string
		gotControllerName string
		inputControllers  []string
		isEnable          bool
	}{
		{
			name:              "all controller should be enable",
			gotControllerName: "job-controller",
			inputControllers:  []string{"*"},
			isEnable:          true,
		},
		{
			name:              "gc-controller should be disable, input allow jobtemplate-controller, jobflow-controller, pg-controller, queue-controller",
			gotControllerName: "gc-controller",
			inputControllers:  []string{"-gc-controller", "+jobtemplate-controller", "+jobflow-controller", "+pg-controller", "+queue-controller"},
			isEnable:          false,
		},
		{
			name:              "job-controller should be enable, input controller is all known controllers",
			gotControllerName: "job-controller",
			inputControllers:  knownControllers(),
			isEnable:          true,
		},
		{
			name:              "job-controller is not in inputControllers, job-controller should be disable",
			gotControllerName: "job-controller",
			inputControllers:  []string{"+gc-controller", "+jobtemplate-controller", "+jobflow-controller"},
			isEnable:          false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isControllerEnabled(tc.gotControllerName, tc.inputControllers)
			if result != tc.isEnable {
				t.Errorf("Expected %s to be enabled: %v, but got: %v", tc.gotControllerName, tc.isEnable, result)
			}
		})
	}
}
