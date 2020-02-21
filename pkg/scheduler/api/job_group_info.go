/*
Copyright 2017 The Kubernetes Authors.

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

package api

type JobGroupID string

// JobGroupInfo has all the jobs within one group
type JobGroupInfo struct {
	UID       JobGroupID
	Namespace string
	Queue     QueueID
	Jobs      map[JobID]*JobInfo
	// TopJob is the job with the highest order
	TopJob *JobInfo
	// orderFn specify how to compare the job order
	orderFn LessFn
}

// NewJobGroupInfo creates a new JobGroupInfo by the UID
func NewJobGroupInfo(job *JobInfo, lessFn LessFn) *JobGroupInfo {
	group := &JobGroupInfo{
		Jobs:      make(map[JobID]*JobInfo),
		Namespace: job.Namespace,
		Queue:     job.Queue,
		orderFn:   lessFn,
	}
	if job.SubGroup == "" {
		group.UID = JobGroupID(job.UID)
	} else {
		group.UID = JobGroupID(job.SubGroup)
	}
	group.Namespace = job.Namespace
	group.Queue = job.Queue
	group.AddJob(job)
	return group

}

// AddJob adds a JobInfo into JobGroupInfo
func (jgi *JobGroupInfo) AddJob(jb *JobInfo) {
	if _, found := jgi.Jobs[jb.UID]; found {
		return
	}
	if jb.Namespace != jgi.Namespace || jb.Queue != jgi.Queue {
		return
	}
	jgi.Jobs[jb.UID] = jb
	if jgi.TopJob == nil || jgi.orderFn(jb, jgi.TopJob) {
		jgi.TopJob = jb
	}
}

// Ready returns whether all jobs are ready for run
func (jgi *JobGroupInfo) Ready() bool {
	for _, job := range jgi.Jobs {
		if !job.Ready() {
			return false
		}
	}

	return true
}

// Pipelined returns whether all jobs are in pipelined state
func (jgi *JobGroupInfo) Pipelined() bool {
	for _, job := range jgi.Jobs {
		if !job.Pipelined() {
			return false
		}
	}

	return true
}
