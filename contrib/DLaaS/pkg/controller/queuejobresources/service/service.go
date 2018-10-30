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

package service

import (
	"fmt"
	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	clientset "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources"
	schedulerapi "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"sync"
	"time"
)

var queueJobKind = arbv1.SchemeGroupVersion.WithKind("XQueueJob")
var queueJobName = "xqueuejob.arbitrator.k8s.io"

const (
	// QueueJobNameLabel label string for queuejob name
	QueueJobNameLabel string = "xqueuejob-name"

	// ControllerUIDLabel label string for queuejob controller uid
	ControllerUIDLabel string = "controller-uid"
)

//QueueJobResService contains service info
type QueueJobResService struct {
	clients    *kubernetes.Clientset
	arbclients *clientset.Clientset
	// A store of services, populated by the serviceController
	serviceStore    corelisters.ServiceLister
	serviceInformer corev1informer.ServiceInformer
	rtScheme        *runtime.Scheme
	jsonSerializer  *json.Serializer
	// Reference manager to manage membership of queuejob resource and its members
	refManager queuejobresources.RefManager
}

//Register registers a queue job resource type
func Register(regs *queuejobresources.RegisteredResources) {
	regs.Register(arbv1.ResourceTypeService, func(config *rest.Config) queuejobresources.Interface {
		return NewQueueJobResService(config)
	})
}

//NewQueueJobResService creates a service controller
func NewQueueJobResService(config *rest.Config) queuejobresources.Interface {
	qjrService := &QueueJobResService{
		clients:    kubernetes.NewForConfigOrDie(config),
		arbclients: clientset.NewForConfigOrDie(config),
	}

	qjrService.serviceInformer = informers.NewSharedInformerFactory(qjrService.clients, 0).Core().V1().Services()
	qjrService.serviceInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch obj.(type) {
				case *v1.Service:
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qjrService.addService,
				UpdateFunc: qjrService.updateService,
				DeleteFunc: qjrService.deleteService,
			},
		})

	qjrService.rtScheme = runtime.NewScheme()
	v1.AddToScheme(qjrService.rtScheme)

	qjrService.jsonSerializer = json.NewYAMLSerializer(json.DefaultMetaFactory, qjrService.rtScheme, qjrService.rtScheme)

	qjrService.refManager = queuejobresources.NewLabelRefManager()

	return qjrService
}

// Run the main goroutine responsible for watching and services.
func (qjrService *QueueJobResService) Run(stopCh <-chan struct{}) {

	qjrService.serviceInformer.Informer().Run(stopCh)
}

func (qjrPod *QueueJobResService) GetAggregatedResources(job *arbv1.XQueueJob) *schedulerapi.Resource {
	return schedulerapi.EmptyResource()
}

func (qjrService *QueueJobResService) addService(obj interface{}) {

	return
}

func (qjrService *QueueJobResService) updateService(old, cur interface{}) {

	return
}

func (qjrService *QueueJobResService) deleteService(obj interface{}) {

	return
}


func (qjrPod *QueueJobResService) GetAggregatedResourcesByPriority(priority int, job *arbv1.XQueueJob) *schedulerapi.Resource {
        total := schedulerapi.EmptyResource()
        return total
}


// Parse queue job api object to get Service template
func (qjrService *QueueJobResService) getServiceTemplate(qjobRes *arbv1.XQueueJobResource) (*v1.Service, error) {

	serviceGVK := schema.GroupVersion{Group: v1.GroupName, Version: "v1"}.WithKind("Service")

	obj, _, err := qjrService.jsonSerializer.Decode(qjobRes.Template.Raw, &serviceGVK, nil)
	if err != nil {
		return nil, err
	}

	service, ok := obj.(*v1.Service)
	if !ok {
		return nil, fmt.Errorf("Queuejob resource not defined as a Service")
	}

	return service, nil

}

func (qjrService *QueueJobResService) createServiceWithControllerRef(namespace string, service *v1.Service, controllerRef *metav1.OwnerReference) error {

	glog.V(4).Infof("==========create service: %s,  %+v \n", namespace, service)
	if controllerRef != nil {
		service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	}

	if _, err := qjrService.clients.Core().Services(namespace).Create(service); err != nil {
		return err
	}

	return nil
}

