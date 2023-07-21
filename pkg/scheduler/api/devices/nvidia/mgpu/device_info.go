package mgpu

import (
	"context"
	"encoding/json"
	"fmt"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"strconv"
	"time"
	"volcano.sh/volcano/pkg/scheduler/api/devices"
)

// DefaultNumThatPolicyValid default DefaultNumThatPolicyValid number
const DefaultNumThatPolicyValid = 5

// GPUDevice include gpu id, memory and the pods that are sharing it.
type GPUDevice struct {
	// GPU Index
	Index int
	// gpu-core per card available
	CoreAvailable int
	// gpu-memory per card available
	MemoryAvailable int
	// gpu-core per card total
	CoreTotal int
	// gpu-memory per card total
	MemoryTotal int
	// container counts of the card
	ContainerCount int
	// MGPUInstanceCount is the num of mgpu-instance in multiple mode
	MGPUInstanceCount int
	// MultiUsedContainers records the container key which used multiple mode
	MultiUsedContainers map[string]int
	// The pods that are sharing this GPU
	//PodMap map[string]*v1.Pod
}

type GPUs []*GPUDevice

type GPUDevices struct {
	Name string
	Rater
	NodePolicy string
	// Pod2OptionMap is the map for pod and it's option
	Pod2OptionMap map[string]*GPUOption
	GPUs          GPUs
	CoreName      v1.ResourceName
	MemName       v1.ResourceName
}

// NewGPUDevice creates a device
func NewGPUDevice(index int, mem int) *GPUDevice {
	return &GPUDevice{
		Index:               index,
		CoreAvailable:       GPUCoreEachCard,
		CoreTotal:           GPUCoreEachCard,
		MemoryAvailable:     mem,
		MemoryTotal:         mem,
		ContainerCount:      0,
		MGPUInstanceCount:   0,
		MultiUsedContainers: make(map[string]int),
	}
}

func NewGPUDevices(name string, node *v1.Node) *GPUDevices {
	if node == nil {
		return nil
	}
	rater := getRater()
	devices := decodeNodeDevices(name, node, rater)

	return devices
}

// AddResource adds the pod to GPU pool if it is assigned
func (gs *GPUDevices) AddResource(pod *v1.Pod) {
	if !isSharedMGPUPod(pod) {
		return
	}
	klog.V(3).Infof("Start to add pod %s/%s", pod.Namespace, pod.Name)
	podName := getPodNamespaceName(pod)
	if _, ok := gs.Pod2OptionMap[podName]; !ok {
		option := NewGPUOptionFromPod(pod, VKEResourceMGPUCore, VKEResourceMGPUMemory)
		if option.Allocated != nil && option.Allocated[0] == nil {
			return
		}
		gs.Pod2OptionMap[podName] = option
		klog.V(5).Infof("Add pod %s/%s option: %+v", pod.Namespace, pod.Name, option)
		if err := gs.GPUs.Transact(pod, option); err != nil {
			delete(gs.Pod2OptionMap, podName)
		}
	}
	klog.V(3).Infof("gs: %+v", gs.GPUs.ToString())
}

// SubResource frees the gpu hold by the pod
func (gs *GPUDevices) SubResource(pod *v1.Pod) {
	if !isSharedMGPUPod(pod) {
		return
	}
	klog.Infof("Start to forget pod %s/%s", pod.Namespace, pod.Name)
	podName := getPodNamespaceName(pod)
	option := NewGPUOptionFromPod(pod, VKEResourceMGPUCore, VKEResourceMGPUMemory)
	//option, ok := gs.Pod2OptionMap[podName]
	//if !ok {
	//	return
	//}
	if option.Allocated != nil && option.Allocated[0] == nil {
		return
	}
	if klog.V(3).Enabled() {
		klog.Infof("Cancel pod %s/%s option %+v on %+v", pod.Namespace, pod.Name, option, gs.GPUs.ToString())
	}
	gs.GPUs.Cancel(pod, option)
	//if klog.V(3).Enabled() {
	klog.Infof("After Cancel, Current GPU allocation of node %s: %+v", gs.Name, gs.GPUs.ToString())
	//}
	delete(gs.Pod2OptionMap, podName)
}

