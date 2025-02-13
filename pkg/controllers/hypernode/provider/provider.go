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
	"context"
	"fmt"
	"path/filepath"
	"plugin"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
)

// Provider is the interface for the hyperNode provider.
type Provider interface {
	Provision(stopCh <-chan struct{})
}

var backoff = wait.Backoff{
	Duration: time.Second,
	Factor:   1,
	Jitter:   0.1,
	Steps:    20,
}

type provider struct {
	eventCh        chan Event
	replyCh        chan Reply
	vcClient       vcclientset.Interface
	factory        vcinformer.SharedInformerFactory
	pluginBuilders map[string]PluginBuilder
	pluginDir      string
}

// NewProvider creates a new hyperNode provider.
func NewProvider(client vcclientset.Interface, factory vcinformer.SharedInformerFactory, pluginDir string) Provider {
	p := &provider{
		vcClient:       client,
		factory:        factory,
		eventCh:        make(chan Event),
		replyCh:        make(chan Reply),
		pluginBuilders: make(map[string]PluginBuilder),
		pluginDir:      pluginDir,
	}
	if err := p.loadProvider(pluginDir); err != nil {
		klog.ErrorS(err, "Failed to load hyperNode provider")
	}
	return p
}

func (p *provider) loadProvider(dir string) error {
	pluginPaths, _ := filepath.Glob(fmt.Sprintf("%s/*.so", dir))
	for _, pluginPath := range pluginPaths {
		klog.InfoS("Loading provider plugin...", "", "path", pluginPath)
		pb, err := loadPlugin(pluginPath)
		if err != nil {
			return err
		}
		plugin := pb()
		//pluginName := getPluginName(pluginPath)
		p.RegisterPlugin(plugin.Name(), pb)
		klog.V(2).Infof("hyperNode provider %s loaded", plugin.Name())
	}

	return nil
}

func loadPlugin(pluginPath string) (PluginBuilder, error) {
	plug, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin, err: %v, path: %v", err, pluginPath)
	}

	symBuilder, err := plug.Lookup("New")
	if err != nil {
		return nil, fmt.Errorf("failed to look up New method: %v", err)
	}

	builder, ok := symBuilder.(PluginBuilder)
	if !ok {
		return nil, fmt.Errorf("unexpected plugin: %s, failed to convert Plugin", pluginPath)
	}

	return builder, nil
}

// RegisterPlugin registers a plugin as part of the provider.
func (p *provider) RegisterPlugin(name string, pb PluginBuilder) {
	if _, ok := p.pluginBuilders[name]; ok {
		klog.ErrorS(nil, "Plugin already registered", "name", name)
		return
	}
	p.pluginBuilders[name] = pb
	klog.InfoS("Successfully registered plugin", "name", name)
}

// Provision starts the hyperNode provider.
func (p *provider) Provision(stopCh <-chan struct{}) {
	for _, pb := range p.pluginBuilders {
		plugin := pb()
		if err := plugin.Start(p.eventCh, p.replyCh, p.factory.Topology().V1alpha1().HyperNodes()); err != nil {
			klog.ErrorS(err, "Failed to load plugin", "name", plugin.Name())
			return
		} else {
			klog.InfoS("Successfully loaded plugin", "name", plugin.Name())
		}
	}

	go p.handleEvents(stopCh)

	<-stopCh
	p.stop()
}

func (p *provider) stop() {
	for _, pb := range p.pluginBuilders {
		plugin := pb()
		if err := plugin.Stop(); err != nil {
			klog.ErrorS(err, "Failed to stop plugin", "name", plugin.Name())
		}
	}
	close(p.replyCh)
}

func (p *provider) handleEvents(stop <-chan struct{}) {
	for {
		select {
		case event := <-p.eventCh:
			name := event.HyperNodeName
			klog.InfoS("Received event", "type", event.Type, "name", name)
			switch event.Type {
			case EventAdd:
				klog.InfoS("Handling hyperNode add event", "name", name)
				go p.handleHyperNodeAdd(event)
			case EventUpdate:
				klog.InfoS("Handling hyperNode update event", "name", name)
				go p.handleHyperNodeUpdate(event)
			case EventDelete:
				klog.InfoS("Handling hyperNode delete event", "name", name)
				go p.handleNodeDeleted(event)
			default:
				klog.ErrorS(nil, "Unknown event type", "type", event.Type)
			}
		case <-stop:
			return
		}
	}
}

func (p *provider) handleHyperNodeAdd(event Event) {
	hyperNode := event.HyperNode
	err := retry.OnError(
		backoff,
		func(err error) bool {
			return !apierrors.IsAlreadyExists(err)
		},
		func() error {
			_, err := p.vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), &hyperNode, metav1.CreateOptions{})
			return err
		},
	)

	if err != nil {
		klog.ErrorS(err, "Failed to add HyperNode after retries", "name", hyperNode.Name)
		p.replyCh <- Reply{
			HyperNodeName: hyperNode.Name,
			Error:         err,
		}
		return
	}
	klog.InfoS("Successfully added hyperNode", "name", hyperNode.Name)
}

func (p *provider) handleHyperNodeUpdate(event Event) {
	hyperNode := event.HyperNode
	err := retry.OnError(
		backoff,
		func(err error) bool {
			return true
		},
		func() error {
			_, err := p.vcClient.TopologyV1alpha1().HyperNodes().ApplyStatus(context.Background(), &event.Patch, metav1.ApplyOptions{})
			return err
		},
	)

	if err != nil {
		klog.ErrorS(err, "Failed to update HyperNode after retries", "name", hyperNode.Name)
		p.replyCh <- Reply{
			HyperNodeName: hyperNode.Name,
			Error:         err,
		}
		return
	}
	klog.InfoS("Successfully updated hyperNode", "name", hyperNode.Name)
}

func (p *provider) handleNodeDeleted(event Event) {
	name := event.HyperNodeName
	err := retry.OnError(
		backoff,
		func(err error) bool {
			return !apierrors.IsNotFound(err)
		},
		func() error {
			err := p.vcClient.TopologyV1alpha1().HyperNodes().Delete(context.Background(), name, metav1.DeleteOptions{})
			return err
		},
	)

	if err != nil {
		klog.ErrorS(err, "Failed to delete HyperNode after retries", "name", name)
		p.replyCh <- Reply{
			HyperNodeName: name,
			Error:         err,
		}
		return
	}
	klog.InfoS("Successfully deleted HyperNode", "name", name)
}