func (qjrService *QueueJobResService) delService(namespace string, name string) error {

	glog.V(4).Infof("==========delete service: %s,  %s \n", namespace, name)
	if err := qjrService.clients.Core().Services(namespace).Delete(name, nil); err != nil {
		return err
	}

	return nil
}

func (qjrPod *QueueJobResService) UpdateQueueJobStatus(queuejob *arbv1.XQueueJob) error {
	return nil
}

//SyncQueueJob syncs the services
func (qjrService *QueueJobResService) SyncQueueJob(queuejob *arbv1.XQueueJob, qjobRes *arbv1.XQueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	services, err := qjrService.getServicesForQueueJobRes(qjobRes, queuejob)
	if err != nil {
		return err
	}

	serviceLen := len(services)
	replicas := qjobRes.Replicas

	diff := int(replicas) - int(serviceLen)

	glog.V(4).Infof("QJob: %s had %d services and %d desired services", queuejob.Name, replicas, serviceLen)

	if diff > 0 {
		template, err := qjrService.getServiceTemplate(qjobRes)
		if err != nil {
			glog.Errorf("Cannot read template from resource %+v %+v", qjobRes, err)
			return err
		}
		//TODO: need set reference after Service has been really added
		tmpService := v1.Service{}
		err = qjrService.refManager.AddReference(qjobRes, &tmpService)
		if err != nil {
			glog.Errorf("Cannot add reference to service resource %+v", err)
			return err
		}

		if template.Labels == nil {
			template.Labels = map[string]string{}
		}
		for k, v := range tmpService.Labels {
			template.Labels[k] = v
		}
		wait := sync.WaitGroup{}
		wait.Add(int(diff))
		for i := 0; i < diff; i++ {
			go func() {
				defer wait.Done()
				err := qjrService.createServiceWithControllerRef(queuejob.Namespace, template, metav1.NewControllerRef(queuejob, queueJobKind))
				if err != nil && errors.IsTimeout(err) {
					return
				}
				if err != nil {
					defer utilruntime.HandleError(err)
				}
			}()
		}
		wait.Wait()
	}

	return nil
}

func (qjrService *QueueJobResService) getServicesForQueueJob(j *arbv1.XQueueJob) ([]*v1.Service, error) {
	servicelist, err := qjrService.clients.CoreV1().Services(j.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	services := []*v1.Service{}
	for i, service := range servicelist.Items {
		metaService, err := meta.Accessor(&service)
		if err != nil {
			return nil, err
		}

		controllerRef := metav1.GetControllerOf(metaService)
		if controllerRef != nil {
			if controllerRef.UID == j.UID {
				services = append(services, &servicelist.Items[i])
			}
		}
	}
	return services, nil

}

func (qjrService *QueueJobResService) getServicesForQueueJobRes(qjobRes *arbv1.XQueueJobResource, j *arbv1.XQueueJob) ([]*v1.Service, error) {

	services, err := qjrService.getServicesForQueueJob(j)
	if err != nil {
		return nil, err
	}

	myServices := []*v1.Service{}
	for i, service := range services {
		if qjrService.refManager.BelongTo(qjobRes, service) {
			myServices = append(myServices, services[i])
		}
	}

	return myServices, nil

}

func (qjrService *QueueJobResService) deleteQueueJobResServices(qjobRes *arbv1.XQueueJobResource, queuejob *arbv1.XQueueJob) error {

	job := *queuejob

	activeServices, err := qjrService.getServicesForQueueJobRes(qjobRes, queuejob)
	if err != nil {
		return err
	}

	active := int32(len(activeServices))

	wait := sync.WaitGroup{}
	wait.Add(int(active))
	for i := int32(0); i < active; i++ {
		go func(ix int32) {
			defer wait.Done()
			if err := qjrService.delService(queuejob.Namespace, activeServices[ix].Name); err != nil {
				defer utilruntime.HandleError(err)
				glog.V(2).Infof("Failed to delete %v, queue job %q/%q deadline exceeded", activeServices[ix].Name, job.Namespace, job.Name)
			}
		}(i)
	}
	wait.Wait()

	return nil
}

//Cleanup deletes all services
func (qjrService *QueueJobResService) Cleanup(queuejob *arbv1.XQueueJob, qjobRes *arbv1.XQueueJobResource) error {

	return qjrService.deleteQueueJobResServices(qjobRes, queuejob)
}