// HasDeviceRequest checks if the 'pod' request this device
func (gs *GPUDevices) HasDeviceRequest(pod *v1.Pod) bool {
	return MGPUEnable && checkMGPUResourceInPod(pod)
}

// FilterNode checks if the 'pod' fit in current node
func (gs *GPUDevices) FilterNode(pod *v1.Pod) (int, string, error) {
	klog.Infoln("MGPU DeviceSharing: Into FitInPod", pod.Name)
	if MGPUEnable {
		fit, err := checkNodeMGPUSharingPredicate(pod, gs)
		if err != nil || !fit {
			klog.Errorln("deviceSharing err=", err.Error())
			return devices.Unschedulable, fmt.Sprintf("mgpuDevice %s", err.Error()), err
		}
	}
	klog.V(3).Infoln("vke.volcengine.com mGPU DeviceSharing: FitInPod succeed", pod.Name)
	return devices.Success, "", nil
}

// Allocate action in predicate
func (gs *GPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.Infoln("MGPU DeviceSharing:Into AllocateToPod", pod.Name)
	if MGPUEnable {
		fit, err := checkNodeMGPUSharingPredicate(pod, gs)
		if err != nil || !fit {
			klog.Errorln("DeviceSharing err=", err.Error())
			return err
		}
		if klog.V(5).Enabled() {
			klog.Infof("GPU Devices: %+v\n", gs.GPUs.ToString())
		}
		var (
			req   GPURequest
			cmReq ContainerMultiGPURequest
		)
		req, cmReq = NewGPURequest(pod, VKEResourceMGPUCore, VKEResourceMGPUMemory)
		// Check container specific number whether exceed the node's GPU number
		gpuCountList, _ := ExtraMultipleGPUCountList(pod)
		if len(gpuCountList) > 0 {
			for i, count := range gpuCountList {
				if count > len(gs.GPUs) {
					return fmt.Errorf("request multiple GPU count %d is exceed the allocatable GPU number, container index: %d", count, i+1)
				}
			}
		}

		// Achieve the option of GPU for pod's containers
		option, err := gs.GPUs.Trade(gs.Rater, req, pod, cmReq)
		if err != nil {
			return err
		}
		if err := gs.Add(pod, option); err != nil {
			return fmt.Errorf("add pod to node allocator failed: %v", err)
		}

		// patch GPU annotations and labels to the pod
		if err := patchGPUInfo(kubeClient, pod, option); err != nil {
			return fmt.Errorf("patch mgpu annotations and labels to pod failed: %v", err)
		}
		klog.V(3).Infoln("DeviceSharing:Allocate Success")
	}
	if klog.V(5).Enabled() {
		klog.Infof("Allocated %s successfully: %s: %+v\n", pod.Name, gs.Name, gs.GPUs.ToString())
	}
	return nil
}

// Release action in predicate
func (gs *GPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.Infoln("MGPU DeviceSharing:Into ReleaseToPod", pod.Name)
	podName := GetPodNamespaceName(pod)
	option, ok := gs.Pod2OptionMap[podName]
	if !ok {
		return fmt.Errorf("")
	}
	if klog.V(3).Enabled() {
		klog.Infof("Cancel pod %s/%s option %+v on %+v", pod.Namespace, pod.Name, option, gs.GPUs.ToString())
	}
	gs.GPUs.Cancel(pod, option)
	if klog.V(3).Enabled() {
		klog.Infof("After Cancel, Current GPU allocation of node %s: %+v", gs.Name, gs.GPUs.ToString())
	}

	return nil
}

// GetIgnoredDevices notify vc-scheduler to ignore devices in return list
func (gs *GPUDevices) GetIgnoredDevices() []string {
	return []string{""}
}

