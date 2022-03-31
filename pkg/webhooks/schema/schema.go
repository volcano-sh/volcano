/*
Copyright 2019 The Volcano Authors.

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

package schema

import (
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog"
	corev1 "k8s.io/kubernetes/pkg/apis/core/v1"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func init() {
	addToScheme(scheme)
}

var scheme = runtime.NewScheme()

//Codecs is for retrieving serializers for the supported wire formats
//and conversion wrappers to define preferred internal and external versions.
var Codecs = serializer.NewCodecFactory(scheme)

func addToScheme(scheme *runtime.Scheme) {
	corev1.AddToScheme(scheme)
	admissionv1.AddToScheme(scheme)
}

//DecodeJob decodes the job using deserializer from the raw object.
func DecodeJob(object runtime.RawExtension, resource metav1.GroupVersionResource) (*batchv1alpha1.Job, error) {
	jobResource := metav1.GroupVersionResource{Group: batchv1alpha1.SchemeGroupVersion.Group, Version: batchv1alpha1.SchemeGroupVersion.Version, Resource: "jobs"}
	raw := object.Raw
	job := batchv1alpha1.Job{}

	if resource != jobResource {
		err := fmt.Errorf("expect resource to be %s", jobResource)
		return &job, err
	}

	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &job); err != nil {
		return &job, err
	}
	klog.V(3).Infof("the job struct is %+v", job)

	return &job, nil
}

func DecodePod(object runtime.RawExtension, resource metav1.GroupVersionResource) (*v1.Pod, error) {
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	raw := object.Raw
	pod := v1.Pod{}

	if resource != podResource {
		err := fmt.Errorf("expect resource to be %s", podResource)
		return &pod, err
	}

	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		return &pod, err
	}
	klog.V(3).Infof("the pod struct is %+v", pod)

	return &pod, nil
}

// DecodeQueue decodes the queue using deserializer from the raw object.
func DecodeQueue(object runtime.RawExtension, resource metav1.GroupVersionResource) (*schedulingv1beta1.Queue, error) {
	queueResource := metav1.GroupVersionResource{
		Group:    schedulingv1beta1.SchemeGroupVersion.Group,
		Version:  schedulingv1beta1.SchemeGroupVersion.Version,
		Resource: "queues",
	}

	if resource != queueResource {
		return nil, fmt.Errorf("expect resource to be %s", queueResource)
	}

	queue := schedulingv1beta1.Queue{}
	if _, _, err := Codecs.UniversalDeserializer().Decode(object.Raw, nil, &queue); err != nil {
		return nil, err
	}

	return &queue, nil
}

// DecodePodGroup decodes the podgroup using deserializer from the raw object.
func DecodePodGroup(object runtime.RawExtension, resource metav1.GroupVersionResource) (*schedulingv1beta1.PodGroup, error) {
	podgroupResource := metav1.GroupVersionResource{
		Group:    schedulingv1beta1.SchemeGroupVersion.Group,
		Version:  schedulingv1beta1.SchemeGroupVersion.Version,
		Resource: "podgroups",
	}

	if resource != podgroupResource {
		return nil, fmt.Errorf("expect resource to be %s", podgroupResource)
	}

	podgroup := schedulingv1beta1.PodGroup{}
	if _, _, err := Codecs.UniversalDeserializer().Decode(object.Raw, nil, &podgroup); err != nil {
		return nil, err
	}

	return &podgroup, nil
}
