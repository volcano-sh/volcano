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

package main

import (
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/topology/v1alpha1"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"

	"volcano.sh/volcano/pkg/controllers/hypernode/provider"
)

func New() provider.Plugin {
	return &exampleProvider{
		stopChan: make(chan struct{}),
	}
}

// exampleProvider is an example provider of hyperNodes.
type exampleProvider struct {
	stopChan          chan struct{}
	hyperNodeInformer topologyinformerv1alpha1.HyperNodeInformer
}

// Name returns the name of the vendor.
func (e *exampleProvider) Name() string {
	return "example-provider"
}

// Start starts the vendor provider.
func (e *exampleProvider) Start(eventCh chan<- provider.Event, replyCh <-chan provider.Reply, informer topologyinformerv1alpha1.HyperNodeInformer) error {
	e.hyperNodeInformer = informer
	go e.receiveReply(replyCh)
	go e.sendEvent(eventCh)
	return nil
}

// Stop stops the provider.
func (e *exampleProvider) Stop() error {
	klog.InfoS("exampleProvider stopped")
	close(e.stopChan)
	return nil
}

func (e *exampleProvider) sendEvent(eventCh chan<- provider.Event) {
	i := 0
	for {
		select {
		case <-e.stopChan:
			klog.InfoS("Stop signal received, exiting sendEvent")
			return
		default:
			hn := v1alpha1.HyperNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hypernode-" + strconv.Itoa(i),
				},
				Spec: v1alpha1.HyperNodeSpec{
					Tier: 1,
					Members: []v1alpha1.MemberSpec{
						{
							Type: v1alpha1.MemberTypeNode,
							Selector: v1alpha1.MemberSelector{
								ExactMatch: &v1alpha1.ExactMatch{
									Name: "node-" + strconv.Itoa(i),
								},
							},
						},
					},
				},
			}
			event := provider.Event{Type: provider.EventAdd, HyperNodeName: hn.Name, HyperNode: hn}
			eventCh <- event
			klog.InfoS("Successfully sent add event", "event", event.Type, "hyperNodeName", hn.Name)
			time.Sleep(5 * time.Second) // analog sending interval
			i++
			if i == 3 {
				return
			}
		}
	}
}

func (e *exampleProvider) receiveReply(replyCh <-chan provider.Reply) {
	for {
		select {
		case reply, ok := <-replyCh:
			if !ok {
				klog.InfoS("Reply channel closed, exiting receiveReply")
				return
			}
			klog.ErrorS(reply.Error, "Failed to process hyperNode event", "hyperNodeName", reply.HyperNodeName)
		}
	}
}