// GetStatus used for debug and monitor
func (gs *GPUDevices) GetStatus() string {
	return ""
}

// Add adds a Pod
func (gs *GPUDevices) Add(pod *v1.Pod, option *GPUOption) error {
	podName := GetPodNamespaceName(pod)
	if _, ok := gs.Pod2OptionMap[podName]; !ok {
		if option == nil {
			option = NewGPUOptionFromPod(pod, gs.CoreName, gs.MemName)
		}
		gs.Pod2OptionMap[podName] = option
		klog.V(5).Infof("Add pod %s/%s option: %+v", pod.Namespace, pod.Name, option)
		if err := gs.GPUs.Transact(pod, option); err != nil {
			delete(gs.Pod2OptionMap, podName)
			return err
		}
	}
	return nil
}

// Transact processes the GPUOption to GPUs in cache.
func (g GPUs) Transact(pod *v1.Pod, option *GPUOption) error {
	if klog.V(5).Enabled() {
		klog.Infof("GPU %+v transacts %+v", g, option)
	}
	addedDict := make(map[int][]int)
	if isMultipleGPUPod(pod) {
		var cmReqIndex int
		for i := 0; i < len(option.Allocated); i++ {
			var gpuIndexes []int
			// container i needs more than one shared card
			//if option.Request[i].GPUCount > 0 {
			if option.CMRequest[i].MultiGPUCount > 0 {
				for j := 0; j < len(option.Allocated[i]); j++ {
					cmReqIndex++
					// option.Allocated[i][i] is the specific gpu indexer
					// judge whether g[option.Allocated[i][j]] can satisfy the request of container i
					if !g[option.Allocated[i][j]].CanAllocate(option.CMRequest[cmReqIndex-1].GPUUnit) {
						g.cancelAdded(addedDict, pod, option)
						klog.Errorf("Fail to trade multi option %+v on %s because the GPU's residual memory or core can't satisfy the container", option, g.ToString())
						return fmt.Errorf("can't trade option %+v on %+v because the GPU's residual memory or core can't satisfy the container", option, g)
					}
					g[option.Allocated[i][j]].AddContainerMultiRequest(option.CMRequest[cmReqIndex-1].GPUUnit)
					gpuIndexes = append(gpuIndexes, option.Allocated[i][j])
					addedDict[i] = gpuIndexes
				}
			}
		}
	} else {
		// traverse all containers in the node
		for i := 0; i < len(option.Allocated); i++ {
			// container i needs more than one full card
			var gpuIndexes []int
			if option.Request[i].GPUCount > 0 {
				// traverse all needed gpus of container i
				for j := 0; j < len(option.Allocated[i]); j++ {
					// option.Allocated[i][i] is the specific gpu indexer
					// judge whether g[option.Allocated[i][j]] can satisfy the request of container i
					if !g[option.Allocated[i][j]].CanAllocate(option.Request[i]) {
						g.cancelAdded(addedDict, pod, option)
						klog.Errorf("Fail to trade option %+v on %s because the GPU's residual memory or core can't satisfy the container", option, g.ToString())
						return fmt.Errorf("can't trade option %+v on %+v because the GPU's residual memory or core can't satisfy the container", option, g)
					}
					g[option.Allocated[i][j]].Add(option.Request[i])
					gpuIndexes = append(gpuIndexes, option.Allocated[i][j])
					addedDict[i] = gpuIndexes
				}
			} else {
				// container i needs only one card
				if len(option.Allocated[i]) > 0 {
					// judge whether the only needed card can satisfy the request of container i
					if !g[option.Allocated[i][0]].CanAllocate(option.Request[i]) {
						g.cancelAdded(addedDict, pod, option)
						klog.Errorf("Fail to trade option %+v on %+v because the GPU's residual memory or core can't satisfy the container", option, g.ToString())
						return fmt.Errorf("can't trade option %+v on %+v because the GPU's residual memory or core can't satisfy the container", option, g)
					}
					g[option.Allocated[i][0]].Add(option.Request[i])
					gpuIndexes = append(gpuIndexes, option.Allocated[i][0])
					addedDict[i] = gpuIndexes
				}
			}
		}
	}
	return nil
}

