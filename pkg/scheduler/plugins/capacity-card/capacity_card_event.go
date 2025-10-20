/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typecorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

var (
	eventRecorder     record.EventRecorder
	eventRecorderOnce sync.Once
)

// buildEventRecorder builds the event recorder for plugin.
func (p *Plugin) buildEventRecorder(ssn *framework.Session) {
	eventRecorderOnce.Do(func() {
		eventBroadcaster := record.NewBroadcaster()
		eventBroadcaster.StartStructuredLogging(0)
		eventBroadcaster.StartRecordingToSink(&typecorev1.EventSinkImpl{
			Interface: ssn.KubeClient().CoreV1().Events(""),
		})
		eventRecorder = eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{
			Component: fmt.Sprintf(`volcano.%s`, p.Name()),
		})
	})
}
