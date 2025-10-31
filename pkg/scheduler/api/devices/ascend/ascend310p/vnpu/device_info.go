/*
Copyright 2025 The Volcano Authors.

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

package vnpu

import (
	"strings"
	"sync"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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
	return &NPUDevices{
		Name: name,
		NodeInf: NodeInf{
			Name:       name,
			Annotation: make(map[string]string),
			Label:      make(map[string]string),
			Capability: make(map[v1.ResourceName]float64),
			Allocate:   make(map[v1.ResourceName]float64),
			Idle:       make(map[v1.ResourceName]float64),
		},
		NPUDevice: NPUDevice{
			VT: VTemplate{
				Temp: Ascend310P,
				Data: map[string]VResource{
					VNPUTempVir01:        {Aicore: 1, Aicpu: 1, DVPP: AscendDVPPEnabledNull},
					VNPUTempVir02:        {Aicore: NPUIndex2, Aicpu: NPUIndex2, DVPP: AscendDVPPEnabledNull},
					VNPUTempVir02C1:      {Aicore: NPUIndex2, Aicpu: 1, DVPP: AscendDVPPEnabledNull},
					VNPUTempVir04:        {Aicore: NPUIndex4, Aicpu: NPUIndex4, DVPP: AscendDVPPEnabledNull},
					VNPUTempVir04C3:      {Aicore: NPUIndex4, Aicpu: NPUIndex3, DVPP: AscendDVPPEnabledNull},
					VNPUTempVir04C3NDVPP: {Aicore: NPUIndex4, Aicpu: NPUIndex3, DVPP: AscendDVPPEnabledOff},
					VNPUTempVir04C4cDVPP: {Aicore: NPUIndex4, Aicpu: NPUIndex4, DVPP: AscendDVPPEnabledOn},
				},
			},
			Chips:            make(map[int]*VChip),
			UnhealthyChipIds: make(map[int]struct{}),
			DowngradeCache:   make(map[string]struct{}, MapInitNum),
			ConCache:         make(map[string]map[types.UID]struct{}),
		},
		FrameAttr: VolcanoFrame{
			VJobTemplate: make(map[string]map[string]VResource),
			ConfigParameters: ConfigParameters{
				StaticParameters: StaticParameters{
					OnceInit:       &sync.Once{},
					IsFirstSession: PtrInit(true),
				},
			},
		},
	}
}

// AddResource adds the pod to NPU pool if it is assigned
func (ns *NPUDevices) AddResource(pod *v1.Pod) {
	if !ns.HasDeviceRequest(pod) {
		return
	}

	//Upper level mechanism has assured that the pod(task) has been scheduled to this node
	//Judge if the pod has resource request on this device
	podRes, err := ns.GetPodResource(pod)
	if err != nil {
		klog.V(LogErrorLev).Infof("%s require get task resource failed: %s",
			ns.NodeInf.Name, err)
	}

	coreAnnotation, ok := pod.Annotations[AscendNPUCore]
	if !ok {
		return
	}
	ascendNPUCoreSplit := strings.Split(coreAnnotation, "-")

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
	if !ns.HasDeviceRequest(pod) {
		return
	}

	//Upper level mechanism has assured that the pod(task) has been scheduled to this node
	podRes, err := ns.GetPodResource(pod)
	if err != nil {
		klog.V(LogErrorLev).Infof("%s require get task resource failed: %s",
			ns.NodeInf.Name, err)
	}

	coreAnnotation, ok := pod.Annotations[AscendNPUCore]
	if !ok {
		return
	}

	ascendNPUCoreSplit := strings.Split(coreAnnotation, "-")

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

func (ns *NPUDevices) AddQueueResource(pod *v1.Pod) map[string]float64 {
	return map[string]float64{}
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
	// implement in deviceShare plugin score policy
	return 0
}

func (ns *NPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.V(4).Infoln("DeviceSharing:Into AllocateToPod", pod.Name)
	if ns == nil {
		klog.V(LogDebugLev).Infof("UseAnnotation failed: %s", ArgumentError)
		return errors.New(ArgumentError)
	}

	podResReq, err := ns.GetPodResource(pod)
	if err != nil {
		klog.V(LogErrorLev).Infof("%s UseAnnotation get require task resource failed: %s", ns.Name, err)
		return err
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
	ns.SetNPUTopologyToPodFn(kubeClient, pod, podResReq, allocChipID, ns.VT)
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