func (g GPUs) cancelAdded(addedDict map[int][]int, pod *v1.Pod, option *GPUOption) {
	if IsMultipleGPUPod(pod) {
		for containerID, gpuCardIndexes := range addedDict {
			for _, index := range gpuCardIndexes {
				g[index].SubContainerMultiRequest(option.CMRequest[containerID].GPUUnit)
			}
		}
	} else {
		for containerID, gpuCardIndexes := range addedDict {
			for _, index := range gpuCardIndexes {
				g[index].Sub(option.Request[containerID])
			}
		}
	}
}

func (g GPUs) Cancel(pod *v1.Pod, option *GPUOption) {
	if IsMultipleGPUPod(pod) {
		var cmReqIndex int
		for i := 0; i < len(option.Allocated); i++ {
			if len(option.Allocated[i]) > 0 {
				for j := 0; j < len(option.Allocated[i]); j++ {
					cmReqIndex++
					g[option.Allocated[i][j]].SubContainerMultiRequest(option.CMRequest[cmReqIndex-1].GPUUnit)
				}
			}
		}
	} else {
		for i := 0; i < len(option.Request); i++ {
			if option.Request[i].GPUCount > 0 {
				for _, gpuIndex := range option.Allocated[i] {
					g[gpuIndex].Sub(option.Request[i])
				}
			} else {
				if len(option.Allocated[i]) > 0 {
					g[option.Allocated[i][0]].Sub(option.Request[i])
				}
			}
		}
	}
}

// Trade trades an allocation
func (g GPUs) Trade(rater Rater, request GPURequest, pod *v1.Pod, cmReq ContainerMultiGPURequest) (option *GPUOption, err error) {
	var (
		found     bool
		startTime time.Time
	)
	// if the sum of multiGPUCount is larger than the length of cmReqs,
	// there is at least one container requests multiple GPUs.
	if len(cmReq) > len(request) {
		found, option = g.TradeForContainerMultipleRequest(rater, request, pod, cmReq)
	} else {
		found, option = g.TradeForOriginRequest(rater, request, pod, cmReq)
	}

	if klog.V(3).Enabled() {
		klog.Infof("The result of allocating gpu to pod %s is %v, container count: %d, gpu cards :%d, elapsed time: %d ms,result: %+v, gpu resource :%+v",
			pod.Name, found, len(request), len(g), time.Since(startTime).Milliseconds(), option.Allocated, g.ToString())
	}
	if !found {
		return nil, fmt.Errorf("no enough mgpu resource to allocate")
	}
	return option, nil
}

