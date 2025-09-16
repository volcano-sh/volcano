package calculate

// todo: read from configmap or calculate cpu and memory limit dynamic
func CalculateCPUWeightFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 1000
	case 1:
		return 500
	case 0:
		return 100
	default:
		return 100
	}
}

func CalculateCPUQuotaFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 0
	case 1:
		return 0
	case 0:
		return 0
	default:
		return 0
	}
}

// CalculateMemoryHighFromQoSLevel calculates memory.high value based on QoS level
// For cgroup v2, memory.high is used to set soft memory limit
func CalculateMemoryHighFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 0 // No limit for high priority
	case 1:
		return 0 // No limit for normal priority
	case 0:
		return 0 // No limit for normal priority
	case -1:
		return 1073741824 // 1GB limit for best effort
	default:
		return 0
	}
}

// CalculateMemoryLowFromQoSLevel calculates memory.low value based on QoS level
// For cgroup v2, memory.low is used to set minimum memory guarantee
func CalculateMemoryLowFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 2147483648 // 2GB guarantee for high priority
	case 1:
		return 1073741824 // 1GB guarantee for normal priority
	case 0:
		return 536870912 // 512MB guarantee for normal priority
	case -1:
		return 0 // No guarantee for best effort
	default:
		return 0
	}
}

// CalculateMemoryMinFromQoSLevel calculates memory.min value based on QoS level
// For cgroup v2, memory.min is used to set minimum memory reservation
func CalculateMemoryMinFromQoSLevel(qosLevel int64) uint64 {
	switch qosLevel {
	case 2:
		return 1073741824 // 1GB reservation for high priority
	case 1:
		return 536870912 // 512MB reservation for normal priority
	case 0:
		return 268435456 // 256MB reservation for normal priority
	case -1:
		return 0 // No reservation for best effort
	default:
		return 0
	}
}
