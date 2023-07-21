package mgpu

// Rater is Rater
type Rater interface {
	Rate(g GPUs) float64
}

// GPUBinpack is the policy for gpu binpack
type GPUBinpack struct{}

// Rate rates a combination
func (bp *GPUBinpack) Rate(g GPUs) float64 {
	return calVar(g)
}

// GPUSpread is the policy for gpu spread
type GPUSpread struct{}

// Rate rates a combination
func (s *GPUSpread) Rate(g GPUs) float64 {
	return 1 - calVar(g)
}

func calVar(g GPUs) float64 {
	var coreVar float64
	var memVar float64
	for _, gpu := range g {
		cpuAllocRatio := float64(gpu.CoreAvailable) / float64(gpu.CoreTotal)
		coreVar += cpuAllocRatio * cpuAllocRatio
		memAllocRatio := float64(gpu.MemoryAvailable) / float64(gpu.MemoryTotal)
		memVar += memAllocRatio * memAllocRatio
	}
	coreVar = coreVar / float64(len(g))
	memVar = memVar / float64(len(g))
	weightOfCore := float64(GlobalConfig.WeightOfCore)
	weightOfMemory := float64(100 - GlobalConfig.WeightOfCore)
	return (weightOfCore*coreVar + weightOfMemory*memVar) / float64(100)
}