// TradeForOriginRequest trades for origin mGPU request.
// nolint
func (g GPUs) TradeForOriginRequest(rater Rater, request GPURequest, pod *v1.Pod,
	cmReq ContainerMultiGPURequest) (found bool, option *GPUOption) {
	var (
		dfs     func(i int)
		indexes = make([][]int, len(request))
	)
	found = false
	option = NewGPUOption(request, cmReq)
	dfs = func(containerIndex int) {
		// 1. return when it reaches the leaf node and update the option with the highest score
		if containerIndex == len(request) {
			found = true
			currScore := rater.Rate(g)
			if option.Score > currScore {
				return
			}
			for i, gpuIndex := range indexes {
				option.Allocated[i] = gpuIndex
			}
			option.Score = currScore
			return
		}
		if klog.V(5).Enabled() {
			klog.Infof("Start allocate gpu to container %s/%d, container req: %+v, gpu resource: %+v", pod.Name, containerIndex, request[containerIndex], g.ToString())
		}
		// 2. when container request several full cards
		if request[containerIndex].GPUCount > 0 {
			idleGPUs := g.GetIdleGPUs()
			if len(idleGPUs) < request[containerIndex].GPUCount {
				klog.Infof("Can't allocate gpu to container %s/%d, container req: %+v, no idle card", pod.Name, containerIndex, request[containerIndex])
				return
			}
			indexes[containerIndex] = idleGPUs[:request[containerIndex].GPUCount]
			for _, gpuIndex := range indexes[containerIndex] {
				g[gpuIndex].Add(request[containerIndex])
			}
			dfs(containerIndex + 1)
			for _, gpuIndex := range indexes[containerIndex] {
				g[gpuIndex].Sub(request[containerIndex])
			}
			return
		}
		// 3. when container request part of one card
		for i, gpu := range g {
			if !gpu.CanAllocate(request[containerIndex]) {
				klog.Infof("Can't allocate gpu to container %s/%d, container req: %+v, current gpu:%+v", pod.Name, containerIndex, request[containerIndex], gpu)
				continue
			}
			klog.V(5).Infof("Try allocate gpu to container %s/%d, container req: %+v, current gpu:%+v", pod.Name, containerIndex, request[containerIndex], gpu)
			gpu.Add(request[containerIndex])
			indexes[containerIndex] = make([]int, 1)
			indexes[containerIndex][0] = i
			dfs(containerIndex + 1)
			gpu.Sub(request[containerIndex])
			// if the container count is too many, it returns the first available gpu cards
			if g.IsTooManyContainers(len(request)) {
				break
			}
		}
	}
	dfs(0)
	return found, option
}

// TradeForContainerMultipleRequest trades for gpu request with multiple mGPU instance.
// nolint
func (g GPUs) TradeForContainerMultipleRequest(rater Rater, request GPURequest, pod *v1.Pod,
	cmReq ContainerMultiGPURequest) (found bool, option *GPUOption) {
	var (
		multiDfs func(i int)
		indexes  = make([][]int, len(request))
	)
	found = false
	option = NewGPUOption(request, cmReq)
	multiDfs = func(containerIndex int) {
		// 1. return when it reaches the leaf node and update the option with the highest score
		if containerIndex == len(cmReq) {
			found = true
			currScore := rater.Rate(g)
			if option.Score > currScore {
				return
			}
			for i, gpuIndex := range indexes {
				option.Allocated[i] = gpuIndex
			}
			option.Score = currScore
			return
		}
		if klog.V(5).Enabled() {
			klog.Infof("Start allocate Multiple gpus to container %s/%d, container req: %+v, gpu resource: %+v", pod.Name, containerIndex, cmReq[containerIndex], g.ToString())
		}
		// 2. when container request several full cards
		if cmReq[containerIndex].MultiGPUCount == NotNeedMultipleGPU &&
			cmReq[containerIndex].GPUUnit.Core != NotNeedGPU {
			idleGPUs := g.GetIdleGPUs()
			if len(idleGPUs) < cmReq[containerIndex].GPUCount {
				klog.Infof("Can't allocate gpu to container %s/%d, container req: %+v, no idle card", pod.Name, containerIndex, request[containerIndex])
				return
			}
			indexes[containerIndex] = idleGPUs[:cmReq[containerIndex].GPUCount]
			for _, gpuIndex := range indexes[containerIndex] {
				g[gpuIndex].Add(cmReq[containerIndex].GPUUnit)
			}
			multiDfs(containerIndex + 1)
			for _, gpuIndex := range indexes[containerIndex] {
				g[gpuIndex].Sub(cmReq[containerIndex].GPUUnit)
			}
			return
		}

		// 2. when container request multi cards
		for i, gpu := range g {
			ck := cmReq[containerIndex].ContainerKey
			if gpu.IsUsedBy(ck) {
				continue
			}
			if !gpu.CanAllocate(cmReq[containerIndex].GPUUnit) {
				klog.V(5).Infof("Can't allocate Multiple gpus to container %s, container cmReq: %+v, current gpu:%+v",
					cmReq[containerIndex].ContainerKey, cmReq[containerIndex], gpu)
				continue
			}
			klog.V(5).Infof("Try allocate Multiple gpus to container %s/%d, container cmReq: %+v, current gpu:%+v", pod.Name, containerIndex, cmReq[containerIndex], gpu)
			gpu.AddContainerMultiRequest(cmReq[containerIndex].GPUUnit)
			gpu.SignMultiUsedContainer(ck)
			indexes[cmReq[containerIndex].ParentContainerIndex] = append(indexes[cmReq[containerIndex].ParentContainerIndex], i)
			multiDfs(containerIndex + 1)
			gpu.SubContainerMultiRequest(cmReq[containerIndex].GPUUnit)
			gpu.ExcludeMultiUsedIndex(ck)
			tmpIndexes := indexes[cmReq[containerIndex].ParentContainerIndex]
			indexes[cmReq[containerIndex].ParentContainerIndex] = tmpIndexes[:len(tmpIndexes)-1]
			// if the container count is too many, it returns the first available gpu cards
			if g.IsTooManyContainers(len(cmReq)) {
				break
			}
		}
	}
	multiDfs(0)
	return found, option
}

