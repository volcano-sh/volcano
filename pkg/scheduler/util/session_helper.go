package util

import (
	"sync"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type StatusLastSetCache struct {
	Cache map[api.JobID]map[api.TaskID]int64
	sync.RWMutex
}

var (
	podStatusLastSetCache = StatusLastSetCache{Cache: map[api.JobID]map[api.TaskID]int64{}}
)

func SetPodStatusLastSetCache(jobID api.JobID, taskID api.TaskID, ts int64) {
	podStatusLastSetCache.Lock()
	defer podStatusLastSetCache.Unlock()
	if _, ok := podStatusLastSetCache.Cache[jobID]; !ok {
		podStatusLastSetCache.Cache[jobID] = map[api.TaskID]int64{}
	}
	podStatusLastSetCache.Cache[jobID][taskID] = ts
}

func GetPodStatusLastSetCache(jobID api.JobID, taskID api.TaskID) (ts int64, exist bool) {
	podStatusLastSetCache.RLock()
	defer podStatusLastSetCache.RUnlock()
	ts, exist = podStatusLastSetCache.Cache[jobID][taskID]
	return
}

func CleanUnusedPodStatusLastSetCache(jobs map[api.JobID]*api.JobInfo) {
	podStatusLastSetCache.Lock()
	defer podStatusLastSetCache.Unlock()
	for jobID := range podStatusLastSetCache.Cache {
		if _, ok := jobs[jobID]; !ok {
			delete(podStatusLastSetCache.Cache, jobID)
		}
	}
}
