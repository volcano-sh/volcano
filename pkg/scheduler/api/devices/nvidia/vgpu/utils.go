/*
Copyright 2023 The Volcano Authors.

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

package vgpu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

func extractGeometryFromType(t string) ([]config.Geometry, error) {
	if config.GetConfig() != nil {
		for _, val := range config.GetConfig().NvidiaConfig.MigGeometriesList {
			found := false
			for _, migDevType := range val.Models {
				if strings.Contains(t, migDevType) {
					found = true
				}
			}
			if found {
				return val.Geometries, nil
			}
		}
	}
	return []config.Geometry{}, errors.New("mig type not found")
}

func decodeNodeDevices(name, str string) (*GPUDevices, string) {
	if !strings.Contains(str, ":") {
		return nil, ""
	}
	tmp := strings.Split(str, ":")
	retval := &GPUDevices{
		Name:   name,
		Device: make(map[int]*GPUDevice),
		Score:  float64(0),
	}
	sharingMode := vGPUControllerHAMICore
	for index, val := range tmp {
		if strings.Contains(val, ",") {
			items := strings.Split(val, ",")
			if len(items) < 6 {
				klog.Error("wrong Pod GPU info: ", val)
				return nil, ""
			}
			count, _ := strconv.Atoi(items[1])
			devmem, _ := strconv.Atoi(items[2])
			health, _ := strconv.ParseBool(items[4])
			i := GPUDevice{
				ID:          index,
				Node:        name,
				UUID:        items[0],
				Number:      uint(count),
				Memory:      uint(devmem),
				Type:        items[3],
				PodMap:      make(map[string]*GPUUsage),
				Health:      health,
				MigTemplate: []config.Geometry{},
				MigUsage: config.MigInUse{
					Index: -1},
			}
			sharingMode = getSharingMode(items[5])
			if sharingMode == vGPUControllerMIG {
				var err error
				i.MigTemplate, err = extractGeometryFromType(i.Type)
				if err != nil {
					sharingMode = vGPUControllerHAMICore
					klog.ErrorS(err, "extract mig geometry error and fall back to hamicore mode")
				}
			}
			retval.Device[index] = &i
		}
	}
	retval.Mode = sharingMode
	return retval, sharingMode
}

func encodeContainerDevices(cd []ContainerDevice) string {
	tmp := ""
	for _, val := range cd {
		tmp += val.UUID + "," + val.Type + "," + strconv.Itoa(int(val.Usedmem)) + "," + strconv.Itoa(int(val.Usedcores)) + ":"
	}
	klog.V(4).Infoln("Encoded container Devices=", tmp)
	return tmp
	//return strings.Join(cd, ",")
}

func encodePodDevices(pd []ContainerDevices) string {
	var ss []string
	for _, cd := range pd {
		ss = append(ss, encodeContainerDevices(cd))
	}
	return strings.Join(ss, ";")
}

func decodeContainerDevices(str string) ContainerDevices {
	if len(str) == 0 {
		return ContainerDevices{}
	}
	cd := strings.Split(str, ":")
	contdev := ContainerDevices{}
	tmpdev := ContainerDevice{}
	if len(str) == 0 {
		return contdev
	}
	for _, val := range cd {
		if strings.Contains(val, ",") {
			tmpstr := strings.Split(val, ",")
			tmpdev.UUID = tmpstr[0]
			tmpdev.Type = tmpstr[1]
			devmem, _ := strconv.ParseInt(tmpstr[2], 10, 32)
			tmpdev.Usedmem = uint(devmem)
			devcores, _ := strconv.ParseInt(tmpstr[3], 10, 32)
			tmpdev.Usedcores = uint(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	return contdev
}

func decodePodDevices(str string) []ContainerDevices {
	if len(str) == 0 {
		return []ContainerDevices{}
	}
	var pd []ContainerDevices
	for _, s := range strings.Split(str, ";") {
		cd := decodeContainerDevices(s)
		pd = append(pd, cd)
	}
	return pd
}

func checkVGPUResourcesInPod(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		_, ok := container.Resources.Limits[config.VolcanoVGPUMemory]
		if ok {
			return true
		}
		_, ok = container.Resources.Limits[config.VolcanoVGPUNumber]
		if ok {
			return true
		}
	}
	return false
}

func resourcereqs(pod *v1.Pod) []ContainerDeviceRequest {
	resourceName := v1.ResourceName(config.VolcanoVGPUNumber)
	resourceMem := v1.ResourceName(config.VolcanoVGPUMemory)
	resourceMemPercentage := v1.ResourceName(config.VolcanoVGPUMemoryPercentage)
	resourceCores := v1.ResourceName(config.VolcanoVGPUCores)
	counts := []ContainerDeviceRequest{}

	//Count Nvidia GPU
	for i := 0; i < len(pod.Spec.Containers); i++ {
		singledevice := false
		v, ok := pod.Spec.Containers[i].Resources.Limits[resourceName]
		if !ok {
			v, ok = pod.Spec.Containers[i].Resources.Limits[resourceMem]
			singledevice = true
		}
		if ok {
			n := int64(1)
			if !singledevice {
				n, _ = v.AsInt64()
			}
			memnum := uint(0)
			mem, ok := pod.Spec.Containers[i].Resources.Limits[resourceMem]
			if !ok {
				mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = uint(memnums)
				}
			}
			mempnum := int32(101)
			mem, ok = pod.Spec.Containers[i].Resources.Limits[resourceMemPercentage]
			if !ok {
				mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMemPercentage]
			}
			if ok {
				mempnums, ok := mem.AsInt64()
				if ok {
					mempnum = int32(mempnums)
				}
			}
			if mempnum == 101 && memnum == 0 {
				mempnum = 100
			}
			corenum := uint(0)
			core, ok := pod.Spec.Containers[i].Resources.Limits[resourceCores]
			if !ok {
				core, ok = pod.Spec.Containers[i].Resources.Requests[resourceCores]
			}
			if ok {
				corenums, ok := core.AsInt64()
				if ok {
					corenum = uint(corenums)
				}
			}
			counts = append(counts, ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             "NVIDIA",
				Memreq:           memnum,
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			})
		}
	}
	klog.V(3).Infoln("counts=", counts)
	return counts
}

func checkGPUtype(annos map[string]string, cardtype string) bool {
	inuse, ok := annos[GPUInUse]
	if ok {
		if !strings.Contains(inuse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(inuse)) {
				return true
			}
		} else {
			for _, val := range strings.Split(inuse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return true
				}
			}
		}
		return false
	}
	nouse, ok := annos[GPUNoUse]
	if ok {
		if !strings.Contains(nouse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(nouse)) {
				return false
			}
		} else {
			for _, val := range strings.Split(nouse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return false
				}
			}
		}
		return true
	}
	return true
}

func checkType(annos map[string]string, d GPUDevice, n ContainerDeviceRequest) bool {
	//General type check, NVIDIA->NVIDIA MLU->MLU
	if !strings.Contains(d.Type, n.Type) {
		return false
	}
	if n.Type == NvidiaGPUDevice {
		return checkGPUtype(annos, d.Type)
	}
	klog.Errorf("Unrecognized device %v", n.Type)
	return false
}

// getGPUDeviceSnapShot is not a strict deep copy, the pointer item is same with origin.
func getGPUDeviceSnapShot(snap *GPUDevices) *GPUDevices {
	ret := GPUDevices{
		Name:    snap.Name,
		Device:  make(map[int]*GPUDevice),
		Score:   float64(0),
		Sharing: snap.Sharing,
	}
	for index, val := range snap.Device {
		if val != nil {
			ret.Device[index] = &GPUDevice{
				ID:          val.ID,
				Node:        val.Node,
				UUID:        val.UUID,
				PodMap:      val.PodMap,
				Memory:      val.Memory,
				Number:      val.Number,
				Type:        val.Type,
				Health:      val.Health,
				UsedNum:     val.UsedNum,
				UsedMem:     val.UsedMem,
				UsedCore:    val.UsedCore,
				MigTemplate: val.MigTemplate,
				MigUsage:    val.MigUsage,
			}
			ret.Device[index].MigUsage = deepCopyMigInUse(val.MigUsage)
			klog.V(4).Infoln("getGPUDeviceSnapShot:", ret.Device[index].UsedMem, val.UsedMem, ret.Device[index].MigUsage, val.MigUsage)
		}
	}
	return &ret
}

func deepCopyMigInUse(src config.MigInUse) config.MigInUse {
	dst := config.MigInUse{
		Index: src.Index,
	}

	dst.UsageList = make(config.MIGS, len(src.UsageList))
	for i, usage := range src.UsageList {
		dst.UsageList[i] = config.MigTemplateUsage{
			Name:      usage.Name,
			Memory:    usage.Memory,
			InUse:     usage.InUse,
			UsedIndex: make([]int, len(usage.UsedIndex)),
		}
		copy(dst.UsageList[i].UsedIndex, usage.UsedIndex)
	}

	return dst
}

// getSharingMode by default, we use hami core as the partitioning mode
func getSharingMode(mode string) string {
	switch mode {
	case vGPUControllerMIG:
		return vGPUControllerMIG
	default:
		return vGPUControllerHAMICore
	}
}

// checkNodeGPUSharingPredicate checks if a pod with gpu requirement can be scheduled on a node.
func checkNodeGPUSharingPredicateAndScore(pod *v1.Pod, gssnap *GPUDevices, replicate bool, schedulePolicy string) (bool, []ContainerDevices, float64, error) {
	// no gpu sharing request
	score := float64(0)
	if !checkVGPUResourcesInPod(pod) {
		return true, []ContainerDevices{}, 0, nil
	}
	podSharingMode, ok := pod.Annotations[GPUModeAnnotation]
	if !ok {
		podSharingMode = vGPUControllerHAMICore
	}
	podSharingMode = getSharingMode(podSharingMode)
	// if the pod requires mig but the device is not in mig mode, return not fitted;
	// if the pod does not speficy the sharing mode, then any device mode will be fitted
	if podSharingMode == vGPUControllerMIG && gssnap.Mode != vGPUControllerMIG {
		return false, []ContainerDevices{}, 0, fmt.Errorf("pod required MIG but the node is not in MIG mode")
	}
	ctrReq := resourcereqs(pod)
	if len(ctrReq) == 0 {
		return true, []ContainerDevices{}, 0, nil
	}

	var gs *GPUDevices
	if replicate {
		gs = getGPUDeviceSnapShot(gssnap)
	} else {
		gs = gssnap
	}
	ctrdevs := []ContainerDevices{}
	for _, val := range ctrReq {
		devs := []ContainerDevice{}
		if int(val.Nums) > len(gs.Device) {
			return false, []ContainerDevices{}, 0, fmt.Errorf("no enough gpu cards on node %s", gs.Name)
		}
		klog.V(3).InfoS("Allocating device for container", "request", val)

		for i := len(gs.Device) - 1; i >= 0; i-- {
			klog.V(3).InfoS("Scoring pod request", "memReq", val.Memreq, "memPercentageReq", val.MemPercentagereq, "coresReq", val.Coresreq, "Nums", val.Nums, "Index", i, "ID", gs.Device[i].ID)
			klog.V(3).InfoS("Current Device", "Index", i, "TotalMemory", gs.Device[i].Memory, "UsedMemory", gs.Device[i].UsedMem, "UsedCores", gs.Device[i].UsedCore, "replicate", replicate)
			if gs.Device[i].Number <= uint(gs.Device[i].UsedNum) {
				continue
			}
			if val.MemPercentagereq != 101 && val.Memreq == 0 {
				val.Memreq = gs.Device[i].Memory * uint(val.MemPercentagereq/100)
			}
			if int(gs.Device[i].Memory)-int(gs.Device[i].UsedMem) < int(val.Memreq) {
				continue
			}
			if gs.Device[i].UsedCore+val.Coresreq > 100 {
				continue
			}
			// Coresreq=100 indicates it want this card exclusively
			if val.Coresreq == 100 && gs.Device[i].UsedNum > 0 {
				continue
			}
			// You can't allocate core=0 job to an already full GPU
			if gs.Device[i].UsedCore == 100 && val.Coresreq == 0 {
				continue
			}
			if !checkType(pod.Annotations, *gs.Device[i], val) {
				klog.Errorln("failed checktype", gs.Device[i].Type, val.Type)
				continue
			}
			fit, uuid := gs.Sharing.TryAddPod(gs.Device[i], uint(val.Memreq), uint(val.Coresreq))
			if !fit {
				klog.V(3).Info(gs.Device[i].ID, "not fit")
				continue
			}
			//total += gs.Devices[i].Count
			//free += node.Devices[i].Count - node.Devices[i].Used
			if val.Nums > 0 {
				val.Nums--
				klog.V(3).Info("fitted uuid: ", uuid)
				devs = append(devs, ContainerDevice{
					UUID:      uuid,
					Type:      val.Type,
					Usedmem:   val.Memreq,
					Usedcores: val.Coresreq,
				})
				score += GPUScore(schedulePolicy, gs.Device[i])
			}
			if val.Nums == 0 {
				break
			}
		}
		if val.Nums > 0 {
			return false, []ContainerDevices{}, 0, fmt.Errorf("not enough gpu fitted on this node")
		}
		ctrdevs = append(ctrdevs, devs)
	}
	return true, ctrdevs, score, nil
}

func GPUScore(schedulePolicy string, device *GPUDevice) float64 {
	var score float64
	switch schedulePolicy {
	case binpackPolicy:
		score = binpackMultiplier * (float64(device.UsedMem) / float64(device.Memory))
	case spreadPolicy:
		if device.UsedNum == 1 {
			score = spreadMultiplier
		}
	default:
		score = float64(0)
	}
	return score
}

func patchPodAnnotations(kubeClient kubernetes.Interface, pod *v1.Pod, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
		//Spec     patchSpec     `json:"spec,omitempty"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = kubeClient.CoreV1().Pods(pod.Namespace).
		Patch(context.Background(), pod.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("patch pod %v failed, %v", pod.Name, err)
	}

	return err
}

func patchNodeAnnotations(node *v1.Node, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchNode struct {
		Metadata patchMetadata `json:"metadata"`
		//Spec     patchSpec     `json:"spec,omitempty"`
	}

	p := patchNode{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = devices.GetClient().CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("patch node %v failed, %v", node.Name, err)
	}
	return err
}
