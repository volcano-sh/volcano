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

package pod

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/golang/glog"
	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/controller/queuejobresources"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
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
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/controller"
)

var controllerKind = qjobv1.SchemeGroupVersion.WithKind("QueueJob")

const qjobResIdxLable = "QJOBRES_INDEX"

type QueueJobResPod struct {
	kubeClient clientset.Interface
	podControl controller.PodControlInterface

	// A TTLCache of pod creates/deletes each rc expects to see
	expectations controller.ControllerExpectationsInterface

	// A store of pods, populated by the podController
	podStore       corelisters.PodLister
	podInformer    corev1informer.PodInformer
	rtScheme       *runtime.Scheme
	jsonSerializer *json.Serializer
	recorder       record.EventRecorder
}

// Register registers a queue job resource type
func Register(regs *queuejobresources.RegisteredResources) {
	regs.Register(qjobv1.ResourceTypePod, func(config *rest.Config) queuejobresources.Interface {
		return NewQueueJobResPod(config)
	})
}

func NewQueueJobResPod(config *rest.Config) queuejobresources.Interface {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)

	// create k8s clientset
	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Errorf("fail to create clientset")
		return nil
	}

	qjrPod := &QueueJobResPod{
		kubeClient: kubeClient,
		podControl: controller.RealPodControl{
			KubeClient: kubeClient,
			Recorder:   eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "queuejob-controller"}),
		},
		expectations: controller.NewControllerExpectations(),
		recorder:     eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "queuejob-controller"}),
	}

	// create informer for pod information
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	qjrPod.podInformer = informerFactory.Core().V1().Pods()
	qjrPod.podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					glog.V(4).Infof("filter pod name(%s) namespace(%s) status(%s)\n", t.Name, t.Namespace, t.Status.Phase)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qjrPod.addPod,
				UpdateFunc: qjrPod.updatePod,
				DeleteFunc: qjrPod.deletePod,
			},
		})

	qjrPod.rtScheme = runtime.NewScheme()
	v1.AddToScheme(qjrPod.rtScheme)
	//qjobv1.AddToScheme(qjrPod.scheme)

	qjrPod.jsonSerializer = json.NewYAMLSerializer(json.DefaultMetaFactory, qjrPod.rtScheme, qjrPod.rtScheme)

	return qjrPod
}

// Run the main goroutine responsible for watching and pods.
func (qjrPod *QueueJobResPod) Run(stopCh <-chan struct{}) {

	qjrPod.podInformer.Informer().Run(stopCh)
}

func (qjrPod *QueueJobResPod) addPod(obj interface{}) {

	return
}

func (qjrPod *QueueJobResPod) updatePod(old, cur interface{}) {

	return
}

func (qjrPod *QueueJobResPod) deletePod(obj interface{}) {

	return
}

// Parse queue job api object to get Pod template
func (qjrPod *QueueJobResPod) getPodTemplate(qjobRes *qjobv1.QueueJobResource) (*v1.PodTemplate, error) {

	podGVK := schema.GroupVersion{Group: v1.GroupName, Version: "v1"}.WithKind("PodTemplate")

	fmt.Printf("=============== getPodTemplate %s\n", qjobRes.Template.Raw)
	obj, _, err := qjrPod.jsonSerializer.Decode(qjobRes.Template.Raw, &podGVK, nil)
	if err != nil {
		return nil, err
	}

	template, ok := obj.(*v1.PodTemplate)
	if !ok {
		return nil, fmt.Errorf("Queuejob resource template not define a Pod")
	}

	fmt.Printf("=============== getPodTemplate %v\n", template)
	return template, nil

}

// scale up queuejob resource to the desired number
func (qjrPod *QueueJobResPod) scaleUpQueueJobResource(resIndex int, diff int32, activePods []*v1.Pod, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	template, err := qjrPod.getPodTemplate(qjobRes)
	if err != nil {
		return err
	}

	if template.Template.Labels == nil {

		template.Template.Labels = map[string]string{}
	}
	template.Template.Labels[qjobResIdxLable] = strconv.Itoa(resIndex)

	wait := sync.WaitGroup{}
	wait.Add(int(diff))
	for i := int32(0); i < diff; i++ {
		go func() {
			defer wait.Done()
			err := qjrPod.podControl.CreatePodsWithControllerRef(queuejob.Namespace, &template.Template, queuejob, metav1.NewControllerRef(queuejob, controllerKind))
			if err != nil && errors.IsTimeout(err) {
				return
			}
			if err != nil {
				defer utilruntime.HandleError(err)
			}
		}()
	}
	wait.Wait()

	return nil

}

