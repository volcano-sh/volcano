package schema

import (
	"fmt"

	"k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog"
	corev1 "k8s.io/kubernetes/pkg/apis/core/v1"

	batchv1alpha1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	schedulingv1alpha2 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
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
	v1beta1.AddToScheme(scheme)
}

//DecodeJob decodes the job using deserializer from the raw object
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

// DecodeQueue decodes the queue using deserializer from the raw object
func DecodeQueue(object runtime.RawExtension, resource metav1.GroupVersionResource) (*schedulingv1alpha2.Queue, error) {
	queueResource := metav1.GroupVersionResource{
		Group:    schedulingv1alpha2.SchemeGroupVersion.Group,
		Version:  schedulingv1alpha2.SchemeGroupVersion.Version,
		Resource: "queues",
	}

	if resource != queueResource {
		return nil, fmt.Errorf("expect resource to be %s", queueResource)
	}

	queue := schedulingv1alpha2.Queue{}
	if _, _, err := Codecs.UniversalDeserializer().Decode(object.Raw, nil, &queue); err != nil {
		return nil, err
	}

	return &queue, nil
}
