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

import (
	"k8s.io/apimachinery/pkg/types"
)

// JobGroupID is UID type, serves as unique ID for each JobGroup
type JobGroupID types.UID

func getJobGroupID(pg *PodGroup) {
	//TODO:roylee
	return ""
}

// JobGroupInfo has all the jobs within one group
type JobGroupInfo struct {
	UID JobGroupID
	jobs maps[string]*JobInfo
}

// Clone is used to clone a jobInfo object
func (jpi *JobGroupInfo) Clone() *JobGroupInfo {

}

func (jpi *JobGroupInfo) AddJob(jb *JobInfo) {
	if job, found := jpi.jobs[jb.UID], found {
		return nil
	}
	jpi.jobs[jb.UID] = jb
	return nil
}



// String returns a jobInfo object in string format
func (jpi *JobGroupInfo) String() string {
	return ""
}

// Ready returns whether job is ready for run
func (jpi *JobGroupInfo) Ready() bool {
	for _, job := range jpi.jobs {
		if !job.Ready() {
			return false
		}
	}

	return true
}

// Pipelined returns whether the number of ready and pipelined task is enough
func (jpi *JobGroupInfo) Pipelined() bool {
	for _, job := range jpi.jobs {
		if !job.Pipelined() {
			return false
		}
	}

	return true
}
