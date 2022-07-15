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

	vcbusv1 "volcano.sh/apis/pkg/apis/batch/v1"
	vcbatchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	vcschedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1"
	vcschedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
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
func DecodeJob(object runtime.RawExtension, resource metav1.GroupVersionResource) (*vcbusv1.Job, error) {
	raw := object.Raw
	job := vcbusv1.Job{}
	supportedGVRs := []metav1.GroupVersionResource{
		{
			Group:    vcbatchv1alpha1.SchemeGroupVersion.Group,
			Version:  vcbatchv1alpha1.SchemeGroupVersion.Version,
			Resource: "jobs",
		},
		{
			Group:    vcbusv1.SchemeGroupVersion.Group,
			Version:  vcbusv1.SchemeGroupVersion.Version,
			Resource: "jobs",
		},
	}

	isSpport := false
	for _, gvr := range supportedGVRs {
		if resource == gvr {
			isSpport = true
			break
		}
	}

	if !isSpport {
		err := fmt.Errorf("expect resource to be %s", supportedGVRs)
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
func DecodeQueue(object runtime.RawExtension, resource metav1.GroupVersionResource) (*vcschedulingv1.Queue, error) {
	supportedGVRs := []metav1.GroupVersionResource{
		{
			Group:    vcschedulingv1.SchemeGroupVersion.Group,
			Version:  vcschedulingv1.SchemeGroupVersion.Version,
			Resource: "queues",
		},
		{
			Group:    vcschedulingv1beta1.SchemeGroupVersion.Group,
			Version:  vcschedulingv1beta1.SchemeGroupVersion.Version,
			Resource: "queues",
		},
	}

	isSpport := false
	for _, gvr := range supportedGVRs {
		if resource == gvr {
			isSpport = true
			break
		}
	}

	if !isSpport {
		return nil, fmt.Errorf("expect resource to be %s", supportedGVRs)
	}

	queue := vcschedulingv1.Queue{}
	if _, _, err := Codecs.UniversalDeserializer().Decode(object.Raw, nil, &queue); err != nil {
		return nil, err
	}

	return &queue, nil
}

// DecodePodGroup decodes the podgroup using deserializer from the raw object.
func DecodePodGroup(object runtime.RawExtension, resource metav1.GroupVersionResource) (*vcschedulingv1.PodGroup, error) {
	supportedGVRs := []metav1.GroupVersionResource{
		{
			Group:    vcschedulingv1.SchemeGroupVersion.Group,
			Version:  vcschedulingv1.SchemeGroupVersion.Version,
			Resource: "podgroups",
		},
		{
			Group:    vcschedulingv1beta1.SchemeGroupVersion.Group,
			Version:  vcschedulingv1beta1.SchemeGroupVersion.Version,
			Resource: "podgroups",
		},
	}

	isSpport := false
	for _, gvr := range supportedGVRs {
		if resource == gvr {
			isSpport = true
			break
		}
	}

	if !isSpport {
		return nil, fmt.Errorf("expect resource to be %s", supportedGVRs)
	}

	podgroup := vcschedulingv1.PodGroup{}
	if _, _, err := Codecs.UniversalDeserializer().Decode(object.Raw, nil, &podgroup); err != nil {
		return nil, err
	}

	return &podgroup, nil
}
