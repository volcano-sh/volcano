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

package provider

import (
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	topologyapplyv1alpha1 "volcano.sh/apis/pkg/client/applyconfiguration/topology/v1alpha1"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"
)

type PluginBuilder = func() Plugin

// EventType defines the event type.
type EventType string

const (
	// EventAdd is the event type for adding hyperNode.
	EventAdd EventType = "Add"
	// EventUpdate is the event type for updating hyperNode.
	EventUpdate EventType = "Update"
	// EventDelete is the event type for deleting hyperNode.
	EventDelete EventType = "Delete"
)

// Event defines the event from hyperNode provider.
type Event struct {
	// Type is the event type.
	Type EventType
	// HyperNodeName is the name of the hyperNode event.
	HyperNodeName string
	// HyperNode is the hyperNode object to add.
	HyperNode topologyv1alpha1.HyperNode
	// Patch is the hyperNode apply configuration, only used for update event.
	Patch topologyapplyv1alpha1.HyperNodeApplyConfiguration
}

// Reply is the reply message to hyperNode provider when processed hyperNode event failed,
// and vendor should be aware of that and retry or som.
type Reply struct {
	// HyperNodeName is the name of the hyperNode.
	HyperNodeName string
	// Error is the error message of hyperNode event processing.
	Error error
}

// Plugin is the interface for the hyperNode provider, vendors should implement this
// and hyperNode controller call the plugin to populate hyperNodes.
type Plugin interface {
	// Name is the name of the plugin.
	Name() string
	// Start starts the plugin.
	Start(eventCh chan<- Event, replyCh <-chan Reply, informer topologyinformerv1alpha1.HyperNodeInformer) error
	// Stop stops the plugin.
	Stop() error
}
