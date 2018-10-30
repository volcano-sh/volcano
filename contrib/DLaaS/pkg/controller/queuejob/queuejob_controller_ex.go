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

package queuejob

import (
	"fmt"
	"github.com/golang/glog"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"strconv"
	"time"

	"k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	"k8s.io/apimachinery/pkg/labels"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources"
	resdeployment "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources/deployment"
	respod "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources/pod"
	resservice "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources/service"
	resstatefulset "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/controller/queuejobresources/statefulset"

	schedulercache "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/cache"

	schedulerapi "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"

	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned/clients"
	clientset "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/clientset/controller-versioned"
	arbinformers "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion"
	informersv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/informers/controller-externalversion/v1"
	listersv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/client/listers/controller/v1"
)

const (
	// QueueJobNameLabel label string for queuejob name
	QueueJobNameLabel string = "xqueuejob-name"

	// ControllerUIDLabel label string for queuejob controller uid
	ControllerUIDLabel string = "controller-uid"

	initialGetBackoff = 60 * time.Second	

)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = arbv1.SchemeGroupVersion.WithKind("XQueueJob")

//XController the XQueueJob Controller type
type XController struct {
	config           *rest.Config
	queueJobInformer informersv1.XQueueJobInformer
	// resources registered for the XQueueJob
	qjobRegisteredResources queuejobresources.RegisteredResources
	// controllers for these resources
	qjobResControls map[arbv1.ResourceType]queuejobresources.Interface

	clients    *kubernetes.Clientset
	arbclients *clientset.Clientset

	// A store of jobs
	queueJobLister listersv1.XQueueJobLister
	queueJobSynced func() bool

	// QueueJobs that need to be initialized
	// Add labels and selectors to XQueueJob
	initQueue *cache.FIFO

	// QueueJobs that need to sync up after initialization
	updateQueue *cache.FIFO

	// eventQueue that need to sync up
	eventQueue *cache.FIFO
	
	//QJ queue that needs to be allocated
	qjqueue SchedulingQueue
	
	// our own local cache, used for computing total amount of resources
	cache      schedulercache.Cache 

	// Reference manager to manage membership of queuejob resource and its members
	refManager queuejobresources.RefManager
}

//RegisterAllQueueJobResourceTypes - gegisters all resources
func RegisterAllQueueJobResourceTypes(regs *queuejobresources.RegisteredResources) {
	respod.Register(regs)
	resservice.Register(regs)
	resdeployment.Register(regs)
	resstatefulset.Register(regs)
}

func queueJobKey(obj interface{}) (string, error) {
	qj, ok := obj.(*arbv1.XQueueJob)
	if !ok {
		return "", fmt.Errorf("not a XQueueJob")
	}

	return fmt.Sprintf("%s/%s", qj.Namespace, qj.Name), nil
}

