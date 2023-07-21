package mgpu

import v1 "k8s.io/api/core/v1"

/*
	   actions: "reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: priority
	     - name: gang
	     - name: conformance
	   - plugins:
	     - name: drf
	     - name: predicates
	       arguments:
	         predicate.MGPUEnable: true
		     predicate.MGPUPolicy: binpack
	 		 predicate.MGPUWeightOfCore: 20
			 predicate.MGPUScheduleMode: index
			 predicate.MGPUMaxContainersPerCard: 16
	     - name: proportion
	     - name: nodeorder
*/

var (
	MGPUEnable   bool
	GlobalConfig = &MGPUConfig{
		Policy:                      Binpack,
		MaxContainersNumPolicyValid: DefaultMaxContainersNumPolicyValid,
		WeightOfCore:                DefaultWeightOfCore,
		ScheduleMode:                DefaultScheduleMode,
		MaxContainersPerCard:        DefaultMaxContainersPerCard,
	}
)

type MGPUConfig struct {
	// policy is the card-level scoring policy, e.g. binpack/spread
	Policy string
	// MaxContainersNumPolicyValid means that bigger then the number GPU Policy is invalid
	MaxContainersNumPolicyValid int
	// WeightOfCore is the weight of core, e.g. 20
	WeightOfCore int
	// ScheduleMode is the schedule mode, e.g. index/id
	ScheduleMode string
	// MaxContainersPerCard is the number of max containers per card, e.g. 16
	MaxContainersPerCard int
}

const (
	Policy                             = "predicate.MGPUPolicy"
	MaxContainersNumPolicyValid        = "predicate.MGPUMaxContainersNumPolicyValid"
	WeightOfCore                       = "predicate.MGPUWeightOfCore"
	ScheduleMode                       = "predicate.MGPUScheduleMode"
	MaxContainersPerCard               = "predicate.MGPUMaxContainersPerCard"
	DefaultWeightOfCore                = 20
	DefaultMaxContainersNumPolicyValid = 5
	DefaultMaxContainersPerCard        = 16
	DefaultScheduleMode                = "index"
)

// ModeType is a type "string".
type ModeType string

// PolicyType is a type "string".
type PolicyType string

const (
	// Binpack is the string "binpack".
	Binpack = "binpack"
	// Spread is the string "spread".
	Spread = "spread"
)

const (
	// GPUCoreEachCard is GPUCoreEachCard
	GPUCoreEachCard = 100
	// NotNeedGPU is NotNeedGPU
	NotNeedGPU = -1
	// NotNeedRate is NotNeedRate
	NotNeedRate = -2
	// NotNeedMultipleGPU is NotNeedMultipleGPU
	NotNeedMultipleGPU = -3
	// GPUTypeMGPU is GPUTypeMGPU
	GPUTypeMGPU = "mgpu"
	// GPUTypePGPU is GPUTypePGPU
	GPUTypePGPU = "nvidia"
	// DefaultComputePolicy is DefaultComputePolicy
	DefaultComputePolicy = "fixed-share"
	// NativeBurstSharePolicy is NativeBurstSharePolicy
	NativeBurstSharePolicy = "native-burst-share"
	// DefaultGPUCount is DefaultGPUCount
	DefaultGPUCount = 1

	// VKEAnnotationMGPUAssumed is the scheduled result for mgpu pod
	VKEAnnotationMGPUAssumed = "vke.volcengine.com/assumed"
	// VKEAnnotationMGPUContainer is the format for gpu index with specific container name
	VKEAnnotationMGPUContainer = "vke.volcengine.com/gpu-index-container-%s"
	// VKELabelNodeResourceType is label key of node resource type, the value is like nvidia/mgpu
	VKELabelNodeResourceType = "vke.node.gpu.schedule"
	// VKEAnnotationMGPUComputePolicy is annotation for mgpu compute policy on vke
	VKEAnnotationMGPUComputePolicy = "vke.volcengine.com/mgpu-compute-policy"
	// VKEAnnotationContainerMultipleGPU is annotation for mgpu container multiple mode on vke
	VKEAnnotationContainerMultipleGPU = "vke.volcengine.com/container-multiple-gpu"
	// VKEResourceMGPUCore is mgpu-core resource on vke
	VKEResourceMGPUCore v1.ResourceName = "vke.volcengine.com/mgpu-core"
	// VKEResourceMGPUMemory is mgpu-memory resource on vke
	VKEResourceMGPUMemory v1.ResourceName = "vke.volcengine.com/mgpu-memory"

	DeviceName = "mgpu"
)
