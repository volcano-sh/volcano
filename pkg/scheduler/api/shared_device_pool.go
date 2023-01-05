package api

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	GPUSharingDevice = "GpuShare"
)

type SharedDevicePool interface {
	//following two functions used in node_info
	AddResource(pod *v1.Pod)
	SubResource(pod *v1.Pod)

	//following four functions used in predicate
	RequestInPod(pod *v1.Pod) bool
	FitInPod(pod *v1.Pod) (bool, error)
	AllocateToPod(kubeClient kubernetes.Interface, pod *v1.Pod) error
	ReleaseFromPod(kubeClient kubernetes.Interface, pod *v1.Pod) error

	//used for debug and monitor
	MonitorStatus() string
}
