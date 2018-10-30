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

package statefulset

import (
	"fmt"
	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources"
	schedulerapi "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	apps "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"sync"
	"time"

	ssinformer "k8s.io/client-go/informers/apps/v1"
	sslister "k8s.io/client-go/listers/apps/v1"

	clientset "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var queueJobKind = arbv1.SchemeGroupVersion.WithKind("XQueueJob")
var queueJobName = "xqueuejob.arbitrator.k8s.io"

const (
	// QueueJobNameLabel label string for queuejob name
	QueueJobNameLabel string = "xqueuejob-name"

	// ControllerUIDLabel label string for queuejob controller uid
	ControllerUIDLabel string = "controller-uid"
)

//QueueJobResSS - stateful sets
type QueueJobResSS struct {
	clients    *kubernetes.Clientset
	arbclients *clientset.Clientset
	// A store of services, populated by the serviceController
	serviceStore   sslister.StatefulSetLister
	deployInformer ssinformer.StatefulSetInformer
	rtScheme       *runtime.Scheme
	jsonSerializer *json.Serializer
	// Reference manager to manage membership of queuejob resource and its members
	refManager queuejobresources.RefManager
}

// Register registers a queue job resource type
func Register(regs *queuejobresources.RegisteredResources) {
	regs.Register(arbv1.ResourceTypeStatefulSet, func(config *rest.Config) queuejobresources.Interface {
		return NewQueueJobResStatefulSet(config)
	})
}

//NewQueueJobResStatefulSet - creates a controller for SS
func NewQueueJobResStatefulSet(config *rest.Config) queuejobresources.Interface {
	qjrd := &QueueJobResSS{
		clients:    kubernetes.NewForConfigOrDie(config),
		arbclients: clientset.NewForConfigOrDie(config),
	}

	qjrd.deployInformer = informers.NewSharedInformerFactory(qjrd.clients, 0).Apps().V1().StatefulSets()
	qjrd.deployInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch obj.(type) {
				case *apps.StatefulSet:
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qjrd.addStatefulSet,
				UpdateFunc: qjrd.updateStatefulSet,
				DeleteFunc: qjrd.deleteStatefulSet,
			},
		})

	qjrd.rtScheme = runtime.NewScheme()
	v1.AddToScheme(qjrd.rtScheme)
	v1beta1.AddToScheme(qjrd.rtScheme)
	apps.AddToScheme(qjrd.rtScheme)
	qjrd.jsonSerializer = json.NewYAMLSerializer(json.DefaultMetaFactory, qjrd.rtScheme, qjrd.rtScheme)

	qjrd.refManager = queuejobresources.NewLabelRefManager()

	return qjrd
}

// Run the main goroutine responsible for watching and services.
func (qjrService *QueueJobResSS) Run(stopCh <-chan struct{}) {
	qjrService.deployInformer.Informer().Run(stopCh)
}

//GetPodTemplate Parse queue job api object to get Pod template
func (qjrPod *QueueJobResSS) GetPodTemplate(qjobRes *arbv1.XQueueJobResource) (*v1.PodTemplateSpec, int32, error) {
	res, err := qjrPod.getStatefulSetTemplate(qjobRes)
	if err != nil {
		return nil, -1, err
	}
        return &res.Spec.Template, *res.Spec.Replicas, nil
}

func (qjrPod *QueueJobResSS) GetAggregatedResources(job *arbv1.XQueueJob) *schedulerapi.Resource {
        total := schedulerapi.EmptyResource()
        if job.Spec.AggrResources.Items != nil {
            //calculate scaling
            for _, ar := range job.Spec.AggrResources.Items {
                if ar.Type == arbv1.ResourceTypeStatefulSet {
                        template, replicas, _ := qjrPod.GetPodTemplate(&ar)
			myres := queuejobresources.GetPodResources(template)
			myres.MilliCPU = float64(replicas) * myres.MilliCPU
			myres.Memory = float64(replicas) * myres.Memory
			myres.GPU = int64(replicas) * myres.GPU
			total = total.Add(myres)
                }
            }
        }
        return total
}

func (qjrPod *QueueJobResSS) GetAggregatedResourcesByPriority(priority int, job *arbv1.XQueueJob) *schedulerapi.Resource {
        total := schedulerapi.EmptyResource()
        if job.Spec.AggrResources.Items != nil {
            //calculate scaling
            for _, ar := range job.Spec.AggrResources.Items {
                  if ar.Priority < float64(priority) {
                        continue
                  }
                  if ar.Type == arbv1.ResourceTypeStatefulSet {
                        template, replicas, _ := qjrPod.GetPodTemplate(&ar)
                	myres := queuejobresources.GetPodResources(template)
                        myres.MilliCPU = float64(replicas) * myres.MilliCPU
                        myres.Memory = float64(replicas) * myres.Memory
                        myres.GPU = int64(replicas) * myres.GPU
                        total = total.Add(myres)
		}
            }
        }
        return total
}



func (qjrService *QueueJobResSS) addStatefulSet(obj interface{}) {

	return
}

func (qjrService *QueueJobResSS) updateStatefulSet(old, cur interface{}) {

	return
}

func (qjrService *QueueJobResSS) deleteStatefulSet(obj interface{}) {

	return
}

