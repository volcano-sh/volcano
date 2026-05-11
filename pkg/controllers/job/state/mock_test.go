/*
Copyright 2024 The Volcano Authors.

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

package state

import (
	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

// Captured call records for the injected action functions.
type syncCall struct {
	Job          *apis.JobInfo
	StatusAfter  vcbatch.JobStatus
	PhaseChanged bool
	HadStatusFn  bool
}

type killJobCall struct {
	Job          *apis.JobInfo
	RetainPhase  PhaseMap
	StatusAfter  vcbatch.JobStatus
	PhaseChanged bool
	HadStatusFn  bool
}

type killTargetCall struct {
	Job          *apis.JobInfo
	Target       Target
	StatusAfter  vcbatch.JobStatus
	PhaseChanged bool
	HadStatusFn  bool
}

// mockOps replaces the package-level action functions with capturing fakes.
// Each fake invokes the supplied UpdateStatusFn against the Job's Status so
// state transition logic is exercised against a real *JobStatus.
type mockOps struct {
	SyncCalls       []syncCall
	KillJobCalls    []killJobCall
	KillTargetCalls []killTargetCall

	SyncErr       error
	KillJobErr    error
	KillTargetErr error

	origSync       ActionFn
	origKillJob    KillActionFn
	origKillTarget KillTargetFn
}

func (m *mockOps) install() {
	m.origSync = SyncJob
	m.origKillJob = KillJob
	m.origKillTarget = KillTarget

	SyncJob = func(job *apis.JobInfo, fn UpdateStatusFn) error {
		c := syncCall{Job: job, HadStatusFn: fn != nil}
		if fn != nil {
			c.PhaseChanged = fn(&job.Job.Status)
		}
		c.StatusAfter = job.Job.Status
		m.SyncCalls = append(m.SyncCalls, c)
		return m.SyncErr
	}
	KillJob = func(job *apis.JobInfo, retain PhaseMap, fn UpdateStatusFn) error {
		c := killJobCall{Job: job, RetainPhase: retain, HadStatusFn: fn != nil}
		if fn != nil {
			c.PhaseChanged = fn(&job.Job.Status)
		}
		c.StatusAfter = job.Job.Status
		m.KillJobCalls = append(m.KillJobCalls, c)
		return m.KillJobErr
	}
	KillTarget = func(job *apis.JobInfo, target Target, fn UpdateStatusFn) error {
		c := killTargetCall{Job: job, Target: target, HadStatusFn: fn != nil}
		if fn != nil {
			c.PhaseChanged = fn(&job.Job.Status)
		}
		c.StatusAfter = job.Job.Status
		m.KillTargetCalls = append(m.KillTargetCalls, c)
		return m.KillTargetErr
	}
}

func (m *mockOps) uninstall() {
	SyncJob = m.origSync
	KillJob = m.origKillJob
	KillTarget = m.origKillTarget
}

// newMock installs a fresh mockOps and returns it with its cleanup func.
func newMock() (*mockOps, func()) {
	m := &mockOps{}
	m.install()
	return m, m.uninstall
}

// makeJob builds a minimal JobInfo with the given phase and initial status.
func makeJob(phase vcbatch.JobPhase) *apis.JobInfo {
	return &apis.JobInfo{
		Namespace: "ns",
		Name:      "job-info",
		Job: &vcbatch.Job{
			Status: vcbatch.JobStatus{
				State: vcbatch.JobState{Phase: phase},
			},
		},
	}
}