//NewXQueueJobController create new XQueueJob Controller
func NewXQueueJobController(config *rest.Config, schedulerName string) *XController {
	cc := &XController{
		config:      config,
		clients:     kubernetes.NewForConfigOrDie(config),
		arbclients:  clientset.NewForConfigOrDie(config),
		eventQueue:  cache.NewFIFO(queueJobKey),
		initQueue:   cache.NewFIFO(queueJobKey),
		updateQueue: cache.NewFIFO(queueJobKey),
		qjqueue:	  NewSchedulingQueue(),
		cache:		  schedulercache.New(config, schedulerName),
	}

	queueJobClient, _, err := clients.NewClient(cc.config)
	if err != nil {
		panic(err)
	}
	cc.qjobResControls = map[arbv1.ResourceType]queuejobresources.Interface{}
	RegisterAllQueueJobResourceTypes(&cc.qjobRegisteredResources)

	//initialize pod sub-resource control
	resControlPod, found, err := cc.qjobRegisteredResources.InitQueueJobResource(arbv1.ResourceTypePod, config)
	if err != nil {
		glog.Errorf("fail to create queuejob resource control")
		return nil
	}
	if !found {
		glog.Errorf("queuejob resource type Pod not found")
		return nil
	}
	cc.qjobResControls[arbv1.ResourceTypePod] = resControlPod

	// initialize service sub-resource control
	resControlService, found, err := cc.qjobRegisteredResources.InitQueueJobResource(arbv1.ResourceTypeService, config)
	if err != nil {
		glog.Errorf("fail to create queuejob resource control")
		return nil
	}
	if !found {
		glog.Errorf("queuejob resource type Service not found")
		return nil
	}
	cc.qjobResControls[arbv1.ResourceTypeService] = resControlService

	// initialize deployment sub-resource control
	resControlDeployment, found, err := cc.qjobRegisteredResources.InitQueueJobResource(arbv1.ResourceTypeDeployment, config)
	if err != nil {
		glog.Errorf("fail to create queuejob resource control")
		return nil
	}
	if !found {
		glog.Errorf("queuejob resource type Service not found")
		return nil
	}
	cc.qjobResControls[arbv1.ResourceTypeDeployment] = resControlDeployment

	// initialize SS sub-resource
	resControlSS, found, err := cc.qjobRegisteredResources.InitQueueJobResource(arbv1.ResourceTypeStatefulSet, config)
	if err != nil {
		glog.Errorf("fail to create queuejob resource control")
		return nil
	}
	if !found {
		glog.Errorf("queuejob resource type StatefulSet not found")
		return nil
	}
	cc.qjobResControls[arbv1.ResourceTypeStatefulSet] = resControlSS

	cc.queueJobInformer = arbinformers.NewSharedInformerFactory(queueJobClient, 0).XQueueJob().XQueueJobs()
	cc.queueJobInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *arbv1.XQueueJob:
					glog.V(4).Infof("Filter XQueueJob name(%s) namespace(%s)\n", t.Name, t.Namespace)
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
	cc.queueJobLister = cc.queueJobInformer.Lister()

	cc.queueJobSynced = cc.queueJobInformer.Informer().HasSynced

	//create sub-resource reference manager
	cc.refManager = queuejobresources.NewLabelRefManager()
	
	return cc
}

func (qjm *XController) PreemptQueueJobs() {
	qjobs := qjm.GetQueueJobsEligibleForPreemption()
	for _, q := range qjobs {
		newjob, e := qjm.queueJobLister.XQueueJobs(q.Namespace).Get(q.Name)
                if e != nil {
                        continue
                }
		newjob.Status.CanRun = false
		if _, err := qjm.arbclients.ArbV1().XQueueJobs(q.Namespace).Update(newjob); err != nil {
			glog.Errorf("Failed to update status of XQueueJob %v/%v: %v",
				q.Namespace, q.Name, err)
		}
	}
}


func (qjm *XController) GetQueueJobsEligibleForPreemption() []*arbv1.XQueueJob {
	qjobs := make([]*arbv1.XQueueJob, 0)

	queueJobs, err := qjm.queueJobLister.XQueueJobs("").List(labels.Everything())
	if err != nil {
		glog.Errorf("I return list of queueJobs %+v", qjobs)
		return qjobs
	}

	for _, value := range queueJobs {
		replicas := value.Spec.SchedSpec.MinAvailable

		if int(value.Status.Succeeded) == replicas {
			qjm.arbclients.ArbV1().XQueueJobs(value.Namespace).Delete(value.Name, &metav1.DeleteOptions{
	        	})
			continue
		}	
		if value.Status.State == arbv1.QueueJobStateEnqueued {
			continue
		}
		glog.Infof("I have job %s eligible for preemption %v - %v , %v !!! \n", value, value.Status.Running, replicas, value.Status.Succeeded)
		if int(value.Status.Running) < replicas {
			glog.Infof("I need to preempt job %s --------------------------------------", value.Name)
			qjobs = append(qjobs, value)
		}
	}
	return qjobs
}

func GetPodTemplate(qjobRes *arbv1.XQueueJobResource) (*v1.PodTemplateSpec, error) {
	rtScheme := runtime.NewScheme()
	v1.AddToScheme(rtScheme)

	jsonSerializer := json.NewYAMLSerializer(json.DefaultMetaFactory, rtScheme, rtScheme)

	podGVK := schema.GroupVersion{Group: v1.GroupName, Version: "v1"}.WithKind("PodTemplate")

	obj, _, err := jsonSerializer.Decode(qjobRes.Template.Raw, &podGVK, nil)
	if err != nil {
		return nil, err
	}

	template, ok := obj.(*v1.PodTemplate)
	if !ok {
		return nil, fmt.Errorf("Queuejob resource template not define a Pod")
	}

	return &template.Template, nil

}

