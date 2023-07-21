package validate

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"strconv"
	"strings"
)

const (
	PrefixVKE                                         = "vke.volcengine.com/"
	ResourceMGPUCore                  v1.ResourceName = PrefixVKE + "mgpu-core"
	ResourceMGPUMemory                v1.ResourceName = PrefixVKE + "mgpu-memory"
	VKEAnnotationContainerMultipleGPU                 = PrefixVKE + "container-multiple-gpu"

	GPUCoreEachCard = 100
	// MinNormalGPUMem is minimum of GPU memory that normal mgpu can work
	MinNormalGPUMem = 256
	// MinMultiMGPUMem is minimum of GPU memory that single container multiple mgpu can work
	MinMultiMGPUMem = 1024
)

// validateNormalMGPUPod validates the pod requested normal mgpu resource.
func validateNormalMGPUPod(pod *v1.Pod) (bool, string) {
	for _, c := range pod.Spec.Containers {
		core, err1 := getGPUCoreFromContainer(&c, ResourceMGPUCore)
		mem, err2 := getGPUMemoryFromContainer(&c, ResourceMGPUMemory)
		// if container not request gpu, skip
		if err1 != nil && err2 != nil {
			continue
		}
		if core <= 0 || mem < 0 {
			return false, "gpu-core require greater than 0 and gpu-mem must be non-negative"
		} else if core < 100 && mem < MinNormalGPUMem {
			return false, fmt.Sprintf("less one gpu card require gpu-mem greater than %d MiB", MinNormalGPUMem)
		} else if isInvalidFullGPUCore(core) {
			return false, "full gpu cards require gpu-core must integer multiple of 100"
		} else if isFullCardCore(core) && err2 == nil {
			return false, "full gpu cards require gpu-mem can't be set"
		}
	}

	return true, ""
}

// validateContainerMultipleMGPUPod validates the pod requested single container multiple mgpu resource.
func validateContainerMultipleMGPUPod(pod *v1.Pod, gpuCountList []int) (bool, string) {
	if len(gpuCountList) != len(pod.Spec.Containers) {
		return false, fmt.Sprintf("requested multiple gpu count in %s doesn't match container numbers", VKEAnnotationContainerMultipleGPU)
	}

	for i, count := range gpuCountList {
		if count < 0 {
			return false, fmt.Sprintf("multiple gpu count in %s can't be negative integers", VKEAnnotationContainerMultipleGPU)
		}
		core, err1 := getGPUCoreFromContainer(&pod.Spec.Containers[i], ResourceMGPUCore)
		mem, err2 := getGPUMemoryFromContainer(&pod.Spec.Containers[i], ResourceMGPUMemory)
		if (err1 == nil && core <= 0) || (err2 == nil && mem < 0) {
			return false, "gpu-core require greater than 0 and gpu-mem must be non-negative"
		}
		if count == 0 {
			if core == 0 && mem == 0 {
				continue
			}
			return false, fmt.Sprintf("gpu-core and gpu-mem not match gpu-count: %d, container index: %d", count, i+1)
		}
		if core <= 0 || mem < 0 {
			return false, "gpu-core require greater than 0 and gpu-mem must be non-negative"
		}
		if isFullCardCore(core) && core/GPUCoreEachCard == count {
			if err2 == nil {
				return false, "full gpu cards require gpu-mem can't be set"
			} else if mem == 0 {
				return true, ""
			}
			return false, "full gpu cards require gpu-mem must equal 0"
		} else if mem/count < MinMultiMGPUMem {
			return false, fmt.Sprintf("gpu request with multiple gpu count require gpu-mem/gpu-count greater than %d MiB", MinMultiMGPUMem)
		} else if core%count != 0 || mem%count != 0 {
			return false, fmt.Sprintf("gpu request can't be divisible by multiple gpu count: %d", count)
		} else if core/count > 100 {
			return false, "mgpu-core/gpu-count can't be greater than 100"
		}
	}

	return true, ""
}

// isFullCardCore returns true when mgpu-core is a multiple of 100.
func isFullCardCore(core int) bool {
	return core >= 100 && core%100 == 0
}

// isInvalidFullGPUCore returns true when mgpu-core > 100 and not a full card core.
func isInvalidFullGPUCore(core int) bool {
	return core > 100 && core%100 != 0
}

// CheckAndExtraMultipleGPUCountList checks whether the pod set the annotation of "container-multiple-gpu" and
// extras all gpu counts of each container which using multiple mgpu.
// MultipleGPUCountList is the value of "vke.volcengine.com/container-multiple-gpu"
// e.g. "vke.volcengine.com/container-multiple-gpu":"2,1,0,4" means four containers with index [0,1,2,3] request gpu cards' counts are [2,1,0,4]
func checkAndExtraMultipleGPUCountList(pod *v1.Pod) (bool, []int, error) {
	var isMultipleGPUShare bool
	gpuCountList := make([]int, 0)
	if s, ok := pod.Annotations[VKEAnnotationContainerMultipleGPU]; ok {
		isMultipleGPUShare = true
		data := strings.Split(s, ",")
		for _, n := range data {
			sharedGPUCount, err := strconv.Atoi(n)
			if err != nil {
				return isMultipleGPUShare, nil, err
			}
			gpuCountList = append(gpuCountList, sharedGPUCount)
		}
	} else {
		isMultipleGPUShare = false
		return isMultipleGPUShare, nil, NewMultipleGPUShareError("container-multiple-gpu hasn't been set")
	}

	if len(gpuCountList) == 0 {
		return true, nil, fmt.Errorf("the value of container-multiple-gpu can't be none")
	}

	return true, gpuCountList, nil
}

// getGPUCoreFromContainer gets GPUCoreFromContainer
func getGPUCoreFromContainer(container *v1.Container, resource v1.ResourceName) (int, error) {
	val, ok := container.Resources.Requests[resource]
	if !ok {
		return 0, fmt.Errorf("%s not exist", resource)
	}

	return int(val.Value()), nil
}

// getGPUMemoryFromContainer gets GPUMemoryFromContainer
func getGPUMemoryFromContainer(container *v1.Container, resource v1.ResourceName) (int, error) {
	val, ok := container.Resources.Requests[resource]
	if !ok {
		return 0, fmt.Errorf("%s not exist", resource)
	}

	return int(val.Value()), nil
}

// notExistMultipleGPUAnno is notExistMultipleGPUAnno
func notExistMultipleGPUAnno(err error) bool {
	if gsErr, ok := err.(*MultipleGPUShareError); ok {
		return gsErr.failReason == "container-multiple-gpu hasn't been set"
	}
	return false
}
