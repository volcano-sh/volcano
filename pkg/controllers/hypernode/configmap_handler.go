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

package hypernode

import (
	"fmt"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/hypernode/config"
)

func (hn *hyperNodeController) setConfigMapNamespaceAndName() {
	namespace := os.Getenv(config.NamespaceEnvKey)
	if namespace == "" {
		namespace = config.DefaultNamespace
	}
	releaseName := os.Getenv(config.ReleaseNameEnvKey)
	if releaseName == "" {
		releaseName = config.DefaultReleaseName
	}
	hn.configMapNamespace = namespace
	hn.configMapName = releaseName + "-controller-configmap"
}

func (hn *hyperNodeController) setupConfigMapInformer() {
	// Only list/watch one ConfigMap
	filteredInformer := coreinformers.NewFilteredConfigMapInformer(
		hn.kubeClient,
		hn.configMapNamespace,
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		func(options *metav1.ListOptions) {
			options.FieldSelector = fmt.Sprintf("metadata.name=%s", hn.configMapName)
		},
	)
	hn.informerFactory.InformerFor(&v1.ConfigMap{}, func(kubernetes.Interface, time.Duration) cache.SharedIndexInformer {
		return filteredInformer
	})
	hn.configMapInformer = hn.informerFactory.Core().V1().ConfigMaps()
	// TODO: Only trigger handler when networkTopologyDiscovery config changed.
	hn.configMapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    hn.addConfigMap,
		UpdateFunc: hn.updateConfigMap,
		DeleteFunc: hn.deleteConfigMap,
	})
}

func (hn *hyperNodeController) addConfigMap(obj interface{}) {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		klog.ErrorS(nil, "Cannot convert to *v1.ConfigMap", "obj", obj)
		return
	}
	klog.V(3).InfoS("Add ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
	hn.enqueueConfigMap(cm)
}

func (hn *hyperNodeController) updateConfigMap(oldObj, newObj interface{}) {
	cm, ok := newObj.(*v1.ConfigMap)
	if !ok {
		klog.ErrorS(nil, "Cannot convert to *v1.ConfigMap", "obj", newObj)
		return
	}
	klog.V(3).InfoS("Update ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
	hn.enqueueConfigMap(cm)
}

func (hn *hyperNodeController) deleteConfigMap(obj interface{}) {
	cm, ok := obj.(*v1.ConfigMap)
	if !ok {
		tombstone, isTombstone := obj.(cache.DeletedFinalStateUnknown)
		if !isTombstone {
			klog.ErrorS(nil, "Cannot convert to *v1.ConfigMap", "obj", obj)
			return
		}
		cm, ok = tombstone.Obj.(*v1.ConfigMap)
		if !ok {
			klog.ErrorS(nil, "Cannot convert tombstone to *v1.ConfigMap", "obj", tombstone.Obj)
			return
		}
	}
	klog.V(3).InfoS("Delete ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
	hn.enqueueConfigMap(cm)
}

func (hn *hyperNodeController) enqueueConfigMap(cm *v1.ConfigMap) {
	key, err := cache.MetaNamespaceKeyFunc(cm)
	if err != nil {
		klog.ErrorS(err, "Failed to get key for ConfigMap", "namespace", cm.Namespace, "name", cm.Name)
		return
	}
	hn.configMapQueue.Add(key)
}
