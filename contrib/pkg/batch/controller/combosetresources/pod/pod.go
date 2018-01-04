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
	"sync"
	"time"

	"github.com/golang/glog"
	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/contrib/pkg/batch/controller/combosetresources"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"

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

var controllerKind = qjobv1.SchemeGroupVersion.WithKind("ComboSet")

type ComboSetResPod struct {
	kubeClient clientset.Interface
	podControl controller.PodControlInterface

	// A TTLCache of pod creates/deletes each rc expects to see
	expectations controller.ControllerExpectationsInterface

	// A store of pods, populated by the podController
	podStore       corelisters.PodLister
	podInformer    corev1informer.PodInformer
	rtScheme       *runtime.Scheme
	jsonSerializer *json.Serializer

	// Reference manager to manage membership of queuejob resource and its members
	refManager combosetresources.RefManager
	recorder   record.EventRecorder
}

// Register registers a queue job resource type
func Register(regs *combosetresources.RegisteredResources) {
	regs.Register(qjobv1.ResourceTypePod, func(config *rest.Config) combosetresources.Interface {
		return NewQueueJobResPod(config)
	})
}

func NewQueueJobResPod(config *rest.Config) combosetresources.Interface {

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)

	// create k8s clientset
	kubeClient, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Errorf("fail to create clientset")
		return nil
	}

	qjrPod := &ComboSetResPod{
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

	qjrPod.jsonSerializer = json.NewYAMLSerializer(json.DefaultMetaFactory, qjrPod.rtScheme, qjrPod.rtScheme)

	qjrPod.refManager = combosetresources.NewLabelRefManager()

	return qjrPod
}

// Run the main goroutine responsible for watching and pods.
func (qjrPod *ComboSetResPod) Run(stopCh <-chan struct{}) {

	qjrPod.podInformer.Informer().Run(stopCh)
}

func (qjrPod *ComboSetResPod) addPod(obj interface{}) {

	return
}

func (qjrPod *ComboSetResPod) updatePod(old, cur interface{}) {

	return
}

func (qjrPod *ComboSetResPod) deletePod(obj interface{}) {

	return
}

// Parse queue job api object to get Pod template
func (qjrPod *ComboSetResPod) getPodTemplate(qjobRes *qjobv1.ComboSetResource) (*v1.PodTemplate, error) {

	podGVK := schema.GroupVersion{Group: v1.GroupName, Version: "v1"}.WithKind("PodTemplate")

	obj, _, err := qjrPod.jsonSerializer.Decode(qjobRes.Template.Raw, &podGVK, nil)
	if err != nil {
		return nil, err
	}

	template, ok := obj.(*v1.PodTemplate)
	if !ok {
		return nil, fmt.Errorf("Queuejob resource template not define a Pod")
	}

	return template, nil

}

// scale up queuejob resource to the desired number
func (qjrPod *ComboSetResPod) scaleUpQueueJobResource(diff int32, activePods []*v1.Pod, queuejob *qjobv1.ComboSet, qjobRes *qjobv1.ComboSetResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	template, err := qjrPod.getPodTemplate(qjobRes)
	if err != nil {
		return err
	}

	//TODO: need set reference after Pod has been really added
	tmpPod := v1.Pod{}
	err = qjrPod.refManager.AddReference(qjobRes, &tmpPod)
	if err != nil {
		return err
	}

	for k, v := range tmpPod.Labels {
		template.Template.Labels[k] = v
	}

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
func (qjrPod *ComboSetResPod) scaleDownQueueJobResource(diff int32, activePods []*v1.Pod, queuejob *qjobv1.ComboSet, qjobRes *qjobv1.ComboSetResource) error {

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

func (qjrPod *ComboSetResPod) Sync(queuejob *qjobv1.ComboSet, qjobRes *qjobv1.ComboSetResource) error {

	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing queue job resource %q (%v)", qjobRes.Template, time.Now().Sub(startTime))
	}()

	pods, err := qjrPod.getPodsForQueueJobRes(qjobRes, queuejob)
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

		qjrPod.scaleUpQueueJobResource(
			int32(qjobRes.DesiredReplicas-numActivePods),
			activePods, queuejob, qjobRes)
	}

	return nil
}

func (qjrPod *ComboSetResPod) getPodsForQueueJob(j *qjobv1.ComboSet) ([]*v1.Pod, error) {
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

func (qjrPod *ComboSetResPod) getPodsForQueueJobRes(qjobRes *qjobv1.ComboSetResource, j *qjobv1.ComboSet) ([]*v1.Pod, error) {

	pods, err := qjrPod.getPodsForQueueJob(j)
	if err != nil {
		return nil, err
	}

	myPods := []*v1.Pod{}
	for i, pod := range pods {
		if qjrPod.refManager.BelongTo(qjobRes, pod) {
			myPods = append(myPods, pods[i])
		}
	}

	return myPods, nil

}

func (qjrPod *ComboSetResPod) deleteQueueJobResPods(qjobRes *qjobv1.ComboSetResource, queuejob *qjobv1.ComboSet) error {

	job := *queuejob

	pods, err := qjrPod.getPodsForQueueJobRes(qjobRes, queuejob)
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

func (qjrPod *ComboSetResPod) Cleanup(queuejob *qjobv1.ComboSet, qjobRes *qjobv1.ComboSetResource) error {

	return qjrPod.deleteQueueJobResPods(qjobRes, queuejob)
}

func (qjrPod *ComboSetResPod) GetResourceAllocated() *qjobv1.ResourceList {
	return nil
}

func (qjrPod *ComboSetResPod) GetResourceRequest() *cache.Resource {
	return nil
}

func (qjrPod *ComboSetResPod) SetResourceAllocated(qjobv1.ResourceList) error {
	return nil
}
