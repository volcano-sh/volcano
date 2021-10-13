/*
Copyright 2021 The Volcano Authors.

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
package elect

import (
	"reflect"
	"testing"
	"time"
	"volcano.sh/volcano/pkg/scheduler/util"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/reservation"
)

func TestElect(t *testing.T) {
	framework.RegisterPluginBuilder("reservation", reservation.New)
	defer framework.CleanupPluginBuilders()

	longestDuration, _ := time.ParseDuration("2h")
	shortDuration, _ := time.ParseDuration("1h")
	tests := []struct {
		name        string
		pendingJobs []*api.JobInfo
	}{
		{
			name: "select the job with highest priority as target job",
			pendingJobs: []*api.JobInfo{
				{
					UID:      api.JobID(rune(1)),
					Name:     "job-1",
					Priority: 1,
				},
				{
					UID:      api.JobID(rune(2)),
					Name:     "job-2",
					Priority: 2,
				},
				{
					UID:      api.JobID(rune(3)),
					Name:     "job-3",
					Priority: 3,
				},
			},
		},
		{
			name: "select the job with longest waiting time among the highest priority jobs as target job",
			pendingJobs: []*api.JobInfo{
				{
					UID:         api.JobID(rune(1)),
					Name:        "job-1",
					WaitingTime: &longestDuration,
				},
				{
					UID:         api.JobID(rune(2)),
					Name:        "job-2",
					WaitingTime: &shortDuration,
				},
				{
					UID:         api.JobID(rune(3)),
					Name:        "job-3",
					WaitingTime: &shortDuration,
				},
			},
		},
	}

	elect := New()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedulerCache := &cache.SchedulerCache{
				Jobs: make(map[api.JobID]*api.JobInfo),
			}
			for _, job := range test.pendingJobs {
				schedulerCache.AddJob(job)
			}

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:                 "reservation",
							EnabledTargetJob:     &trueValue,
							EnabledReservedNodes: &trueValue,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)
			elect.Execute(ssn)

			if reflect.DeepEqual(util.Reservation.TargetJob, nil) {
				t.Errorf("expected: not nil, got nil")
			}
		})
	}
}
