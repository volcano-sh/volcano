package vnpu

import (
	"errors"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
)

type NPUDevices struct { //schedulerHandler, including all the scheduler cache
	Name string

	NodeInf

	NPUDevice

	FrameAttr VolcanoFrame
}

func NewNPUDevices(name string, node *v1.Node) *NPUDevices {
	return &NPUDevices{}
}

// AddResource adds the pod to NPU pool if it is assigned
func (ns *NPUDevices) AddResource(pod *v1.Pod) {
	//Upper level mechanism has assured that the pod(task) has been scheduled to this node
	//Judge if the pod has resource request on this device
	podRes, err := ns.GetPodResource(pod)
	if err != nil {
		klog.V(LogErrorLev).Infof("%s require get task resource failed: %s",
			ns.NodeInf.Name, err)
	}

	// to judge whether this pod has been downgraded in this node
	_, ok := ns.DowngradeCache[pod.Name]
	if ok {
		podRes = ns.downgradeTaskAICPU(podRes)
	}
	ascendNPUCoreSplit := strings.Split(pod.Annotations[AscendNPUCore], "-")

	var allocChipID, chipVTemplate string

	if len(ascendNPUCoreSplit) == 2 {
		allocChipID, chipVTemplate = ascendNPUCoreSplit[0], ascendNPUCoreSplit[1]
		ns.UpdateNodeInfoSegmentWithAdd(allocChipID, podRes)
	} else {
		allocChipID = ascendNPUCoreSplit[0]
		ns.UpdateNodeInfoWholeWithAdd(allocChipID)
	}

	if addErr := ns.addTaskInConCache(pod, podRes, chipVTemplate); addErr != nil {
		klog.V(LogErrorLev).Infof("dynamic vnpu %s addResource addTaskInConCache:%s", pod.Name, addErr)
	}
}

// SubResource frees the npu hold by the pod
func (ns *NPUDevices) SubResource(pod *v1.Pod) {
	//Upper level mechanism has assured that the pod(task) has been scheduled to this node
	podRes, err := ns.GetPodResource(pod)
	if err != nil {
		klog.V(LogErrorLev).Infof("%s require get task resource failed: %s",
			ns.NodeInf.Name, err)
	}

	// to judge whether this pod has been downgraded in this node
	_, ok := ns.DowngradeCache[pod.Name]
	if ok {
		podRes = ns.downgradeTaskAICPU(podRes)
	}
	ascendNPUCoreSplit := strings.Split(pod.Annotations[AscendNPUCore], "-")

	var allocChipID, chipVTemplate string

	if len(ascendNPUCoreSplit) == 2 {
		allocChipID, chipVTemplate = ascendNPUCoreSplit[0], ascendNPUCoreSplit[1]
		ns.UpdateNodeInfoSegmentWithSub(allocChipID, podRes)
	} else {
		allocChipID = ascendNPUCoreSplit[0]
		ns.UpdateNodeInfoWholeWithSub(allocChipID)
	}

	if addErr := ns.releaseTaskInConCache(pod, podRes, chipVTemplate); addErr != nil {
		klog.V(LogErrorLev).Infof("dynamic vnpu %s addResource addTaskInConCache:%s", pod.Name, addErr)
	}
}

func (ns *NPUDevices) HasDeviceRequest(pod *v1.Pod) bool {
	if Ascend310pvNPUEnable && checkVNPUResourcesInPod(pod) {
		return true
	}
	return false
}

func (ns *NPUDevices) FilterNode(pod *v1.Pod, schedulePolicy string) (int, string, error) {
	if err := ns.preCheckNodePredicate(pod); err != nil {
		return devices.Error, "preCheckNodePredicate failure", err
	}
	if err := ns.CheckNodeNPUByPod(pod); err != nil {
		// node doesn't have enough npu for the task
		klog.V(LogDebugLev).Infof("checkNPUByTask %s:%s ,cannot be selected.", ns.NodeInf.Name, SafePrint(err))
		return devices.Error, "", err
	}

	return devices.Success, "", nil
}

func (ns *NPUDevices) ScoreNode(pod *v1.Pod, schedulePolicy string) float64 {
	return 0
}

//func (ns *NPUDevices) ScoreBatchNodes(pod *v1.Pod, schedulePolicy string, nodeInfos []*NodeInf, npuDevices []*NPUDevice) []float64 {
//	return 0
//}

func (ns *NPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.V(4).Infoln("DeviceSharing:Into AllocateToPod", pod.Name)
	if ns == nil {
		klog.V(LogDebugLev).Infof("UseAnnotation failed: %s", ArgumentError)
		return errors.New(ArgumentError)
	}

	podResReq, err := ns.GetPodResource(pod)
	if err != nil {
		klog.V(LogErrorLev).Infof("%s UseAnnotation get require task resource failed: %s", ns.Name, err)
	}

	_, ok := ns.DowngradeCache[pod.Name]
	if ok {
		podResReq = ns.downgradeTaskAICPU(podResReq)
	}

	allocChipID, err := ns.SelectChipFromNode(podResReq)
	if err != nil {
		klog.V(LogErrorLev).Infof("UseAnnotation dynamic %s on %s err: %s", pod.Name, ns.NodeInf.Name, err)
		return err
	}
	klog.V(LogDebugLev).Infof("dynamic vnpu UseAnnotation allocChipID:<%s>", allocChipID)
	ns.SetNPUTopologyToPodFn(pod, podResReq, allocChipID, ns.VT)
	return nil
}

func (ns *NPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	return nil
}

func (ns *NPUDevices) GetStatus() string {
	return ""
}

// GetIgnoredDevices return device names which wish vc-scheduler to ignore
func (ns *NPUDevices) GetIgnoredDevices() []string {
	return []string{""}
}
