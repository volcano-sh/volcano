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

package config

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
)

const (
	specNodeNameField      = "spec.nodeName"
	defaultPodReSyncPeriod = 15 * time.Minute
)

type Configuration struct {
	GenericConfiguration *VolcanoAgentConfiguration
	InformerFactory      *InformerFactory
}

func NewConfiguration() *Configuration {
	return &Configuration{
		GenericConfiguration: &VolcanoAgentConfiguration{},
		InformerFactory:      &InformerFactory{},
	}
}

func (c *Configuration) Complete(client clientset.Interface) {
	// only pod needs to be re-synced.
	sharedFactory := informers.NewSharedInformerFactoryWithOptions(client, 0, informers.WithCustomResyncConfig(map[metav1.Object]time.Duration{
		&corev1.Pod{}: defaultPodReSyncPeriod,
	}))
	nodeInformer := sharedFactory.InformerFor(&corev1.Node{},
		func(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
			tweakListOptions := func(options *metav1.ListOptions) {
				options.FieldSelector = fields.OneTermEqualSelector(metav1.ObjectNameField, c.GenericConfiguration.KubeNodeName).String()
			}
			return coreinformers.NewFilteredNodeInformer(client, resyncPeriod, cache.Indexers{}, tweakListOptions)
		})

	podInformer := sharedFactory.InformerFor(&corev1.Pod{},
		func(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
			tweakListOptions := func(options *metav1.ListOptions) {
				options.FieldSelector = fields.OneTermEqualSelector(specNodeNameField, c.GenericConfiguration.KubeNodeName).String()
			}
			return coreinformers.NewFilteredPodInformer(client, metav1.NamespaceAll, resyncPeriod, cache.Indexers{}, tweakListOptions)
		})

	nodeLister := sharedFactory.Core().V1().Nodes().Lister()
	podLister := sharedFactory.Core().V1().Pods().Lister()
	c.GenericConfiguration.NodeLister = nodeLister
	c.GenericConfiguration.PodLister = podLister
	c.GenericConfiguration.NodeHasSynced = func() bool {
		return nodeInformer.HasSynced()
	}
	c.GenericConfiguration.PodsHasSynced = func() bool {
		return podInformer.HasSynced()
	}
	c.InformerFactory.K8SInformerFactory = sharedFactory
}

func (c *Configuration) GetNode() (*corev1.Node, error) {
	// if we have a valid kube client, we wait for initial lister to sync
	if !c.GenericConfiguration.NodeHasSynced() {
		node, err := c.GenericConfiguration.KubeClient.CoreV1().Nodes().Get(context.TODO(), c.GenericConfiguration.KubeNodeName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return node, nil
	}
	return c.GenericConfiguration.NodeLister.Get(c.GenericConfiguration.KubeNodeName)
}

func (c *Configuration) IsFeatureSupported(name string) bool {
	visitStar := false
	for _, feature := range c.GenericConfiguration.SupportedFeatures {
		switch feature {
		case "-" + name:
			return false
		case name:
			return true
		case "*":
			visitStar = true
		}
	}
	return visitStar
}

func (c *Configuration) GetActivePods() ([]*corev1.Pod, error) {
	allPods := []*corev1.Pod{}
	var err error
	if !c.GenericConfiguration.PodsHasSynced() {
		selector := fields.OneTermEqualSelector("spec.nodeName", c.GenericConfiguration.KubeNodeName)
		pods, err := c.GenericConfiguration.KubeClient.CoreV1().Pods(corev1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
			FieldSelector: selector.String(),
			LabelSelector: labels.Everything().String(),
		})
		if err != nil {
			return nil, err
		}
		for i := range pods.Items {
			allPods = append(allPods, &pods.Items[i])
		}
	} else {
		allPods, err = c.InformerFactory.K8SInformerFactory.Core().V1().Pods().Lister().List(labels.Everything())
		if err != nil {
			return nil, err
		}
	}

	// Filter out terminated pods.
	rPods := []*corev1.Pod{}
	for i := range allPods {
		if utilpod.IsPodTerminated(allPods[i]) {
			continue
		}
		rPods = append(rPods, allPods[i])
	}
	return rPods, nil
}
