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

package controller

import (
	"fmt"
	"time"

	"k8s.io/api/policy/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/informers"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/client/informers/v1"
)

type QueueJobController struct {
	config           *rest.Config
	queueJobInformer v1.QueueJobInformer
	clients          *kubernetes.Clientset
	msgQueue         *cache.FIFO
}

func NewQueueJobController(config *rest.Config) *QueueJobController {
	cc := &QueueJobController{
		config:   config,
		msgQueue: cache.NewFIFO(queueJobKey),
		clients:  kubernetes.NewForConfigOrDie(config),
	}

	queueJobClient, _, err := client.NewClient(cc.config)
	if err != nil {
		panic(err)
	}
	informerFactory := informers.NewSharedInformerFactory(queueJobClient, 0)
	cc.queueJobInformer = informerFactory.QueueJob().QueueJobs()
	cc.queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *arbv1.QueueJob:
					glog.V(4).Infof("Filter QueueJob name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    cc.addQueueJob,
				UpdateFunc: cc.updateQueueJob,
				DeleteFunc: cc.deleteQueueJob,
			},
		})

	return cc
}

func (cc *QueueJobController) addQueueJob(obj interface{}) {
	qj, ok := obj.(*arbv1.QueueJob)
	if !ok {
		glog.Errorf("obj is not QueueJob")
		return
	}

	qji := newQueueJobInfo(qj)
	cc.enqueueController(qji)
}

func (cc *QueueJobController) updateQueueJob(oldObj, newObj interface{}) {}

func (cc *QueueJobController) deleteQueueJob(obj interface{}) {}

func (cc *QueueJobController) enqueueController(obj interface{}) {
	err := cc.msgQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue QueueJobInfo, err %#v", err)
	}
}

func (cc *QueueJobController) worker() {
	cc.msgQueue.Pop(func(obj interface{}) error {
		qj, ok := obj.(*QueueJobInfo)
		if !ok {
			return fmt.Errorf("Not a QueueJobInfo")
		}

		cc.startupQueueJob(qj.QueueJob)

		return nil
	})
}

func (cc *QueueJobController) startupQueueJob(qj *arbv1.QueueJob) {
	// Add queuejob_name label for each Pod
	labels := map[string]string{
		"queuejob_name": qj.Name,
	}

	minAvailable := int32(0)
	if qj.Spec.MinAvailable != nil {
		minAvailable = *qj.Spec.MinAvailable
	} else {
		minAvailable = int32(1)
	}

	// Create PodDisruptionBudgets for this queue job to support gang-scheduling
	err := cc.initPDB(int(minAvailable), labels)
	if err != nil {
		glog.Errorf("Failed to create PDB for QueueJob %s, error %#v", qj.Name, err)
		return
	}

	// Create Pods for QueueJob
	owner := buildOwnerReference("v1", arbv1.QueueJobType, string(qj.UID))
	for i := int32(0); i < qj.Spec.Replicas; i++ {
		labels["index"] = fmt.Sprintf("%d", i)
		pod := buildPod(fmt.Sprintf("%s-%d", qj.Name, i), qj.Namespace, qj.Spec.Template, []metav1.OwnerReference{owner}, labels)

		_, err := cc.clients.CoreV1().Pods(pod.Namespace).Create(pod)
		if err != nil {
			// TODO(jinzhejz): try to start pod again when it failed
			glog.Errorf("Failed to create Pod for QueueJob %s, error %#v", qj.Name, err)
			return
		}
	}
}

func (cc *QueueJobController) initPDB(min int, selectorMap map[string]string) error {
	selector := &metav1.LabelSelector{
		MatchLabels: selectorMap,
	}
	minAvailable := intstr.FromInt(min)
	pdb := &v1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("pdb-%s", generateUUID()),
		},
		Spec: v1beta1.PodDisruptionBudgetSpec{
			Selector:     selector,
			MinAvailable: &minAvailable,
		},
	}

	_, err := cc.clients.Policy().PodDisruptionBudgets("default").Create(pdb)

	return err
}

func (cc *QueueJobController) Run(stopCh chan struct{}) {
	// initialized
	cc.createQueueJobCRD()

	go cc.queueJobInformer.Informer().Run(stopCh)

	go wait.Until(cc.worker, time.Second, stopCh)
}

func (cc *QueueJobController) createQueueJobCRD() error {
	extensionscs, err := apiextensionsclient.NewForConfig(cc.config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueJobCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