func (qjm *XController) GetAggregatedResources(cqj *arbv1.XQueueJob) *schedulerapi.Resource {
	allocated := schedulerapi.EmptyResource()
        for _, resctrl := range qjm.qjobResControls {
                qjv     := resctrl.GetAggregatedResources(cqj)
                allocated = allocated.Add(qjv)
        }

        return allocated
}


func (qjm *XController) getAggregatedAvailableResourcesPriority(targetpr int, cqj string) *schedulerapi.Resource {
	cluster := qjm.cache.Snapshot()
	r := schedulerapi.EmptyResource()
	total := schedulerapi.EmptyResource()
	allocated := schedulerapi.EmptyResource()
	
	for _, value := range cluster.Nodes {
		total = total.Add(value.Allocatable)
	}

	queueJobs, err := qjm.queueJobLister.XQueueJobs("").List(labels.Everything())
        if err != nil {
                glog.Errorf("I return list of queueJobs %+v", err)
                return r
        }

	for _, value := range queueJobs {
		if value.Name == cqj {
			continue
		}
		if value.Status.State != arbv1.QueueJobStateActive {
			continue
		}
		glog.Infof("Job with priority: %s %v", value.Name, value.Spec.Priority)
		if value.Spec.Priority >= targetpr {
			for _, resctrl := range qjm.qjobResControls {
				qjv	:= resctrl.GetAggregatedResources(value)
				allocated = allocated.Add(qjv)
			}
		}
	}

	glog.Infof("I have allocated %+v, total %+v", total, allocated)

	if allocated.MilliCPU > total.MilliCPU || allocated.Memory > total.Memory || allocated.GPU > total.GPU {
		return r
	}
	
	r = total.Sub(allocated)
	return r
}


func (qjm *XController) ScheduleNext() {
	// get next QJ from the queue
	// check if we have enough compute resources for it 
	// if we have enough compute resources then we set the AllocatedReplicas to the total
	// amount of resources asked by the job
	glog.Infof("=======Schedule Next QueueJob!!!=======")
	qj, err := qjm.qjqueue.Pop()
	if err != nil {
		glog.Infof("Cannot pop QueueJob from the queue!")
	}
	glog.Infof("I have queuejob %+v", qj)
	// if scheduling error:
	// start thread that backs-off and puts back the QJ in the queue
	resources := qjm.getAggregatedAvailableResourcesPriority(qj.Spec.Priority, qj.Name)
	// get agg resources for the current qj
	aggqj := qjm.GetAggregatedResources(qj)

	glog.Infof("I have QueueJob with resources %v to be scheduled on aggregated idle resources %v", aggqj, resources)

	if qj.Status.CanRun {
		return
	}
	
	if aggqj.LessEqual(resources) {
		// qj is ready to go!
		newjob, e := qjm.queueJobLister.XQueueJobs(qj.Namespace).Get(qj.Name)
		if e != nil {
			return
		}
		desired := int32(0)
		for i, ar := range newjob.Spec.AggrResources.Items {
			desired += ar.Replicas
			newjob.Spec.AggrResources.Items[i].AllocatedReplicas = ar.Replicas
		}
		newjob.Status.CanRun = true
		qj.Status.CanRun = true
		if _, err := qjm.arbclients.ArbV1().XQueueJobs(qj.Namespace).Update(newjob); err != nil {
                        glog.Errorf("Failed to update status of XQueueJob %v/%v: %v",
                                qj.Namespace, qj.Name, err)
                }
	} else {
		// start thread to backoff
		go qjm.backoff(qj)
	}
	
}

func (qjm *XController) backoff(q *arbv1.XQueueJob) {
	time.Sleep(initialGetBackoff)
	qjm.qjqueue.AddIfNotPresent(q)
}