// Parse queue job api object to get Service template
func (qjrService *QueueJobResSS) getStatefulSetTemplate(qjobRes *arbv1.XQueueJobResource) (*apps.StatefulSet, error) {
	serviceGVK := schema.GroupVersion{Group: "", Version: "v1"}.WithKind("StatefulSet")
	obj, _, err := qjrService.jsonSerializer.Decode(qjobRes.Template.Raw, &serviceGVK, nil)
	if err != nil {
		return nil, err
	}

	service, ok := obj.(*apps.StatefulSet)
	if !ok {
		return nil, fmt.Errorf("Queuejob resource not defined as a StatefulSet")
	}

	return service, nil

}

func (qjrService *QueueJobResSS) createStatefulSetWithControllerRef(namespace string, service *apps.StatefulSet, controllerRef *metav1.OwnerReference) error {
	glog.V(4).Infof("==========create service: %s,  %+v \n", namespace, service)
	if controllerRef != nil {
		service.OwnerReferences = append(service.OwnerReferences, *controllerRef)
	}

	if _, err := qjrService.clients.AppsV1().StatefulSets(namespace).Create(service); err != nil {
		return err
	}

	return nil
}

func (qjrService *QueueJobResSS) delStatefulSet(namespace string, name string) error {

	glog.V(4).Infof("==========delete service: %s,  %s \n", namespace, name)
	if err := qjrService.clients.AppsV1().StatefulSets(namespace).Delete(name, nil); err != nil {
		return err
	}

	return nil
}

func (qjrPod *QueueJobResSS) UpdateQueueJobStatus(queuejob *arbv1.XQueueJob) error {
	return nil
}

//SyncQueueJob - syncs the resources of the queuejob
func (qjrService *QueueJobResSS) SyncQueueJob(queuejob *arbv1.XQueueJob, qjobRes *arbv1.XQueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	services, err := qjrService.getStatefulSetsForQueueJobRes(qjobRes, queuejob)
	if err != nil {
		return err
	}

	serviceLen := len(services)
	replicas := qjobRes.Replicas

	diff := int(replicas) - int(serviceLen)

	glog.V(4).Infof("QJob: %s had %d services and %d desired services", queuejob.Name, replicas, serviceLen)

	if diff > 0 {
		template, err := qjrService.getStatefulSetTemplate(qjobRes)
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

		template.Labels[queueJobName] = queuejob.Name
                if template.Spec.Template.Labels == nil {
                        template.Labels = map[string]string{}
                }
                template.Spec.Template.Labels[queueJobName] = queuejob.Name

		wait := sync.WaitGroup{}
		wait.Add(int(diff))
		for i := 0; i < diff; i++ {
			go func() {
				defer wait.Done()
				err := qjrService.createStatefulSetWithControllerRef(queuejob.Namespace, template, metav1.NewControllerRef(queuejob, queueJobKind))
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

func (qjrService *QueueJobResSS) getStatefulSetsForQueueJob(j *arbv1.XQueueJob) ([]*apps.StatefulSet, error) {
	servicelist, err := qjrService.clients.AppsV1().StatefulSets(j.Namespace).List(
									metav1.ListOptions{
                 					               LabelSelector: fmt.Sprintf("%s=%s", queueJobName, j.Name),
                						})
	if err != nil {
		return nil, err
	}

	services := []*apps.StatefulSet{}
	for _, service := range servicelist.Items {
				services = append(services, &service)
	}
	return services, nil

}

func (qjrService *QueueJobResSS) getStatefulSetsForQueueJobRes(qjobRes *arbv1.XQueueJobResource, j *arbv1.XQueueJob) ([]*apps.StatefulSet, error) {

	services, err := qjrService.getStatefulSetsForQueueJob(j)
	if err != nil {
		return nil, err
	}

	myServices := []*apps.StatefulSet{}
	for i, service := range services {
		if qjrService.refManager.BelongTo(qjobRes, service) {
			myServices = append(myServices, services[i])
		}
	}

	return myServices, nil

}

func (qjrService *QueueJobResSS) deleteQueueJobResStatefulSet(qjobRes *arbv1.XQueueJobResource, queuejob *arbv1.XQueueJob) error {
	job := *queuejob

	activeServices, err := qjrService.getStatefulSetsForQueueJob(queuejob)
	if err != nil {
		return err
	}

	active := int32(len(activeServices))

	wait := sync.WaitGroup{}
	wait.Add(int(active))
	for i := int32(0); i < active; i++ {
		go func(ix int32) {
			defer wait.Done()
			if err := qjrService.delStatefulSet(queuejob.Namespace, activeServices[ix].Name); err != nil {
				defer utilruntime.HandleError(err)
				glog.V(2).Infof("Failed to delete %v, queue job %q/%q deadline exceeded", activeServices[ix].Name, job.Namespace, job.Name)
			}
		}(i)
	}
	wait.Wait()

	return nil
}

//Cleanup - cleans the resources
func (qjrService *QueueJobResSS) Cleanup(queuejob *arbv1.XQueueJob, qjobRes *arbv1.XQueueJobResource) error {
	return qjrService.deleteQueueJobResStatefulSet(qjobRes, queuejob)
}
