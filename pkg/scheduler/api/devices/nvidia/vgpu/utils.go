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
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
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
				klog.Error("wrong Node GPU info: ", val)
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
			if len(tmpstr) < 4 {
				klog.Errorf("invalid container device %q: expected at least 4 fields", val)
				continue
			}
			devmem, err := strconv.ParseUint(tmpstr[2], 10, 32)
			if err != nil {
				klog.Errorf("invalid used memory %q in container device %q: %v", tmpstr[2], val, err)
				continue
			}
			devcores, err := strconv.ParseUint(tmpstr[3], 10, 32)
			if err != nil {
				klog.Errorf("invalid used cores %q in container device %q: %v", tmpstr[3], val, err)
				continue
			}
			tmpdev.UUID = tmpstr[0]
			tmpdev.Type = tmpstr[1]
			tmpdev.Usedmem = uint(devmem)
			tmpdev.Usedcores = uint(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	return contdev
}

// DecodePodDevices parses the vgpu-ids-new annotation into per-container device lists.
func DecodePodDevices(str string) []ContainerDevices {
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

// getPodGroupKey returns a unique key for the pod's group (namespace/name of PodGroup),
// or "" if the pod has no group annotation. Used to avoid placing two pods from the same
// PodGroup on the same vGPU device. Uses the same annotation keys as the job/podgroup
// controllers (KubeGroupNameAnnotationKey and VolcanoGroupNameAnnotationKey).
func getPodGroupKey(pod *v1.Pod) string {
	if pod == nil || pod.Annotations == nil {
		return ""
	}
	groupName := pod.Annotations[v1beta1.KubeGroupNameAnnotationKey]
	if groupName == "" {
		groupName = pod.Annotations[v1beta1.VolcanoGroupNameAnnotationKey]
	}
	if groupName == "" {
		return ""
	}
	return pod.Namespace + "/" + groupName
}

// deviceHasPodFromSameGroup returns true if the device already has a pod from the same
// PodGroup as currentKey (and that pod has non-zero usage), so we should not place
// another pod from the same group on this device.
func deviceHasPodFromSameGroup(gd *GPUDevice, currentKey string) bool {
	if gd == nil || currentKey == "" {
		return false
	}
	for _, usage := range gd.PodMap {
		if usage == nil {
			continue
		}
		if usage.PodGroupKey == currentKey && (usage.UsedMem > 0 || usage.UsedCore > 0) {
			return true
		}
	}
	return false
}

func checkVGPUResourcesInPod(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		_, ok := container.Resources.Limits[v1.ResourceName(getConfig().ResourceMemoryName)]
		if ok {
			return true
		}
		_, ok = container.Resources.Limits[v1.ResourceName(getConfig().ResourceCountName)]
		if ok {
			return true
		}
	}
	return false
}

