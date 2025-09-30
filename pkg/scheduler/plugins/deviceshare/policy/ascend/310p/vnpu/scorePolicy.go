package vnpu310p

import (
	v1 "k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/ascend310p/vnpu"
)

func ScoreBatchNodes(pod *v1.Pod, schedulePolicy string, device api.Devices, neighbours []api.Devices) []float64 {
	npuDevice, ok := device.(*vnpu.NPUDevices)
	if !ok {
		return nil
	}

	var neighbourNodeInfs []*vnpu.NodeInf
	var neighbourNPUDevice []*vnpu.NPUDevice
	var podDowngradeCache []string
	for _, neighbour := range neighbours {
		if npuNeighbour, ok := neighbour.(*vnpu.NPUDevices); ok {
			neighbourNodeInfs = append(neighbourNodeInfs, &npuNeighbour.NodeInf)
			neighbourNPUDevice = append(neighbourNPUDevice, &npuNeighbour.NPUDevice)
			_, ok := npuNeighbour.DowngradeCache[pod.Name]
			if ok {
				podDowngradeCache = append(podDowngradeCache, npuNeighbour.NodeInf.Name)
			}
		}
	}

}
