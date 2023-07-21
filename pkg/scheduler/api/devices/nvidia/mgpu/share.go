package mgpu

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"strconv"
	"strings"
)

const (
	// MinGPUMem is minimum of GPU memory that normal mgpu can work
	MinGPUMem = 256
	// MinMultiMGPUMem is minimum of GPU memory that multiple container mgpu can work
	MinMultiMGPUMem = 1024
)

// GPUUnit is GPUUnit
type GPUUnit struct {
	Core     int
	Memory   int
	GPUCount int
}

// GPURequest is GPURequest
type GPURequest []GPUUnit

// MultiGPUUnit is the unit request of container using multiple gpu
type MultiGPUUnit struct {
	GPUUnit
	ContainerKey         string
	MultiGPUCount        int
	ParentContainerIndex int
}

// ContainerMultiGPURequest is the list of Container MultiGPUUnit
type ContainerMultiGPURequest []MultiGPUUnit

// NewGPURequest initializes GPU requests, including two cases:
// 1. Pod with normal mGPU which only set mgpu-core and mgpu-memory resources without
// 2. Pod with single container using multiple mGPU which external using annotation "vke.volcengine.com/container-multiple-gpu":{gpuCountList}
// e.g. "vke.volcengine.com/container-multiple-gpu": "1,0,3" means No.0 container using one GPU Card,
// No.1 Container using none GPU Card and No.2 Container using two GPU Card.
func NewGPURequest(pod *v1.Pod, core v1.ResourceName, mem v1.ResourceName) (GPURequest, ContainerMultiGPURequest) {
	request := make([]GPUUnit, len(pod.Spec.Containers))
	gpuCountList, _ := ExtraMultipleGPUCountList(pod)
	var cmRequest ContainerMultiGPURequest
	for i, c := range pod.Spec.Containers {
		core, _ := GetGPUCoreFromContainer(&c, core)
		mem, _ := GetGPUMemoryFromContainer(&c, mem)
		if core == 0 && mem == 0 {
			request[i].Core = NotNeedGPU
			request[i].Memory = NotNeedGPU
			continue
		}
		request[i] = GPUUnit{
			Core:   core,
			Memory: mem,
		}
		if core >= GPUCoreEachCard {
			request[i].GPUCount = core / GPUCoreEachCard
		}

		var multiGPUUnit MultiGPUUnit
		multiGPUUnit.ContainerKey = GetKeyOfContainer(pod, c.Name)
		// gpuCountList is the value of annotation "vke.volcengine.com/container-multiple-gpu" which means the Pod is using single container multiple GPU.
		// when gpuCountList's length is greater than 0, we need initialize an extra cmRequest for trade, including two cases:
		// 1. if requested gpuCount = 0, we mark is as NotNeedMultipleGPU
		// 2. if requested gpuCount > 0, we average the origin GPURequest by gpuCount for several cmRequests.
		if len(gpuCountList) > 0 {
			if gpuCountList[i] == 0 {
				multiGPUUnit.MultiGPUCount = NotNeedMultipleGPU
				multiGPUUnit.GPUUnit.GPUCount = core / GPUCoreEachCard
				multiGPUUnit.GPUUnit.Core = NotNeedGPU
				cmRequest = append(cmRequest, multiGPUUnit)
				continue
			} else {
				multiGPUUnit.MultiGPUCount = gpuCountList[i]
				multiGPUUnit.GPUUnit.Core = core / multiGPUUnit.MultiGPUCount
				multiGPUUnit.GPUUnit.Memory = mem / multiGPUUnit.MultiGPUCount
				multiGPUUnit.ParentContainerIndex = i
				for i := 0; i < multiGPUUnit.MultiGPUCount; i++ {
					cmRequest = append(cmRequest, multiGPUUnit.DeepCopy())
				}
			}
		}
	}
	klog.V(5).Infof("pod %s gpu request: %+v, container multiple gpu request: %+v", pod.Name, request, cmRequest)
	return request, cmRequest
}

// GPUOption is GPUOption
type GPUOption struct {
	// request of container with mgpu resources
	Request   GPURequest
	CMRequest ContainerMultiGPURequest
	// Each column represents the cards needed for the container and each row
	// represents the card indexer used by each container. e.g. Allocated[1]={0,2,5}
	// Container with index 1 requires 3 GPU cards, with card indexes are 0, 2 and 5.
	Allocated [][]int
	Score     float64
}

// NewGPUOption news GPUOption
func NewGPUOption(request GPURequest, cmRequest ContainerMultiGPURequest) *GPUOption {
	opt := &GPUOption{
		Request:   request,
		CMRequest: cmRequest,
		Allocated: make([][]int, len(request)),
		Score:     0,
	}
	return opt
}

// NewGPUOptionFromPod news GPUOption from Pod
func NewGPUOptionFromPod(pod *v1.Pod, core v1.ResourceName, mem v1.ResourceName) *GPUOption {
	request, cmReq := NewGPURequest(pod, core, mem)
	option := NewGPUOption(request, cmReq)
	for i, c := range pod.Spec.Containers {
		if v, ok := pod.Annotations[fmt.Sprintf(VKEAnnotationMGPUContainer, c.Name)]; ok {
			klog.V(5).Infof("container %s gpu key: %s", c.Name, v)
			// ids is a string of gpus which container i needs, like "2,3"
			ids := strings.Split(v, ",")
			idsInt := make([]int, 0)
			for _, s := range ids {
				id, _ := strconv.Atoi(s)
				idsInt = append(idsInt, id)
			}
			option.Allocated[i] = idsInt
		}
	}
	klog.V(5).Infof("pod %s/%s allocated gpu: %d", pod.Namespace, pod.Name, option.Allocated)

	return option
}

// GetKeyOfContainer returns the key of container
func GetKeyOfContainer(pod *v1.Pod, c string) string {
	if pod == nil {
		return ""
	}

	return fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, c)
}

// IsFullCardCore returns true when mgpu-core is a multiple of 100.
func IsFullCardCore(core int) bool {
	return core >= 100 && core%100 == 0
}

// IsInvalidFullGPUCore returns true when mgpu-core > 100 and not a full card core.
func IsInvalidFullGPUCore(core int) bool {
	return core > 100 && core%100 != 0
}

// DeepCopy is the multiGPUUnit DeepCopy.
func (mgu MultiGPUUnit) DeepCopy() MultiGPUUnit {
	return MultiGPUUnit{
		GPUUnit: GPUUnit{
			Core:     mgu.Core,
			Memory:   mgu.Memory,
			GPUCount: mgu.GPUCount,
		},
		ContainerKey:         mgu.ContainerKey,
		MultiGPUCount:        mgu.MultiGPUCount,
		ParentContainerIndex: mgu.ParentContainerIndex,
	}
}

// GetGPUCoreFromContainer gets GPUCoreFromContainer
func GetGPUCoreFromContainer(container *v1.Container, resource v1.ResourceName) (int, error) {
	val, ok := container.Resources.Requests[resource]
	if !ok {
		return 0, fmt.Errorf("%s not exist", resource)
	}
	return int(val.Value()), nil
}

// GetGPUMemoryFromContainer gets GPUMemoryFromContainer
func GetGPUMemoryFromContainer(container *v1.Container, resource v1.ResourceName) (int, error) {
	val, ok := container.Resources.Requests[resource]
	if !ok {
		return 0, fmt.Errorf("%s not exist", resource)
	}
	return int(val.Value()), nil
}
