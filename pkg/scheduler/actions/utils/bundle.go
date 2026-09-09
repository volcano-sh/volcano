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

package utils

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

// VictimOrderPolicy orders disruption class and workload priority within a
// victim queue. Reclamation always compares victim queues before this policy.
type VictimOrderPolicy string

const (
	// VictimOrderSafeFirst considers safe bundles before whole bundles within
	// each victim queue, then prefers lower-priority workloads.
	VictimOrderSafeFirst VictimOrderPolicy = "safe-first"
	// VictimOrderPriorityFirst prefers lower-priority workloads within each victim
	// queue, with safe before whole across workloads at the same priority.
	VictimOrderPriorityFirst VictimOrderPolicy = "priority-first"
)

// ParseVictimOrderPolicy validates a configured gang-aware victim ordering policy.
func ParseVictimOrderPolicy(value string) (VictimOrderPolicy, bool) {
	policy := VictimOrderPolicy(value)
	switch policy {
	case VictimOrderSafeFirst, VictimOrderPriorityFirst:
		return policy, true
	default:
		return VictimOrderSafeFirst, false
	}
}

type Bundle struct {
	Type      BundleType
	Job       *api.JobInfo
	Tasks     []*api.TaskInfo
	LocalRes  *api.Resource
	GlobalRes *api.Resource
}

