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

package source

import (
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/config/utils"
)

type configMapSource struct {
	kubeClient            clientset.Interface
	informerFactory       informers.SharedInformerFactory
	configMapListerSynced cache.InformerSynced
	nodeListerSynced      cache.InformerSynced
	queue                 workqueue.RateLimitingInterface
	nodeName              string
	configmapNamespace    string
	configmapName         string
}

func NewConfigMapSource(client clientset.Interface, nodeName, namespace string) ConfigEventSource {
	tweakListOptions := func(options *metav1.ListOptions) {
		options.FieldSelector = fields.OneTermEqualSelector(utils.ObjectNameField, utils.ConfigMapName).String()
	}
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	configMapInformer := informerFactory.InformerFor(&corev1.ConfigMap{},
		func(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
			return coreinformers.NewFilteredConfigMapInformer(client, namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, tweakListOptions)
		})
	nodeInformer := informerFactory.InformerFor(&corev1.Node{},
		func(client clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
			tweakListOptions := func(options *metav1.ListOptions) {
				options.FieldSelector = fields.OneTermEqualSelector(metav1.ObjectNameField, nodeName).String()
			}
			return coreinformers.NewFilteredNodeInformer(client, resyncPeriod, cache.Indexers{}, tweakListOptions)
		})
	cs := &configMapSource{
		informerFactory:       informerFactory,
		configMapListerSynced: configMapInformer.HasSynced,
		nodeListerSynced:      nodeInformer.HasSynced,
		kubeClient:            client,
		queue: workqueue.NewNamedRateLimitingQueue(workqueue.NewMaxOfRateLimiter(
			workqueue.NewItemExponentialFailureRateLimiter(500*time.Millisecond, 1000*time.Second),
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)}), "configmap-resource"),
		nodeName:           nodeName,
		configmapNamespace: namespace,
		configmapName:      utils.ConfigMapName,
	}
	configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cs.queue.Add("configmap") },
		UpdateFunc: func(oldObj, newObj interface{}) { cs.queue.Add("configmap") },
	})
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { cs.nodeAdd(obj) },
		UpdateFunc: func(oldObj, newObj interface{}) { cs.nodeUpdate(oldObj, newObj) },
	})

	return cs
}

func (cs *configMapSource) nodeAdd(obj interface{}) {
	cs.queue.Add("node")
}

func (cs *configMapSource) nodeUpdate(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*corev1.Node)
	if !ok {
		klog.ErrorS(nil, "can not convert interface object to Node", "oldobj", oldObj)
		cs.queue.Add("node")
		return
	}

	newNode, ok := newObj.(*corev1.Node)
	if !ok {
		klog.ErrorS(nil, "can not convert interface object to Node", "newobj", newObj)
		cs.queue.Add("node")
		return
	}

	if oldNode.Labels[apis.ColocationEnableNodeLabelKey] == newNode.Labels[apis.ColocationEnableNodeLabelKey] &&
		oldNode.Labels[apis.OverSubscriptionNodeLabelKey] == newNode.Labels[apis.OverSubscriptionNodeLabelKey] &&
		oldNode.Labels[apis.ColocationPolicyKey] == newNode.Labels[apis.ColocationPolicyKey] {
		return
	}
	cs.queue.Add("node")
}

func (cs *configMapSource) GetLatestConfig() (cfg *api.ColocationConfig, err error) {
	node, err := cs.informerFactory.Core().V1().Nodes().Lister().Get(cs.nodeName)
	if err != nil {
		klog.ErrorS(err, "Failed to get node", "node", cs.nodeName)
		return nil, err
	}

	configmap, err := cs.informerFactory.Core().V1().ConfigMaps().Lister().ConfigMaps(cs.configmapNamespace).Get(cs.configmapName)
	if err != nil {
		klog.ErrorS(err, "Failed to get configmap", "name", cs.configmapName)
		return nil, err
	}

	config := &api.VolcanoAgentConfig{}
	data, ok := configmap.Data[utils.ColocationConfigKey]
	if !ok || len(data) == 0 {
		return nil, fmt.Errorf("configMap data(%s) is empty", utils.ColocationConfigKey)
	}
	if err = json.Unmarshal([]byte(data), config); err != nil {
		return nil, err
	}
	return utils.MergerCfg(config, node)
}

func (cs *configMapSource) Source(stopCh <-chan struct{}) (change workqueue.RateLimitingInterface, err error) {
	cs.informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, cs.configMapListerSynced, cs.nodeListerSynced) {
		return nil, fmt.Errorf("timed out waiting for mapfigmap source caches to sync")
	}

	go func() {
		<-stopCh
		cs.queue.ShutDown()
	}()

	return cs.queue, nil
}