// ToString is  ToString
func (g GPUs) ToString() string {
	res, _ := json.Marshal(g)
	return string(res)
}

// GetIdleGPUs get idle gpus
func (g GPUs) GetIdleGPUs() []int {
	indexes := make([]int, 0)
	for i := 0; i < len(g); i++ {
		if g[i].CoreAvailable == g[i].CoreTotal && g[i].MemoryAvailable == g[i].MemoryTotal {
			indexes = append(indexes, i)
		}
	}

	return indexes
}

// IsTooManyContainers return true if container more than MaxContainersNumPolicyValid
func (g GPUs) IsTooManyContainers(containerCount int) bool {
	num := GlobalConfig.MaxContainersNumPolicyValid
	if num <= 0 {
		num = DefaultNumThatPolicyValid
	}
	if containerCount > num {
		return true
	}
	return false
}

// IsUsedBy returns true when the container index is existed in this gpu multiple used list.
func (g *GPUDevice) IsUsedBy(containerKey string) bool {
	_, exist := g.MultiUsedContainers[containerKey]
	return exist
}

// Add adds resources
func (g *GPUDevice) Add(resource GPUUnit) {
	if resource.Core == NotNeedGPU {
		return
	}
	if resource.GPUCount > 0 {
		g.CoreAvailable = 0
		g.MemoryAvailable = 0
	} else {
		g.CoreAvailable -= resource.Core
		g.MemoryAvailable -= resource.Memory
	}
	g.ContainerCount++
	g.MGPUInstanceCount++
}

// Sub subs resources
func (g *GPUDevice) Sub(resource GPUUnit) {
	if resource.Core == NotNeedGPU {
		return
	}
	if resource.GPUCount > 0 {
		g.CoreAvailable = g.CoreTotal
		g.MemoryAvailable = g.MemoryTotal
	} else {
		g.CoreAvailable += resource.Core
		g.MemoryAvailable += resource.Memory
	}
	g.ContainerCount--
	g.MGPUInstanceCount--
}

// AddContainerMultiRequest add container multi GPU request resources
func (g *GPUDevice) AddContainerMultiRequest(resource GPUUnit) {
	g.CoreAvailable -= resource.Core
	g.MemoryAvailable -= resource.Memory
	g.MGPUInstanceCount++
}

// SubContainerMultiRequest sub container multi GPU request resources
func (g *GPUDevice) SubContainerMultiRequest(resource GPUUnit) {
	g.CoreAvailable += resource.Core
	g.MemoryAvailable += resource.Memory
	g.MGPUInstanceCount--
}

