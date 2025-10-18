package vnpu310p

import (
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/ascend310p/vnpu"
)

func ScoreBatchNodes(pod *v1.Pod, schedulePolicy string, device api.Devices, neighbours []api.Devices) []float64 {
	var neighbourNodeInfs []*vnpu.NodeInf
	var podDowngradeCache []string
	for _, neighbour := range neighbours {
		if npuNeighbour, ok := neighbour.(*vnpu.NPUDevices); ok {
			neighbourNodeInfs = append(neighbourNodeInfs, &npuNeighbour.NodeInf)
			_, ok := npuNeighbour.DowngradeCache[pod.Name]
			if ok {
				podDowngradeCache = append(podDowngradeCache, npuNeighbour.NodeInf.Name)
			}
		}
	}

	// init score-map
	scoreMap := initScoreMap(neighbourNodeInfs)

	nodesSorted := orderVNodesByFreeResource(neighbourNodeInfs)
	if len(nodesSorted) == 0 {
		klog.V(vnpu.LogErrorLev).Infof("dynamic vnpu task<%s> ScoreBestNPUNodes err: sorted nodes len 0", pod.Name)
		// Return array with same length as neighbours, all zeros
		return make([]float64, len(neighbours))
	}

	// 2. give the first node high score, none nodes are downgraded
	if len(podDowngradeCache) == 0 {
		_, sOK := scoreMap[nodesSorted[0].Name]
		if !sOK {
			scoreMap[nodesSorted[0].Name] = 0.0
		}
		scoreMap[nodesSorted[0].Name] += vnpu.NPUIndex8
	} else {
		// 3. if downgrade nodes exists, skip, util find none-downgraded nodes and add score
		for _, node := range nodesSorted {
			downgradeFlag := false
			for _, dNode := range podDowngradeCache {
				if node.Name == dNode {
					downgradeFlag = true
					break
				}
			}
			if !downgradeFlag {
				scoreMap[node.Name] += vnpu.NPUIndex8 * vnpu.NPUIndex2
				break
			}
			scoreMap[node.Name] += vnpu.NPUIndex8
		}
	}

	// Convert scoreMap to array in the same order as neighbours
	scores := make([]float64, len(neighbours))
	for i, neighbour := range neighbours {
		if npuNeighbour, ok := neighbour.(*vnpu.NPUDevices); ok {
			if score, exists := scoreMap[npuNeighbour.NodeInf.Name]; exists {
				scores[i] = score
			}
		}
	}

	return scores
}

func initScoreMap(nodes []*vnpu.NodeInf) map[string]float64 {
	scoreMap := make(map[string]float64, len(nodes))
	for _, node := range nodes {
		if reflect.ValueOf(node).IsNil() {
			continue
		}
		scoreMap[node.Name] = 0.0
	}
	return scoreMap
}
