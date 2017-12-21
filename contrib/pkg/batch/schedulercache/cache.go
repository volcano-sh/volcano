/*
Copyright 2017 The Kubernetes Authors.

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

package schedulercache

import (
	"fmt"
	"sync"

	"github.com/golang/glog"
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client"
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client/informers"
	qjobclient "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/client/informers/comboset/v1"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// New returns a Cache implementation.
func New(
	config *rest.Config,
) Cache {
	return newSchedulerCache(config)
}

type schedulerCache struct {
	sync.Mutex

	comboSetInformer qjobclient.ComboSetInformer

	combosets map[string]*ComboSetInfo
}

func newSchedulerCache(config *rest.Config) *schedulerCache {
	sc := &schedulerCache{
		combosets: make(map[string]*ComboSetInfo),
	}

	// create queue resource first
	err := createComboSet(config)
	if err != nil {
		panic(err)
	}

	// create queuejob informer
	queuejobClient, _, err := client.NewQueueJobClient(config)
	if err != nil {
		panic(err)
	}

	qjobInformerFactory := qInformerfactory.NewSharedInformerFactory(queuejobClient, 0)

	// create informer for queuejob information
	sc.comboSetInformer = qjobInformerFactory.ComboSet().ComboSets()
	sc.comboSetInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *apiv1.ComboSet:
					glog.V(4).Infof("filter queuejob name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    sc.AddComboSet,
				UpdateFunc: sc.UpdateComboSet,
				DeleteFunc: sc.DeleteComboSet,
			},
		})

	return sc
}

func createComboSet(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateComboSet(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (sc *schedulerCache) Run(stopCh <-chan struct{}) {
	go sc.comboSetInformer.Informer().Run(stopCh)
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) addComboSet(queuejob *apiv1.ComboSet) error {
	if _, ok := sc.combosets[queuejob.Name]; ok {
		return fmt.Errorf("queuejob %v exist", queuejob.Name)
	}

	info := &ComboSetInfo{
		name:     queuejob.Name,
		comboset: queuejob.DeepCopy(),
	}
	sc.combosets[queuejob.Name] = info
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) updateComboSet(oldQueueJob, newQueueJob *apiv1.ComboSet) error {
	if err := sc.deleteComboSet(oldQueueJob); err != nil {
		return err
	}
	sc.addComboSet(newQueueJob)
	return nil
}

// Assumes that lock is already acquired.
func (sc *schedulerCache) deleteComboSet(queuejob *apiv1.ComboSet) error {
	if _, ok := sc.combosets[queuejob.Name]; !ok {
		return fmt.Errorf("queuejob %v doesn't exist", queuejob.Name)
	}
	delete(sc.combosets, queuejob.Name)
	return nil
}

func (sc *schedulerCache) AddComboSet(obj interface{}) {

	queuejob, ok := obj.(*apiv1.ComboSet)
	if !ok {
		glog.Errorf("Cannot convert to *apiv1.QueueJob: %v", obj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Add queuejob(%s) into cache, status(%#v), spec(%#v)\n", queuejob.Name, queuejob.Status, queuejob.Spec)
	err := sc.addComboSet(queuejob)
	if err != nil {
		glog.Errorf("Failed to add queuejob %s into cache: %v", queuejob.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) UpdateComboSet(oldObj, newObj interface{}) {
	oldQueueJob, ok := oldObj.(*apiv1.ComboSet)
	if !ok {
		glog.Errorf("Cannot convert oldObj to *apiv1.QueueJob: %v", oldObj)
		return
	}
	newQueueJob, ok := newObj.(*apiv1.ComboSet)
	if !ok {
		glog.Errorf("Cannot convert newObj to *apiv1.QueueJob: %v", newObj)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	glog.V(4).Infof("Update oldQueueJob(%s) in cache, status(%#v), spec(%#v)", oldQueueJob.Name, oldQueueJob.Status, oldQueueJob.Spec)
	glog.V(4).Infof("Update newQueueJob(%s) in cache, status(%#v), spec(%#v)", newQueueJob.Name, newQueueJob.Status, newQueueJob.Spec)
	err := sc.updateComboSet(oldQueueJob, newQueueJob)
	if err != nil {
		glog.Errorf("Failed to update queuejob %s into cache: %v", oldQueueJob.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) DeleteComboSet(obj interface{}) {
	var queuejob *apiv1.ComboSet
	switch t := obj.(type) {
	case *apiv1.ComboSet:
		queuejob = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		queuejob, ok = t.Obj.(*apiv1.ComboSet)
		if !ok {
			glog.Errorf("Cannot convert to *v1.QueueJob: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.QueueJob: %v", t)
		return
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	err := sc.deleteComboSet(queuejob)
	if err != nil {
		glog.Errorf("Failed to delete queuejob %s from cache: %v", queuejob.Name, err)
		return
	}
	return
}

func (sc *schedulerCache) QueueJobInformer() qjobclient.ComboSetInformer {
	return sc.comboSetInformer
}

func (sc *schedulerCache) Dump() *CacheSnapshot {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	snapshot := &CacheSnapshot{
		ComboSets: make([]*ComboSetInfo, 0, len(sc.combosets)),
	}

	for _, value := range sc.combosets {
		snapshot.ComboSets = append(snapshot.ComboSets, value.Clone())
	}

	return snapshot
}