// CreateJobBundles splits local candidate tasks into safe/whole bundles at job level.
//
// A task is "safe" iff evicting it does not flip the job from gang-ready to
// not-ready. Gang readiness is the conjunction of three checks (mirroring the
// gang plugin):
//
//   - jobSurplus     = ReadyTaskNum() - MinAvailable           (mirrors IsReady)
//   - roleSurplus[r] = allocated(r)   - TaskMinAvailable[r]    (mirrors CheckTaskReady)
//   - groupSurplus[g] + subJobBroken                           (mirrors CheckSubJobReady)
//
// Sub-job tracking assumes the controller contract sj.MinAvailable == len(sj.Tasks),
// so a sub-job is either ready (all pods ready) or broken (at least one missing).
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

	jobSurplus := job.ReadyTaskNum() - job.MinAvailable
	// Job is already not gang-ready; further eviction cannot make it more broken.
	if jobSurplus < 0 {
		return []*Bundle{{
			Type:      BundleSafe,
			Job:       job,
			Tasks:     localTasks,
			LocalRes:  sumTasks(localTasks),
			GlobalRes: api.EmptyResource(),
		}}
	}

	// Initialize role-based surplus (unit: tasks). roleSurplus[r] is the number
	// of extra tasks role r can lose before falling below TaskMinAvailable[r].
	// Mirrors CheckTaskReady's own bypass when MinAvailable < TaskMinAvailableTotal.
	enforceRoles := job.MinAvailable >= job.TaskMinAvailableTotal
	roleSurplus := map[string]int32{}
	if enforceRoles {
		roleReady := map[string]int32{}
		for status, tasks := range job.TaskStatusIndex {
			switch {
			case api.AllocatedStatus(status) || status == api.Succeeded:
				for _, t := range tasks {
					roleReady[t.TaskRole]++
				}
			case status == api.Pending:
				for _, t := range tasks {
					if t.InitResreq.IsEmpty() {
						roleReady[t.TaskRole]++
					}
				}
			}
		}
		for role, min := range job.TaskMinAvailable {
			roleSurplus[role] = roleReady[role] - min
		}
	}

	// Initialize subJobGroup-based surplus (unit: sub-jobs). groupSurplus[g] is
	// the number of extra ready sub-jobs group g can lose before falling below
	// MinSubJobs[g]; subJobBroken tracks sub-jobs already missing a pod.
	enforceSubJobs := len(job.MinSubJobs) > 0
	// subJobBroken records sub-jobs that have already lost at least one pod and
	// so no longer count toward groupSurplus.
	subJobBroken := map[api.SubJobID]bool{}
	// groupSurplus is the per-group count of ready sub-jobs minus MinSubJobs[g].
	groupSurplus := map[api.SubJobGID]int32{}
	if enforceSubJobs {
		for _, sj := range job.SubJobs {
			if sj.IsReady() {
				groupSurplus[sj.GID]++
			} else {
				subJobBroken[sj.UID] = true
			}
		}
		for gid, minSJ := range job.MinSubJobs {
			groupSurplus[gid] -= minSJ
		}
	}

	safeTasks := make([]*api.TaskInfo, 0, len(localTasks))
	wholeTasks := make([]*api.TaskInfo, 0, len(localTasks))

	for _, task := range localTasks {
		// roleSafe: after removing this task, is the role still at or above
		// TaskMinAvailable? Defaults to true when the role has no min.
		roleSafe := true
		hasRoleMin := false
		if enforceRoles {
			if _, ok := job.TaskMinAvailable[task.TaskRole]; ok {
				hasRoleMin = true
				roleSafe = roleSurplus[task.TaskRole] > 0
			}
		}

		var sj *api.SubJobInfo
		// subJobGroupSafe: after removing this task, is the number of ready
		// sub-jobs in the same group still at or above MinSubJobs[g]?
		subJobGroupSafe := true
		if enforceSubJobs {
			sj = job.SubJobs[job.TaskToSubJob[task.UID]]
			if sj == nil {
				// Orphan task: bookkeeping bug; route to whole defensively.
				subJobGroupSafe = false
			} else {
				// Safe if sj is already broken (no further gang cost) or the
				// group has a ready sub-job to spare.
				subJobGroupSafe = subJobBroken[sj.UID] || groupSurplus[sj.GID] > 0
			}
		}

		if jobSurplus > 0 && roleSafe && subJobGroupSafe {
			safeTasks = append(safeTasks, task)
			jobSurplus--
			if hasRoleMin {
				roleSurplus[task.TaskRole]--
			}
			if enforceSubJobs && sj != nil && !subJobBroken[sj.UID] {
				// First eviction breaks sj; costs the group one ready sub-job.
				subJobBroken[sj.UID] = true
				groupSurplus[sj.GID]--
			}
		} else {
			wholeTasks = append(wholeTasks, task)
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

// FilterOrderedBundles replays the accepted prefix for each candidate. A failed
// Whole trial therefore cannot spend the allowance of later bundles. Filters
// must evaluate each call against the unchanged session snapshot; replaying the
// prefix preserves cumulative resource accounting across calls.
// Safe bundles become single-task selection units so placement can stop early.
func FilterOrderedBundles(bundles []*Bundle, allowWhole bool, filter func([]*api.TaskInfo) []*api.TaskInfo) []*Bundle {
	var accepted []*api.TaskInfo
	var result []*Bundle
	try := func(b *Bundle) {
		trial := make([]*api.TaskInfo, 0, len(accepted)+len(b.Tasks))
		trial = append(trial, accepted...)
		trial = append(trial, b.Tasks...)
		allowed := make(map[api.TaskID]bool, len(trial))
		for _, task := range filter(trial) {
			allowed[task.UID] = true
		}
		for _, task := range trial {
			if !allowed[task.UID] {
				return
			}
		}
		accepted = trial
		result = append(result, b)
	}
	for _, b := range bundles {
		if b.Type == BundleWhole {
			if allowWhole {
				try(b)
			}
			continue
		}
		for _, task := range b.Tasks {
			unit := *b
			unit.Tasks = []*api.TaskInfo{task}
			unit.LocalRes = sumTasks(unit.Tasks)
			try(&unit)
		}
	}
	return result
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

func SortBundlesForPreempt(bundles []*Bundle, need *api.Resource, policy VictimOrderPolicy, lessVictimJobFn func(l, r *api.JobInfo) bool) {
	sort.SliceStable(bundles, func(i, j int) bool {
		l, r := bundles[i], bundles[j]
		if cmp := compareBundlePolicy(l, r, policy, lessVictimJobFn); cmp != 0 {
			return cmp < 0
		}
		iroi := bundleROI(l, need)
		jroi := bundleROI(r, need)
		if iroi != jroi {
			return iroi > jroi
		}
		return stableBundleLess(l, r)
	})
}

func SortBundlesForReclaim(bundles []*Bundle, need *api.Resource, policy VictimOrderPolicy, lessQueueFn func(l, r *api.QueueInfo) bool, queues map[api.QueueID]*api.QueueInfo) {
	sort.SliceStable(bundles, func(i, j int) bool {
		l, r := bundles[i], bundles[j]
		if cmp := compareVictimQueues(l, r, lessQueueFn, queues); cmp != 0 {
			return cmp < 0
		}
		if cmp := compareBundlePolicy(l, r, policy, nil); cmp != 0 {
			return cmp < 0
		}
		iroi := bundleROI(l, need)
		jroi := bundleROI(r, need)
		if iroi != jroi {
			return iroi > jroi
		}
		return stableBundleLess(l, r)
	})
}

func compareBundlePolicy(l, r *Bundle, policy VictimOrderPolicy, lessVictimJobFn func(l, r *api.JobInfo) bool) int {
	if policy == VictimOrderPriorityFirst {
		if cmp := compareJobPriority(l, r); cmp != 0 {
			return cmp
		}
		if cmp := compareBundleType(l, r); cmp != 0 {
			return cmp
		}
		return compareVictimJobs(l.Job, r.Job, lessVictimJobFn)
	}
	if cmp := compareBundleType(l, r); cmp != 0 {
		return cmp
	}
	if cmp := compareJobPriority(l, r); cmp != 0 {
		return cmp
	}
	return compareVictimJobs(l.Job, r.Job, lessVictimJobFn)
}

func compareBundleType(l, r *Bundle) int {
	if l.Type < r.Type {
		return -1
	}
	if l.Type > r.Type {
		return 1
	}
	return 0
}

func compareJobPriority(l, r *Bundle) int {
	if l == nil || r == nil || l.Job == nil || r.Job == nil || l.Job.UID == r.Job.UID {
		return 0
	}
	if l.Job.Priority < r.Job.Priority {
		return -1
	}
	if l.Job.Priority > r.Job.Priority {
		return 1
	}
	return 0
}

func compareVictimJobs(l, r *api.JobInfo, lessVictimJobFn func(l, r *api.JobInfo) bool) int {
	if lessVictimJobFn == nil || l == nil || r == nil || l.UID == r.UID {
		return 0
	}
	return compareLess(lessVictimJobFn(l, r), lessVictimJobFn(r, l))
}

func compareVictimQueues(l, r *Bundle, lessQueueFn func(l, r *api.QueueInfo) bool, queues map[api.QueueID]*api.QueueInfo) int {
	if l == nil || r == nil || l.Job == nil || r.Job == nil {
		return 0
	}
	lq := queues[l.Job.Queue]
	rq := queues[r.Job.Queue]
	if lq == nil || rq == nil || lq.UID == rq.UID {
		return 0
	}
	if lessQueueFn != nil {
		if cmp := compareLess(lessQueueFn(lq, rq), lessQueueFn(rq, lq)); cmp != 0 {
			return cmp
		}
	}
	if lq.UID < rq.UID {
		return -1
	}
	return 1
}

func compareLess(lBeforeR, rBeforeL bool) int {
	if lBeforeR == rBeforeL {
		return 0
	}
	if lBeforeR {
		return -1
	}
	return 1
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