func resourcereqs(pod *v1.Pod) []devices.ContainerDeviceRequest {
	countName := getConfig().ResourceCountName
	memoryName := getConfig().ResourceMemoryName
	percentageName := getConfig().ResourceMemoryPercentageName
	coreName := getConfig().ResourceCoreName
	return devices.ExtractResourceRequest(pod, "NVIDIA", countName, memoryName, percentageName, coreName)
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

type gpuUUIDFilter struct {
	allowlist []string
	denylist  []string
}

func newGPUUUIDFilter(annos map[string]string) gpuUUIDFilter {
	return gpuUUIDFilter{
		allowlist: parseGPUUUIDList(annos[VGPUUseUUIDAnnotation]),
		denylist:  parseGPUUUIDList(annos[VGPUNoUseUUIDAnnotation]),
	}
}

func parseGPUUUIDList(value string) []string {
	var uuids []string
	for _, uuid := range strings.Split(value, ",") {
		if uuid = strings.TrimSpace(uuid); uuid != "" {
			uuids = append(uuids, uuid)
		}
	}
	return uuids
}

func (f gpuUUIDFilter) enabled() bool {
	return len(f.allowlist) > 0 || len(f.denylist) > 0
}

func (f gpuUUIDFilter) matches(id string) bool {
	if id == "" && f.enabled() {
		return false
	}
	if len(f.allowlist) > 0 && !slices.Contains(f.allowlist, id) {
		return false
	}
	return !slices.Contains(f.denylist, id)
}

func checkType(annos map[string]string, d GPUDevice, n devices.ContainerDeviceRequest) bool {
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
			podMapCopy := make(map[string]*GPUUsage, len(val.PodMap))
			for uid, usage := range val.PodMap {
				if usage != nil {
					u := *usage
					podMapCopy[uid] = &u
				}
			}
			ret.Device[index] = &GPUDevice{
				ID:          val.ID,
				Node:        val.Node,
				UUID:        val.UUID,
				PodMap:      podMapCopy,
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

	// if the pod specify the sharing mode but the device is not in the same mode, return not fitted;
	// if the pod does not speficy the sharing mode, any device mode will be fitted
	podSharingMode, ok := pod.Annotations[GPUModeAnnotation]
	if ok && podSharingMode != gssnap.Mode {
		return false, []ContainerDevices{}, 0, fmt.Errorf("pod required sharing mode %s is not the same as the node mode %s", podSharingMode, gssnap.Mode)
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
	var currentPodGroupKey string
	if pod.Annotations[VGPUPodGroupPolicyAnnotation] == VGPUPodGroupPolicySpreadValue {
		currentPodGroupKey = getPodGroupKey(pod)
	}

	type tentativeAlloc struct {
		device *GPUDevice
		mem    uint
		core   uint
	}
	rollbackTentative := func(allocs []tentativeAlloc) {
		for _, alloc := range allocs {
			if alloc.device == nil {
				continue
			}
			if alloc.device.UsedNum > 0 {
				alloc.device.UsedNum--
			}
			if alloc.device.UsedMem >= alloc.mem {
				alloc.device.UsedMem -= alloc.mem
			} else {
				alloc.device.UsedMem = 0
			}
			if alloc.device.UsedCore >= alloc.core {
				alloc.device.UsedCore -= alloc.core
			} else {
				alloc.device.UsedCore = 0
			}
		}
	}

	tentativeAllocs := []tentativeAlloc{}
	ctrdevs := []ContainerDevices{}
	uuidFilter := newGPUUUIDFilter(pod.Annotations)
	for _, val := range ctrReq {
		devs := []ContainerDevice{}
		uuidMatched := false
		if int(val.Nums) > len(gs.Device) {
			rollbackTentative(tentativeAllocs)
			return false, []ContainerDevices{}, 0, fmt.Errorf("no enough gpu cards on node %s", gs.Name)
		}
		klog.V(3).InfoS("Allocating device for container", "request", val)

		for _, i := range sortedDeviceIndicesByPolicy(gs, schedulePolicy) {
			klog.V(3).InfoS("Scoring pod request", "memReq", val.Memreq, "memPercentageReq", val.MemPercentagereq, "coresReq", val.Coresreq, "Nums", val.Nums, "Index", i, "ID", gs.Device[i].ID)
			klog.V(3).InfoS("Current Device", "Index", i, "TotalMemory", gs.Device[i].Memory, "UsedMemory", gs.Device[i].UsedMem, "UsedCores", gs.Device[i].UsedCore, "UsedNum", gs.Device[i].UsedNum, "Number", gs.Device[i].Number, "replicate", replicate)
			if !uuidFilter.matches(gs.Device[i].UUID) {
				continue
			}
			uuidMatched = true
			if gs.Device[i].Number <= uint(gs.Device[i].UsedNum) {
				continue
			}
			if currentPodGroupKey != "" && deviceHasPodFromSameGroup(gs.Device[i], currentPodGroupKey) {
				continue
			}
			memreqForCard := uint(0)
			// if we have mempercentage request, we ignore the mem request for every cards
			if val.MemPercentagereq != 101 {
				memreqForCard = uint(float64(gs.Device[i].Memory) * float64(val.MemPercentagereq) / 100.0)
			} else {
				memreqForCard = uint(val.Memreq)
			}
			if int(gs.Device[i].Memory)-int(gs.Device[i].UsedMem) < int(memreqForCard) {
				continue
			}
			if gs.Device[i].UsedCore+uint(val.Coresreq) > 100 {
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
			fit, uuid := gs.Sharing.TryAddPod(gs.Device[i], memreqForCard, uint(val.Coresreq))
			if !fit {
				klog.V(3).Info(gs.Device[i].ID, "not fit")
				continue
			}
			tentativeAllocs = append(tentativeAllocs, tentativeAlloc{
				device: gs.Device[i],
				mem:    memreqForCard,
				core:   uint(val.Coresreq),
			})
			//total += gs.Devices[i].Count
			//free += node.Devices[i].Count - node.Devices[i].Used
			if val.Nums > 0 {
				val.Nums--
				klog.V(3).Info("fitted uuid: ", uuid)
				devs = append(devs, ContainerDevice{
					UUID:      uuid,
					Type:      val.Type,
					Usedmem:   memreqForCard,
					Usedcores: uint(val.Coresreq),
				})
				score += GPUScore(schedulePolicy, gs.Device[i])
			}
			if val.Nums == 0 {
				break
			}
		}
		if val.Nums > 0 {
			rollbackTentative(tentativeAllocs)
			if uuidFilter.enabled() && !uuidMatched {
				return false, []ContainerDevices{}, 0, fmt.Errorf(
					"no GPU matches UUID constraints %s=%q, %s=%q on node %s",
					VGPUUseUUIDAnnotation, pod.Annotations[VGPUUseUUIDAnnotation],
					VGPUNoUseUUIDAnnotation, pod.Annotations[VGPUNoUseUUIDAnnotation],
					gs.Name,
				)
			}
			return false, []ContainerDevices{}, 0, fmt.Errorf("not enough gpu fitted on this node")
		}
		ctrdevs = append(ctrdevs, devs)
	}
	return true, ctrdevs, score, nil
}

func sortedDeviceIndicesByPolicy(gs *GPUDevices, schedulePolicy string) []int {
	n := len(gs.Device)
	idx := make([]int, 0, n)
	switch schedulePolicy {
	case binpackPolicy:
		for i := range gs.Device {
			idx = append(idx, i)
		}
		sort.Slice(idx, func(a, b int) bool {
			da, db := gs.Device[idx[a]], gs.Device[idx[b]]
			if da.UsedMem != db.UsedMem {
				return da.UsedMem > db.UsedMem
			}
			return idx[a] < idx[b]
		})
	case spreadPolicy:
		for i := range gs.Device {
			idx = append(idx, i)
		}
		sort.Slice(idx, func(a, b int) bool {
			da, db := gs.Device[idx[a]], gs.Device[idx[b]]
			if da.UsedNum != db.UsedNum {
				return da.UsedNum < db.UsedNum
			}
			return idx[a] < idx[b]
		})
	default:
		for i := n - 1; i >= 0; i-- {
			idx = append(idx, i)
		}
	}
	return idx
}

func GPUScore(schedulePolicy string, device *GPUDevice) float64 {
	var score float64
	switch schedulePolicy {
	case binpackPolicy:
		score = binpackMultiplier * (float64(device.UsedMem) / float64(device.Memory))
	case spreadPolicy:
		if device.UsedNum == 0 {
			score = spreadMultiplier
		}
	default:
		score = float64(0)
	}
	return score
}

func getConfig() config.NvidiaConfig {
	if config.GetConfig() != nil {
		return config.GetConfig().NvidiaConfig
	}
	return config.GetDefaultDevicesConfig().NvidiaConfig
}