// Run start XQueueJob Controller
func (cc *XController) Run(stopCh chan struct{}) {
	// initialized
	createXQueueJobKind(cc.config)

	go cc.queueJobInformer.Informer().Run(stopCh)

	go cc.qjobResControls[arbv1.ResourceTypePod].Run(stopCh)
	go cc.qjobResControls[arbv1.ResourceTypeService].Run(stopCh)
	go cc.qjobResControls[arbv1.ResourceTypeDeployment].Run(stopCh)
	go cc.qjobResControls[arbv1.ResourceTypeStatefulSet].Run(stopCh)

	cache.WaitForCacheSync(stopCh, cc.queueJobSynced)

	cc.cache.Run(stopCh)

	go wait.Until(cc.ScheduleNext, 2*time.Second, stopCh)
	// start preempt thread based on preemption of pods

	// TODO - scheduleNext...Job....
        // start preempt thread based on preemption of pods
        go wait.Until(cc.PreemptQueueJobs, 60*time.Second, stopCh)

	go wait.Until(cc.UpdateQueueJobs, 2*time.Second, stopCh)

	go wait.Until(cc.worker, time.Second, stopCh)

}

func (qjm *XController) UpdateQueueJobs() {
        queueJobs, err := qjm.queueJobLister.XQueueJobs("").List(labels.Everything())
        if err != nil {
                glog.Errorf("I return list of queueJobs %+v", err)
                return 
        }

        for _, newjob := range queueJobs {
		qjm.enqueue(newjob)
                //if _, err := qjm.arbclients.ArbV1().XQueueJobs(newjob.Namespace).Update(newjob); err != nil {
                //        glog.Errorf("Failed to update status of XQueueJob %v/%v: %v",
                //                newjob.Namespace, newjob.Name, err)
                //}
        }
}

func (cc *XController) addQueueJob(obj interface{}) {
	qj, ok := obj.(*arbv1.XQueueJob)
	if !ok {
		glog.Errorf("obj is not XQueueJob")
		return
	}

	glog.V(4).Infof("QueueJob added - info -  %+v")

	cc.enqueue(qj)
}

func (cc *XController) updateQueueJob(oldObj, newObj interface{}) {
	newQJ, ok := newObj.(*arbv1.XQueueJob)
	if !ok {
		glog.Errorf("newObj is not XQueueJob")
		return
	}

	cc.enqueue(newQJ)
}

func (cc *XController) deleteQueueJob(obj interface{}) {
	qj, ok := obj.(*arbv1.XQueueJob)
	if !ok {
		glog.Errorf("obj is not XQueueJob")
		return
	}

	cc.enqueue(qj)
}

func (cc *XController) enqueue(obj interface{}) {
	err := cc.eventQueue.Add(obj)
	if err != nil {
		glog.Errorf("Fail to enqueue XQueueJob to updateQueue, err %#v", err)
	}
}

func (cc *XController) worker() {
	if _, err := cc.eventQueue.Pop(func(obj interface{}) error {
		var queuejob *arbv1.XQueueJob
		switch v := obj.(type) {
		case *arbv1.XQueueJob:
			queuejob = v
		default:
			glog.Errorf("Un-supported type of %v", obj)
			return nil
		}

		if queuejob == nil {
			if acc, err := meta.Accessor(obj); err != nil {
				glog.Warningf("Failed to get XQueueJob for %v/%v", acc.GetNamespace(), acc.GetName())
			}

			return nil
		}

		// sync XQueueJob
		if err := cc.syncQueueJob(queuejob); err != nil {
			glog.Errorf("Failed to sync XQueueJob %s, err %#v", queuejob.Name, err)
			// If any error, requeue it.
			return err
		}

		return nil
	}); err != nil {
		glog.Errorf("Fail to pop item from updateQueue, err %#v", err)
		return
	}
}

func (cc *XController) syncQueueJob(qj *arbv1.XQueueJob) error {
	queueJob, err := cc.queueJobLister.XQueueJobs(qj.Namespace).Get(qj.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			glog.V(3).Infof("Job has been deleted: %v", qj.Name)
			return nil
		}
		return err
	}

	// we call sync for each controller
        // update pods running, pending,...
        cc.qjobResControls[arbv1.ResourceTypePod].UpdateQueueJobStatus(qj)

	return cc.manageQueueJob(queueJob)
}

