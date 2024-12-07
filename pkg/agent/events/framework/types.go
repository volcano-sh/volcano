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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type EventName string

const (
	PodEventName EventName = "PodsSync"

	NodeResourcesEventName EventName = "NodeResourcesSync"

	NodeMonitorEventName EventName = "NodeUtilizationSync"
)

type PodEvent struct {
	UID      types.UID
	QoSLevel int64
	QoSClass corev1.PodQOSClass
	Pod      *corev1.Pod
}

// NodeResourceEvent defines node resource event, overSubscription resource recently.
type NodeResourceEvent struct {
	MillCPU     int64
	MemoryBytes int64
}

// NodeMonitorEvent defines node pressure event.
type NodeMonitorEvent struct {
	// TimeStamp is the time when event occur.
	TimeStamp time.Time
	// Resource represents which resource is under pressure.
	Resource corev1.ResourceName
}
