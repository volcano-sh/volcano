package vnpu

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api/devices"
)

type NPUDevice struct { // One VNode/VChip?

}

type NPUDevices struct { //schedulerHandler, including all the scheduler cache
	Name string

	Device map[int]*NPUDevice
}

func NewNPUDevices(name string, node *v1.Node) *NPUDevices {
	return &NPUDevices{}
}

// GetIgnoredDevices return device names which wish vc-scheduler to ignore
func (ns *NPUDevices) GetIgnoredDevices() []string {
	return []string{""}
}

// AddResource adds the pod to NPU pool if it is assigned
func (ns *NPUDevices) AddResource(pod *v1.Pod) {

}

// SubResource frees the npu hold by the pod
func (ns *NPUDevices) SubResource(pod *v1.Pod) {

}

func (ns *NPUDevice) HasDeviceRequest(pod *v1.Pod) bool {
	return true
}

func (ns *NPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	return nil
}

func (ns *NPUDevices) FilterNode(pod *v1.Pod, schedulePolicy string) (int, string, error) {

	return devices.Success, "", nil
}

func (ns *NPUDevices) GetStatus() string {
	return ""
}

func (ns *NPUDevices) ScoreNode(pod *v1.Pod, schedulePolicy string) float64 {
	return 0
}

func (ns *NPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.V(4).Infoln("DeviceSharing:Into AllocateToPod", pod.Name)

	return nil
}
