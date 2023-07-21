package mgpu

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"strconv"
	"strings"
)

// GetPodNamespaceName is GetPodNamespaceName
func GetPodNamespaceName(pod *v1.Pod) string {
	if pod == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

// ExtraMultipleGPUCountList extras all gpu counts of each container which using multiple mGPU.
func ExtraMultipleGPUCountList(pod *v1.Pod) ([]int, error) {
	var gpuList []int
	if s, ok := pod.Annotations[VKEAnnotationContainerMultipleGPU]; ok {
		data := strings.Split(s, ",")
		for _, n := range data {
			sharedGPUCount, err := strconv.Atoi(n)
			if err != nil {
				return nil, err
			}
			gpuList = append(gpuList, sharedGPUCount)
		}
	} else {
		return nil, NewMultiGPUError("container-multiple-gpu hasn't been set")
		//return nil, "container-multiple-gpu hasn't been set"
	}
	return gpuList, nil
}

func checkMGPUResourceInPod(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if _, ok := container.Resources.Limits[VKEResourceMGPUCore]; ok {
			return true
		} else if _, ok = container.Resources.Limits[VKEResourceMGPUMemory]; ok {
			return true
		}
	}

	return false
}

func getRater() Rater {
	//var rater Rater
	//switch GlobalConfig.Policy {
	//case Binpack:
	//	rater = &GPUBinpack{}
	//case Spread:
	//	rater = &GPUSpread{}
	//default:
	//	klog.Errorf("priority algorithm is not supported: %s", GlobalConfig.Policy)
	//	return nil
	//}
	return &GPUBinpack{}
}

func decodeNodeDevices(name string, node *v1.Node, rater Rater) *GPUDevices {
	gs := &GPUDevices{
		Name:          name,
		Rater:         rater,
		Pod2OptionMap: make(map[string]*GPUOption),
		GPUs:          make([]*GPUDevice, 0),
		CoreName:      VKEResourceMGPUCore,
		MemName:       VKEResourceMGPUMemory,
	}
	if nodePolicy, ok := node.ObjectMeta.Labels[VKEAnnotationMGPUComputePolicy]; ok {
		gs.NodePolicy = nodePolicy
	} else {
		gs.NodePolicy = DefaultComputePolicy
	}
	gpus := gs.GPUs
	if gpuType, ok := node.ObjectMeta.Labels[VKELabelNodeResourceType]; ok {
		switch gpuType {
		case GPUTypeMGPU:
			// coreAvail is an integral multiple number of 100, like vke.volcengine.com/mgpu-core: "200"
			coreAvail := node.Status.Allocatable[VKEResourceMGPUCore]
			// memAvail is specific value of gpu memory, like vke.volcengine.com/mgpu-memory: "30218"
			memAvail := node.Status.Allocatable[VKEResourceMGPUMemory]
			// gpuCount is the total gpu number of a node, like 200/100 = 2.
			gpuCount := int(coreAvail.Value() / GPUCoreEachCard)
			if gpuCount == 0 {
				klog.Errorf("no gpu available on node %s", node.Name)
				return nil
			}
			for i := 0; i < gpuCount; i++ {
				gpus = append(gpus, &GPUDevice{
					Index:               i,
					CoreAvailable:       100,
					CoreTotal:           100,
					MemoryAvailable:     int(memAvail.Value()) / gpuCount,
					MemoryTotal:         int(memAvail.Value()) / gpuCount,
					ContainerCount:      0,
					MGPUInstanceCount:   0,
					MultiUsedContainers: make(map[string]int),
				})
			}
		default:
			klog.Errorf("invalid resource value of %s", VKELabelNodeResourceType)
			return nil
		}
	}
	gs.GPUs = gpus
	return gs
}