// manageQueueJob is the core method responsible for managing the number of running
// pods according to what is specified in the job.Spec.
// Does NOT modify <activePods>.
func (cc *XController) manageQueueJob(qj *arbv1.XQueueJob) error {
	var err error
	startTime := time.Now()
	defer func() {
		glog.Infof("Finished syncing queue job %q (%v)", qj.Name, time.Now().Sub(startTime))
	}()

	if qj.DeletionTimestamp != nil {
		// cleanup resources for running job
		err = cc.Cleanup(qj)
		if err != nil {
			return err
		}
		//empty finalizers and delete the queuejob again
		accessor, err := meta.Accessor(qj)
		if err != nil {
			return err
		}
		accessor.SetFinalizers(nil)
		
		// we delete the job from the queue if it is there
		cc.qjqueue.Delete(qj)

		return nil
		//var result arbv1.XQueueJob
		//return cc.arbclients.Put().
		//	Namespace(qj.Namespace).Resource(arbv1.QueueJobPlural).
		//	Name(qj.Name).Body(qj).Do().Into(&result)
	}

	glog.Infof("I have job with name %s status %+v ", qj.Name, qj.Status)   
	
	if !qj.Status.CanRun && (qj.Status.State != arbv1.QueueJobStateEnqueued && qj.Status.State != arbv1.QueueJobStateDeleted) {
		// if there are running resources for this job then delete them because the job was put in
		// pending state...
		glog.Infof("Deleting queuejob resources because it will be preempted! %s", qj.Name)
		err = cc.Cleanup(qj)
		if err != nil {
			return err
		}
		
		qj.Status.State = arbv1.QueueJobStateEnqueued
		_, err = cc.arbclients.ArbV1().XQueueJobs(qj.Namespace).Update(qj)
		if err != nil {
			return err
		}
		return nil
	}


	if !qj.Status.CanRun && qj.Status.State == arbv1.QueueJobStateEnqueued {
		glog.Infof("Putting job in queue!")
		cc.qjqueue.AddIfNotPresent(qj)
		return nil
	}
	
	if qj.Status.CanRun && qj.Status.State != arbv1.QueueJobStateActive {
		qj.Status.State =  arbv1.QueueJobStateActive
	}
	
	if qj.Spec.AggrResources.Items != nil {
		for i := range qj.Spec.AggrResources.Items {
			err := cc.refManager.AddTag(&qj.Spec.AggrResources.Items[i], func() string {
				return strconv.Itoa(i)
			})
			if err != nil {
				return err
			}

		}
	}

	for _, ar := range qj.Spec.AggrResources.Items {
		err00 := cc.qjobResControls[ar.Type].SyncQueueJob(qj, &ar)
		if err00 != nil {
			glog.Infof("I have error from sync job: %v", err00)
		}
	}

	// TODO(k82cn): replaced it with `UpdateStatus`
	if _, err := cc.arbclients.ArbV1().XQueueJobs(qj.Namespace).Update(qj); err != nil {
		glog.Errorf("Failed to update status of XQueueJob %v/%v: %v",
			qj.Namespace, qj.Name, err)
		return err
	}


	return err
}

//Cleanup function
func (cc *XController) Cleanup(queuejob *arbv1.XQueueJob) error {

	glog.Infof("Calling cleanup for XQueueJob %s \n", queuejob.Name)
	if queuejob.Spec.AggrResources.Items != nil {
		// we call clean-up for each controller
		for _, ar := range queuejob.Spec.AggrResources.Items {
			cc.qjobResControls[ar.Type].Cleanup(queuejob, &ar)
		}
	}

	old_flag := queuejob.Status.CanRun
        queuejob.Status = arbv1.XQueueJobStatus{
                Pending:      0,
                Running:      0,
                Succeeded:    0,
                Failed:       0,
                MinAvailable: int32(queuejob.Spec.SchedSpec.MinAvailable),
        }
	queuejob.Status.CanRun = old_flag

	return nil
}