// scale down queuejob resource to the desired number
func (qjrPod *QueueJobResPod) scaleDownQueueJobResource(diff int32, activePods []*v1.Pod, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	wait := sync.WaitGroup{}
	wait.Add(int(diff))
	for i := int32(0); i < diff; i++ {
		go func(ix int32) {
			defer wait.Done()
			if err := qjrPod.podControl.DeletePod(queuejob.Namespace, activePods[ix].Name, queuejob); err != nil {
				defer utilruntime.HandleError(err)
			}
		}(i)
	}
	wait.Wait()
	return nil

}

func (qjrPod *QueueJobResPod) Sync(resIndex int, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	fmt.Printf("=============== sync  %v\n", qjobRes)
	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	if qjobRes.ObjectMeta.Labels == nil {
		qjobRes.ObjectMeta.Labels = map[string]string{}
	}
	qjobRes.ObjectMeta.Labels[qjobResIdxLable] = strconv.Itoa(resIndex)

	pods, err := qjrPod.getPodsForQueueJobRes(resIndex, queuejob)
	if err != nil {
		return err
	}

	activePods := controller.FilterActivePods(pods)
	numActivePods := int32(len(activePods))

	if qjobRes.DesiredReplicas < numActivePods {

		qjrPod.scaleDownQueueJobResource(
			int32(numActivePods-qjobRes.DesiredReplicas),
			activePods, queuejob, qjobRes)
	} else if qjobRes.DesiredReplicas > numActivePods {

		qjrPod.scaleUpQueueJobResource(resIndex,
			int32(qjobRes.DesiredReplicas-numActivePods),
			activePods, queuejob, qjobRes)
	}

	return nil
}

func (qjrPod *QueueJobResPod) getPodsForQueueJob(j *qjobv1.QueueJob) ([]*v1.Pod, error) {
	podlist, err := qjrPod.kubeClient.CoreV1().Pods(j.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	pods := []*v1.Pod{}
	for i, pod := range podlist.Items {
		meta_pod, err := meta.Accessor(&pod)
		if err != nil {
			return nil, err
		}

		controllerRef := metav1.GetControllerOf(meta_pod)
		if controllerRef != nil {
			if controllerRef.UID == j.UID {
				pods = append(pods, &podlist.Items[i])
			}
		}
	}
	return pods, nil

}

func (qjrPod *QueueJobResPod) getPodsForQueueJobRes(resIndex int, j *qjobv1.QueueJob) ([]*v1.Pod, error) {

	pods, err := qjrPod.getPodsForQueueJob(j)
	if err != nil {
		return nil, err
	}

	myPods := []*v1.Pod{}
	for i, pod := range pods {
		if pod.Labels[qjobResIdxLable] == strconv.Itoa(resIndex) {
			myPods = append(myPods, pods[i])
		}
	}

	return myPods, nil

}

func (qjrPod *QueueJobResPod) deleteQueueJobResPods(resIndex int, queuejob *qjobv1.QueueJob) error {

	job := *queuejob

	pods, err := qjrPod.getPodsForQueueJobRes(resIndex, queuejob)
	if err != nil {
		return err
	}

	activePods := controller.FilterActivePods(pods)
	active := int32(len(activePods))

	wait := sync.WaitGroup{}
	wait.Add(int(active))
	for i := int32(0); i < active; i++ {
		go func(ix int32) {
			defer wait.Done()
			if err := qjrPod.podControl.DeletePod(queuejob.Namespace, activePods[ix].Name, queuejob); err != nil {
				defer utilruntime.HandleError(err)
				glog.V(2).Infof("Failed to delete %v, queue job %q/%q deadline exceeded", activePods[ix].Name, job.Namespace, job.Name)
			}
		}(i)
	}
	wait.Wait()

	return nil
}

func (qjrPod *QueueJobResPod) Cleanup(resIndex int, queuejob *qjobv1.QueueJob, qjobRes *qjobv1.QueueJobResource) error {

	return qjrPod.deleteQueueJobResPods(resIndex, queuejob)
}

func (qjrPod *QueueJobResPod) GetResourceAllocated() *qjobv1.ResourceList {
	return nil
}

func (qjrPod *QueueJobResPod) GetResourceRequest() *schedulercache.Resource {
	return nil
}

func (qjrPod *QueueJobResPod) SetResourceAllocated(qjobv1.ResourceList) error {
	return nil
}