func checkNodeMGPUSharingPredicate(pod *v1.Pod, gssnap *GPUDevices) (bool, error) {
	if !isSharedMGPUPod(pod) {
		return true, nil
	}

	gs := getMGPUDevicesSnapShot(gssnap)
	if isMultipleGPUPod(pod) && (getPodComputePolicy(pod) != NativeBurstSharePolicy ||
		gs.NodePolicy != NativeBurstSharePolicy) {
		return false, fmt.Errorf("compute policy not match multiple mgpu")
	}
	if !isFullCardGPUPod(pod) && getPodComputePolicy(pod) != gs.NodePolicy {
		return false, fmt.Errorf("compute policy not match normal mgpu")
	}
	// Check container specific number whether exceed the node's GPU number
	gpuCountList, _ := ExtraMultipleGPUCountList(pod)
	if len(gpuCountList) > 0 {
		for i, count := range gpuCountList {
			if count > len(gs.GPUs) {
				return false, fmt.Errorf("request multiple GPU count %d is exceed the allocatable GPU number, container index: %d", count, i+1)
			}
		}
	}

	return true, nil
}

func getPodNamespaceName(pod *v1.Pod) string {
	if pod == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

func isSharedMGPUPod(pod *v1.Pod) bool {
	containers := pod.Spec.Containers
	hasGPURequest := false
	for _, container := range containers {
		if _, ok := container.Resources.Limits[VKEResourceMGPUCore]; ok {
			hasGPURequest = true
			break
		} else if _, ok = container.Resources.Limits[VKEResourceMGPUMemory]; ok {
			hasGPURequest = true
			break
		}
	}

	return hasGPURequest
}

func getMGPUDevicesSnapShot(gssnap *GPUDevices) *GPUDevices {
	ret := GPUDevices{
		Name:          gssnap.Name,
		Rater:         gssnap.Rater,
		NodePolicy:    gssnap.NodePolicy,
		Pod2OptionMap: make(map[string]*GPUOption),
		GPUs:          make([]*GPUDevice, len(gssnap.GPUs)),
	}
	for podName, option := range gssnap.Pod2OptionMap {
		request := make(GPURequest, len(option.Request))
		for i, unit := range option.Request {
			request[i].Core = unit.Core
			request[i].Memory = unit.Memory
			request[i].GPUCount = unit.GPUCount
		}
		allocated := make([][]int, len(request))
		for i, gpuIndexes := range option.Allocated {
			allocated[i] = gpuIndexes
		}
		o := &GPUOption{
			Request:   request,
			Allocated: allocated,
			Score:     option.Score,
		}
		ret.Pod2OptionMap[podName] = o
	}
	for i, gpu := range gssnap.GPUs {

		ret.GPUs[i] = &GPUDevice{
			Index:             gpu.Index,
			CoreAvailable:     gpu.CoreAvailable,
			MemoryAvailable:   gpu.MemoryAvailable,
			CoreTotal:         gpu.CoreTotal,
			MemoryTotal:       gpu.MemoryTotal,
			ContainerCount:    gpu.ContainerCount,
			MGPUInstanceCount: gpu.MGPUInstanceCount,
		}
		ret.GPUs[i].MultiUsedContainers = make(map[string]int)
		for k, v := range gpu.MultiUsedContainers {
			ret.GPUs[i].MultiUsedContainers[k] = v
		}
	}
	return &ret
}

func getPodComputePolicy(pod *v1.Pod) string {
	if policy, ok := pod.ObjectMeta.Annotations[VKEAnnotationMGPUComputePolicy]; ok {
		return policy
	}
	return DefaultComputePolicy
}

// IsFullCardGPUPod returns true when all containers of the pod uses full card GPU, like "vke.volcengine.com/mgpu-core: 200"
// In this case, the pod will use the whole GPU card without regard to QoS
func isFullCardGPUPod(pod *v1.Pod) bool {
	containers := pod.Spec.Containers
	for _, c := range containers {
		core, ok := c.Resources.Requests[VKEResourceMGPUCore]
		if ok && core.Value()%100 != 0 {
			return false
		}
	}
	return true
}

// IsMultipleGPUPod return true when there is one pod required mgpu-core, mgpu-mem and multiple gpu count.
func isMultipleGPUPod(pod *v1.Pod) bool {
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

// MultiGPUError is MultiGPUError
type MultiGPUError struct {
	failReason string
}

func (mge *MultiGPUError) Error() string {
	return mge.failReason
}

// NewMultiGPUError is NewMultiGPUError
func NewMultiGPUError(f string) error {
	return &MultiGPUError{failReason: f}
}