// CanAllocate returns whether the GPU can satisfy the resource request.
func (g *GPUDevice) CanAllocate(resource GPUUnit) bool {
	if resource.Core == NotNeedGPU {
		return true
	}
	if g.ContainerCount >= GlobalConfig.MaxContainersPerCard ||
		g.MGPUInstanceCount >= GlobalConfig.MaxContainersPerCard {
		return false
	}
	// if resource request needs more than one card
	if resource.GPUCount > 0 {
		return g.CoreAvailable == g.CoreTotal && g.MemoryAvailable == g.MemoryTotal
	}
	return g.CoreAvailable >= resource.Core && g.MemoryAvailable >= resource.Memory
}

// SignMultiUsedContainer appends the used container index to this gpu multiple used list.
func (g *GPUDevice) SignMultiUsedContainer(containerKey string) {
	g.MultiUsedContainers[containerKey]++
}

// ExcludeMultiUsedIndex excludes the container index from this gpu multiple used list.
func (g *GPUDevice) ExcludeMultiUsedIndex(containerKey string) {
	g.MultiUsedContainers[containerKey]--
	if g.MultiUsedContainers[containerKey] == 0 {
		delete(g.MultiUsedContainers, containerKey)
	}
}

// Patch patches new data to the old obj
func Patch(client kubernetes.Interface, old, new *v1.Pod) error {
	oldData, err := json.Marshal(old)
	if err != nil {
		return fmt.Errorf("generate old data json marshal err: %v", err)
	}
	newData, err := json.Marshal(new)
	if err != nil {
		return fmt.Errorf("generate new data json marshal err: %v", err)
	}

	patchData, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, &v1.Pod{})
	if err != nil {
		return fmt.Errorf("generate new data json marshal err: %v", err)
	}
	if len(patchData) == 0 {
		return nil
	}

	if _, err := client.CoreV1().Pods(old.Namespace).Patch(context.Background(), old.Name,
		types.StrategicMergePatchType, patchData, metav1.PatchOptions{}); err != nil {
		return err
	}

	return nil
}

func patchGPUInfo(kubeClient kubernetes.Interface, pod *v1.Pod, option *GPUOption) error {
	// convert containers' gpu option to pods' annotations
	podCopy := pod.DeepCopy()
	if podCopy.Annotations == nil {
		podCopy.Annotations = make(map[string]string)
	}
	for i, c := range podCopy.Spec.Containers {
		cGPUReq := option.Request[i]
		//fixed one pod has gpu container and non gpu container
		if cGPUReq.Core == NotNeedGPU {
			continue
		}
		var ids string
		for _, id := range option.Allocated[i] {
			if len(ids) > 0 {
				ids += ","
			}
			ids += strconv.Itoa(id)
		}

		podCopy.Annotations[fmt.Sprintf(VKEAnnotationMGPUContainer, c.Name)] = ids
		podCopy.Annotations[VKEAnnotationMGPUAssumed] = "true"
		klog.V(5).Infof("patch pod %s annotation: container %s with gpus' index: %s ", pod.Name, c.Name, ids)
	}

	return Patch(kubeClient, pod, podCopy)
}

// IsMultipleGPUPod return true when there is one pod required mgpu-core, mgpu-mem and multiple gpu count.
func IsMultipleGPUPod(pod *v1.Pod) bool {
	gpuCountList, err := ExtraMultipleGPUCountList(pod)
	if err != nil {
		return false
	}
	for i, count := range gpuCountList {
		// skip container doesn't need gpu
		if count == 0 {
			continue
		}
		core, _ := GetGPUCoreFromContainer(&pod.Spec.Containers[i], VKEResourceMGPUCore)
		mem, _ := GetGPUMemoryFromContainer(&pod.Spec.Containers[i], VKEResourceMGPUMemory)
		if core%count == 0 && mem/count >= MinGPUMem {
			return true
		} else if core/GPUCoreEachCard == count {
			return true
		}
	}

	return false
}
