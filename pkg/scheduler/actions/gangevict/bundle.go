/*
Copyright 2026 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gangevict

import (
	"math"
	"sort"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type BundleType int

const (
	BundleSafe BundleType = iota
	BundleWhole
)

type Bundle struct {
	Type      BundleType
	Job       *api.JobInfo
	Tasks     []*api.TaskInfo
	LocalRes  *api.Resource
	GlobalRes *api.Resource
}

// CreateJobBundles splits local candidate tasks into safe/whole bundles at job level.
func CreateJobBundles(job *api.JobInfo, localTasks []*api.TaskInfo) []*Bundle {
	if job == nil || len(localTasks) == 0 {
		return nil
	}

	sort.Slice(localTasks, func(i, j int) bool {
		if localTasks[i].Priority != localTasks[j].Priority {
			return localTasks[i].Priority < localTasks[j].Priority
		}
		return localTasks[i].UID < localTasks[j].UID
	})

	globalSurplus := job.ReadyTaskNum() - job.MinAvailable
	enforceRoles := job.MinAvailable >= job.TaskMinAvailableTotal
	roleSurplus := map[string]int32{}
	if enforceRoles {
		for role, min := range job.TaskMinAvailable {
			roleSurplus[role] = readyByRole(job, role) - min
		}
	}

	safeTasks := make([]*api.TaskInfo, 0, len(localTasks))
	wholeTasks := make([]*api.TaskInfo, 0, len(localTasks))

	if globalSurplus < 0 {
		safeTasks = append(safeTasks, localTasks...)
	} else {
		for _, task := range localTasks {
			roleSafe := !enforceRoles || roleSurplus[task.TaskRole] > 0
			if globalSurplus > 0 && roleSafe {
				safeTasks = append(safeTasks, task)
				globalSurplus--
				if enforceRoles {
					roleSurplus[task.TaskRole]--
				}
			} else {
				wholeTasks = append(wholeTasks, task)
			}
		}
	}

	bundles := make([]*Bundle, 0, 2)
	if len(safeTasks) > 0 {
		bundles = append(bundles, &Bundle{
			Type:      BundleSafe,
			Job:       job,
			Tasks:     safeTasks,
			LocalRes:  sumTasks(safeTasks),
			GlobalRes: api.EmptyResource(),
		})
	}
	if len(wholeTasks) > 0 {
		bundles = append(bundles, &Bundle{
			Type:      BundleWhole,
			Job:       job,
			Tasks:     wholeTasks,
			LocalRes:  sumTasks(wholeTasks),
			GlobalRes: sumJobTasks(job),
		})
	}
	return bundles
}

func FlattenBundles(bundles []*Bundle) []*api.TaskInfo {
	out := make([]*api.TaskInfo, 0)
	for _, b := range bundles {
		out = append(out, b.Tasks...)
	}
	return out
}

func ApplyAllowedTasks(bundles []*Bundle, allowed map[api.TaskID]struct{}) []*Bundle {
	valid := make([]*Bundle, 0, len(bundles))
	for _, b := range bundles {
		if len(b.Tasks) == 0 {
			continue
		}
		approved := make([]*api.TaskInfo, 0, len(b.Tasks))
		for _, t := range b.Tasks {
			if _, ok := allowed[t.UID]; ok {
				approved = append(approved, t)
			}
		}

		if b.Type == BundleWhole {
			if len(approved) == len(b.Tasks) {
				valid = append(valid, b)
			}
			continue
		}

		if len(approved) > 0 {
			nb := *b
			nb.Tasks = approved
			nb.LocalRes = sumTasks(approved)
			valid = append(valid, &nb)
		}
	}
	return valid
}

func SelectBundles(bundles []*Bundle, need *api.Resource, allowWhole bool) []*Bundle {
	selected := make([]*Bundle, 0, len(bundles))
	freed := api.EmptyResource()
	for _, b := range bundles {
		if b.Type == BundleWhole && !allowWhole {
			continue
		}
		selected = append(selected, b)
		freed.Add(b.LocalRes)
		if need.LessEqual(freed, api.Zero) {
			break
		}
	}
	return selected
}

func SortBundlesForPreempt(bundles []*Bundle, need *api.Resource, lessVictimJobFn func(l, r *api.JobInfo) bool) {
	sort.SliceStable(bundles, func(i, j int) bool {
		if bundles[i].Type != bundles[j].Type {
			return bundles[i].Type < bundles[j].Type
		}
		if lessVictimJobFn != nil && bundles[i].Job != nil && bundles[j].Job != nil && bundles[i].Job.UID != bundles[j].Job.UID {
			ij := lessVictimJobFn(bundles[i].Job, bundles[j].Job)
			ji := lessVictimJobFn(bundles[j].Job, bundles[i].Job)
			if ij != ji {
				return ij
			}
		}
		iroi := bundleROI(bundles[i], need)
		jroi := bundleROI(bundles[j], need)
		if iroi != jroi {
			return iroi > jroi
		}
		return stableBundleLess(bundles[i], bundles[j])
	})
}

func SortBundlesForReclaim(bundles []*Bundle, need *api.Resource, lessQueueFn func(l, r *api.QueueInfo) bool, queues map[api.QueueID]*api.QueueInfo) {
	sort.SliceStable(bundles, func(i, j int) bool {
		if bundles[i].Type != bundles[j].Type {
			return bundles[i].Type < bundles[j].Type
		}
		if lessQueueFn != nil {
			lq := queues[bundles[i].Job.Queue]
			rq := queues[bundles[j].Job.Queue]
			if lq != nil && rq != nil && lq.UID != rq.UID {
				ij := lessQueueFn(lq, rq)
				ji := lessQueueFn(rq, lq)
				if ij != ji {
					return ij
				}
			}
		}
		iroi := bundleROI(bundles[i], need)
		jroi := bundleROI(bundles[j], need)
		if iroi != jroi {
			return iroi > jroi
		}
		return stableBundleLess(bundles[i], bundles[j])
	})
}

func bundleROI(b *Bundle, need *api.Resource) float64 {
	local := scoreAgainstNeed(b.LocalRes, need, true)
	global := scoreAgainstNeed(b.GlobalRes, need, false)
	if global == 0 {
		return math.Inf(1)
	}
	return local / global
}

func scoreAgainstNeed(res, need *api.Resource, capAtNeed bool) float64 {
	if res == nil || need == nil {
		return 0
	}
	score := 0.0
	for _, name := range need.ResourceNames() {
		n := need.Get(name)
		if n <= 0 {
			continue
		}
		v := res.Get(name)
		if capAtNeed && v > n {
			v = n
		}
		score += v / n
	}
	return score
}

func sumTasks(tasks []*api.TaskInfo) *api.Resource {
	r := api.EmptyResource()
	for _, t := range tasks {
		r.Add(t.Resreq)
	}
	return r
}

func sumJobTasks(job *api.JobInfo) *api.Resource {
	r := api.EmptyResource()
	for _, t := range job.Tasks {
		r.Add(t.Resreq)
	}
	return r
}

func readyByRole(job *api.JobInfo, role string) int32 {
	var n int32
	for status, tasks := range job.TaskStatusIndex {
		if !api.AllocatedStatus(status) && status != api.Succeeded {
			continue
		}
		for _, t := range tasks {
			if t.TaskRole == role {
				n++
			}
		}
	}
	return n
}

func stableBundleLess(l, r *Bundle) bool {
	lJobUID := ""
	if l.Job != nil {
		lJobUID = string(l.Job.UID)
	}
	rJobUID := ""
	if r.Job != nil {
		rJobUID = string(r.Job.UID)
	}
	if lJobUID != rJobUID {
		return lJobUID < rJobUID
	}

	lFirstTaskUID := firstTaskUID(l)
	rFirstTaskUID := firstTaskUID(r)
	if lFirstTaskUID != rFirstTaskUID {
		return lFirstTaskUID < rFirstTaskUID
	}

	if len(l.Tasks) != len(r.Tasks) {
		return len(l.Tasks) < len(r.Tasks)
	}
	return false
}

func firstTaskUID(b *Bundle) api.TaskID {
	if b == nil || len(b.Tasks) == 0 {
		return ""
	}
	return b.Tasks[0].UID
}
